# fav

Batch-oriented helper CLI for the favorites workflow. Every subcommand is a
deterministic evidence gatherer: it fetches, normalizes, and matches, then
prints full records for a human or agent to judge. There is deliberately no
concurrency — requests are sequential and rate-limited, so batch runs stay
respectful of the servers they touch.

Build:

```bash
cd tools/fav && go build -o fav .
```

All subcommands accept batch input as arguments or one-per-line on stdin
(`-` also reads stdin), preserve input order in the output, ignore blank
lines and `#` comments, and support `--json` for machine-readable output.

## fav dedupe

Find out whether URLs are already in the collection, and get the full stored
records back — not just a "duplicate" verdict.

```bash
fav dedupe https://stripe.com/blog/rate-limiters
printf '%s\n' "https://a.example/x" "https://b.example/y naval ravikant" | fav dedupe
```

- Matches against both `url` and `alternate-url` of every entry after
  normalization: scheme-insensitive (http == https), leading `www.` removed,
  trailing slashes trimmed, tracking params (`utm_*`, `fbclid`, ...) dropped,
  query sorted, fragment removed. Wayback snapshot URLs are unwrapped first,
  so an original URL matches a Wayback-flipped canonical (and vice versa).
- A line may append title keywords after the URL (`URL title words...`) to
  also get fuzzy title candidates (`--min-score`, `--max-candidates`).
- Status per input: `url-match` | `alternate-match` | `title-only` | `none`.
  Matches print the complete stored record (title, url, alternate-url,
  published, tags) plus its `file[index]` locator.

## fav wayback

Find the latest (or earliest) 200-status Wayback capture for each URL —
the `alternate-url` backfill step.

```bash
fav wayback --mode latest https://example.com/article
fav wayback --mode both < urls.txt
```

- Availability API first, CDX API fallback (`filter=statuscode:200`).
- Retries variants when the exact form has no capture: `www.` toggle, then
  trailing-slash toggle (the report notes which variant hit).
- Snapshot URLs are normalized to the `https` scheme for storage.
- Politeness: `--delay 8s` between requests, `--backoff 25s` after 429/503
  (both tunable), `--retries 3`. Wayback rate-limits hard; do not lower the
  defaults for large batches.

## fav check

Verify what each URL actually serves before trusting it as canonical.

```bash
fav check https://www.salon.com/2001/03/23/wizards/
```

- Follows and prints the redirect chain (up to 10), flagging cross-host
  redirects.
- Extracts `<title>` and `rel=canonical` (og:url fallback), flagging when the
  declared canonical differs from the submitted/final URL (moved-domain
  detection, e.g. a Substack that migrated to a custom domain).
- Classification: `ok` | `bot-block-suspect` | `forbidden` | `not-found` |
  `server-error` | `error`. Bot-block detection uses interstitial titles and
  small challenge pages — a 200 page full of article text that merely embeds
  reCAPTCHA scripts stays `ok`.
- Uses a browser User-Agent (per the fetch playbook), a cookie jar, and an
  HTTP/1.1 fallback on alternate retry attempts (some servers drop HTTP/2
  streams from non-browser clients).
- Known limit: a few sites (e.g. mckinsey.com) reject this egress at the
  TLS/network layer regardless of client; use a proxied fetch for those.
- Politeness: `--delay 2s` default between requests.

## fav podcast lookup

Grep the committed episode indexes without writing jq one-liners.

```bash
fav podcast lookup --show lennys rumelt
fav podcast lookup "mauboussin"            # searches every show
```

- `--show` filters by slug or name substring; without it, all shows search.
- Keywords are AND-matched case-insensitively against episode titles.
- Prints each match as `published | title | url` plus the show's
  `canonicalPattern` and `appleId` for constructing Apple mirror URLs.

## fav podcast refresh

Regenerate `podcasts/<slug>.json` from the feeds in `podcasts/registry.json`
(drop-in Go replacement for `update-podcast-indexes.sh`, byte-compatible
output), or check staleness without writing.

```bash
fav podcast refresh                     # rewrite all indexes
fav podcast refresh --show acquired     # one show only
fav podcast refresh --check             # report drift, write nothing
```

- `--check` compares each live feed against the committed index and reports
  `missing` (new episodes), `retitled` (same URL, new title — rebrands
  retitle old episodes), and `redated` (same URL, new date). Exit code is 1
  when anything is stale or failed, 0 when all fresh.
- Identity: equal URL when both sides have one, else equal title.
- Date parsing mirrors the Python builder: RFC 822/1123 variants and ISO
  shapes; zoned timestamps keep their feed-local date (no UTC shift), naive
  timestamps are treated as UTC, unparseable becomes `null`.
- Output format matches the Python builder byte for byte (two-space indent,
  no HTML escaping, UTF-8, single trailing newline), so refreshes produce
  clean diffs.
- Politeness: `--delay 2s` default between feed fetches.

## fav podcast delisted

Diff each committed index against Apple's store listing to find delisted
episodes (and Apple-only episodes the index lacks).

```bash
fav podcast delisted                      # every show (slow: one API call per show)
fav podcast delisted --show fallthrough
```

- Source of truth for "still listed": the iTunes lookup API
  (`entity=podcastEpisode&limit=200`), compared by case-folded title.
