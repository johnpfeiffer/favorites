#!/usr/bin/env bash
# Count total and unique URLs across every .json file in content/.
set -uo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/content"

if ! command -v jq >/dev/null 2>&1; then
  echo "Error: jq is required but not installed." >&2
  exit 2
fi

echo "Entries per file:"
for f in "$DIR"/*.json; do
  printf '  %-20s %s\n' "$(basename "$f")" "$(jq 'to_entries[0].value | length' "$f")"
done

urls=$(jq -r 'to_entries[0].value[].url' "$DIR"/*.json)

total=$(printf '%s\n' "$urls" | wc -l)
unique=$(printf '%s\n' "$urls" | sort -u | wc -l)

echo
echo "Total entries:  $total"
echo "Unique URLs:    $unique"

dupes=$(printf '%s\n' "$urls" | sort | uniq -d)
if [ -n "$dupes" ]; then
  echo
  echo "Duplicate URLs (same URL in more than one entry):"
  printf '%s\n' "$dupes" | sed 's/^/  /'
fi
