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

## Exit codes

`0` success; `1` runtime failure (or staleness for `refresh --check`);
`2` usage error. Query subcommands (`dedupe`, `wayback`, `check`, `lookup`)
exit 0 even when inputs miss — the verdicts live in the output, not the
exit code.