- Apple returns only the ~200 most recent episodes; for longer feeds the
  comparison is windowed: index episodes older than Apple's oldest are
  reported as `skipped`, not falsely flagged delisted.
- Output: `notInApple` (delisted suspects), `notInIndex` (in Apple, absent
  from the index — usually just-published episodes a refresh would add).
- Politeness: `--delay 2s` default between API calls.

## fav date

Gather published-date evidence for URLs, ranked by the resolve-published-date
skill's ladder — every candidate printed, never a verdict.

```bash
fav date https://www.kalzumeus.com/2012/01/23/salary-negotiation/
fav date --offline < urls.txt     # local rungs only (URL dates, podcast indexes)
fav date --force <url>            # fetch the page even when an early rung found a day
```

The ladder, cheapest/most-authoritative first:

1. URL path/filename dates (`/2012/01/23/`, `manager-tools-2011-03-07.mp3`).
2. Committed podcast indexes (normalized episode-URL match), HN item
   timestamps (Firebase API), bare Apple episode URLs (iTunes lookup; reports
   delisted episodes).
3. Page metadata: JSON-LD `datePublished`/`dateCreated`/`uploadDate`,
   `article:published_time`, `dc.date`, `<time datetime>`, dated MP3 links;
   for PDFs, XMP `CreateDate` and Info `CreationDate`.
4. Wayback snapshot content when the live page is blocked/failed.
5. Crossref for `queue.acm.org/detail.cfm?id=N` and `doi.org` links
   (month precision for 2-part date-parts).
7. HN Algolia first-submission month.
8. Wayback earliest capture as a lower bound.

- Early exit once a day-precision candidate exists (`--force` overrides);
  later rungs also stop when a day is known.
- Conflicting day-precision dates (the feed-date vs MP3-filename trap) are
  surfaced as `NOTE`s.
- `--offline` runs only rungs 1–2 (no network).
- Politeness: `--delay 2s` default, one retry, 20s per-request cap — metadata
  sniffing fails fast with a NOTE rather than hanging on slow captures.

## fav lint

Content-file hygiene checks. Absorbs `validate-json.sh` (structure) and
`count-urls.sh` (duplicate detection, but stricter: normalization makes
utm/www/scheme variants collide too).

```bash
fav lint                  # content/ in the repo root
fav lint --content dir
```

- Errors (exit 1): JSON syntax, single top-level category key, empty
  title/tags, non-absolute URL, malformed/invalid `published`, duplicate
  normalized `url`, duplicate normalized `alternate-url`.
- Warnings (exit 0): unknown category, first tag not the file category,
  missing media-type tag (HN Discussions exempt), media-type tag not last,
  multiple media-type tags, `alternate-url` same address as `url` (a Wayback
  backup of the url itself is fine and not flagged), `alternate-url` matching
  another entry's `url`, year-parenthetical in title, HN URL without the
  `HN Discussion:` prefix, tag case-collisions (`Golang` vs `golang`).

## fav graph add-edge

Idempotent graph writer for the maintain-favorites-graph skill. Reuses
existing entities case-insensitively, mints uppercase UUID4s for new ones,
checks edge types against the closed ontology, and routes `Is_a_Person` edges
to `is_a_person-edges.json` with the `Person` class entity forced as target.

```bash
fav graph add-edge "Peter Adkison" Founder_of "Wizards of the Coast" \
                   "Peter Adkison" Is_a_Person Person
printf '%s\t%s\t%s\n' "Charity Majors" "Current_Employee_of" "Honeycomb" | fav graph add-edge -
fav graph add-edge --dry-run ...        # report adds/reuses, write nothing
```

- Writes byte-compatibly with the Python `json.dump(indent=2,
  ensure_ascii=True)` format (non-ASCII as `\uXXXX`, no trailing newline), so
  diffs stay append-only.
- Duplicate edges are no-ops; per-triple output reports
  `added | exists` plus whether each endpoint entity was created.
- Reminder printed on write: run the graph validator before committing.

## fav graph bio

Wikidata evidence for people/orgs — candidates printed for the agent to
judge, never written.

```bash
fav graph bio "Naval Ravikant" "Wizards of the Coast"
fav graph bio --qid Q978815 "Peter Adkison"   # skip the search step
```

- `wbsearchentities` top hit plus up to 2 alternatives (`ALTS`); `--qid`
  pins the entity when the search hit is wrong.
- Claims read: P31=Q5 (human check), P108 employers with P580/P582
  qualifiers → `Current_Employee_of` (no end date) vs `Previous_Employee_of`
  (has end date) candidates, P112 founded-by for orgs.
- Reverse founder lookup for humans (orgs whose P112 points at them) via the
  Wikidata SPARQL endpoint.
- Always prints the verification note: `Current_Employee_of` claims need a
  first-party current source (LinkedIn, personal site) before writing.
- Politeness: `--delay 2s` default between Wikidata calls.

## Exit codes

`0` success; `1` runtime failure, staleness (`refresh --check`), or lint
errors; `2` usage error. Query subcommands (`dedupe`, `wayback`, `check`,
`date`, `lookup`, `bio`) exit 0 even when inputs miss — the verdicts live in
the output, not the exit code. `lint` warnings alone do not fail the run.
