#!/bin/bash
# Bulk liveness audit of every URL stored in content/*.jsonld (url + archivedAt).
#
# Lane design, learned from a 976-URL run (see ../SKILL.md for the full story):
#   lane A: everything except web.archive.org / youtube — shuffled, split into
#           chunks, 8 parallel workers, 1s per-request spacing inside each.
#           Shuffling matters: it spreads same-host URLs across workers and time.
#   lane B: web.archive.org — strictly serial, 8s spacing. Under parallel load
#           archive.org connection-refuses; the errors look like dead snapshots
#           but are pure rate-limiting (46 false "connection refused" in one run).
#   lane C: youtube.com — strictly serial, 4s spacing. YouTube 429s under any
#           parallelism from datacenter IPs; if it still 429s serially, fall back
#           to the oEmbed endpoint for a liveness verdict.
#
# Usage: audit.sh <workdir>
# Reads: content/*.jsonld in the current repo (run from the repo root)
# Writes: <workdir>/results.json   merged findings, one object per URL
#         <workdir>/triage.tsv     status-classified list for review
set -uo pipefail

WORK="${1:?usage: audit.sh <workdir>}"
FAV="$(dirname "$0")/../../../tools/fav/fav"
[ -x "$FAV" ] || FAV=tools/fav/fav
mkdir -p "$WORK"
cd "$(dirname "$0")/../../.."  # repo root

echo "== extracting URLs =="
cat content/*.jsonld \
  | jq -r '.itemListElement[]? | .url, (.archivedAt // empty)' \
  | sort -u > "$WORK/urls.txt"
grep    'web\.archive\.org' "$WORK/urls.txt" > "$WORK/lane-b.txt" || true
grep -v 'web\.archive\.org' "$WORK/urls.txt" | grep -E '^(https?://)?(www\.|m\.)?youtube\.com' > "$WORK/lane-c.txt" || true
grep -v 'web\.archive\.org' "$WORK/urls.txt" | grep -vE '^(https?://)?(www\.|m\.)?youtube\.com' > "$WORK/lane-a.txt" || true
wc -l "$WORK"/lane-*.txt

worker() {  # worker <chunk-file> <delay>
  mapfile -t urls < "$1"
  "$FAV" check -json -timeout 15s -retries 1 -backoff 5s -delay "$2" "${urls[@]}" > "$1.json" 2> "$1.err"
  echo "done $(basename "$1")"
}
export -f worker
export FAV

echo "== lane A: parallel general =="
shuf "$WORK/lane-a.txt" > "$WORK/lane-a-shuffled.txt"
split -n l/35 -d "$WORK/lane-a-shuffled.txt" "$WORK/chunk_"
ls "$WORK"/chunk_?? | xargs -P 8 -I{} bash -c 'worker "$@"' _ {} 1s

echo "== lane B: serial archive.org =="
mapfile -t burls < "$WORK/lane-b.txt"
[ ${#burls[@]} -gt 0 ] && "$FAV" check -json -timeout 20s -retries 2 -backoff 25s -delay 8s "${burls[@]}" > "$WORK/lane-b.json" 2> "$WORK/lane-b.err"

echo "== lane C: serial youtube =="
mapfile -t curls < "$WORK/lane-c.txt"
[ ${#curls[@]} -gt 0 ] && "$FAV" check -json -timeout 20s -retries 2 -backoff 10s -delay 4s "${curls[@]}" > "$WORK/lane-c.json" 2> "$WORK/lane-c.err"

echo "== merging =="
jq -s 'add // []' "$WORK"/chunk_??.json "$WORK"/lane-b.json "$WORK"/lane-c.json 2>/dev/null > "$WORK/results.json"
jq -r '.[] | [.status, (.httpStatus // ""), .input] | @tsv' "$WORK/results.json" | sort > "$WORK/triage.tsv"
jq -r '.[].status' "$WORK/results.json" | sort | uniq -c | sort -rn
echo "results: $WORK/results.json  triage: $WORK/triage.tsv"
