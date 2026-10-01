---
name: audit-urls
description: Bulk liveness audit of every URL in the favorites collection. Use when asked to check whether stored links are still alive, find dead/rotted domains and redirects, or produce a triage list of URLs needing fixes (301, 404, 5xx, DNS failures). Owns the parallel check harness and the result-classification rules; fixes themselves follow the add-favorite defunct-flip convention.
---

# Audit URLs

Verify every stored URL (`url` + `archivedAt` across `content/*.jsonld`) still serves content, classify the failures, and hand confirmed rot to the add-favorite defunct-flip flow. The goal is a triage list, not instant edits — most non-200s are bot-blocking or rate-limit artifacts, not dead links.

## Harness

```bash
.agents/skills/audit-urls/scripts/audit.sh <workdir>   # run from the repo root
```

Extracts unique URLs (~976 as of the first run: ~660 canonicals + ~310 archivedAt), checks each with `fav check -json`, and writes `results.json` plus a sorted `triage.tsv`. Three lanes with different politeness profiles:

- **Lane A (general):** shuffled, 35 chunks, 8 parallel workers, 1s per-request spacing. ~660 URLs finish in about 5 minutes. Shuffling is load-balancing: it spreads the big hosts (podcasts.apple.com 211, manager-tools.com 49) across workers and time.
- **Lane B (web.archive.org):** strictly serial, 8s spacing. Under lane-A parallelism archive.org connection-refuses — 46 false "connection refused" errors in the first run, all recovered on a slow retry. Never trust a parallel-run archive.org error.
- **Lane C (youtube.com):** strictly serial, 4s spacing. YouTube 429s every request under parallelism from a datacenter IP (37 false 429s in the first run). If serial still 429s, use `https://www.youtube.com/oembed?url=<watch-url>` for the liveness verdict instead of the watch page.

A full run is ~10 minutes, dominated by the serial lanes. For a cheap domains-only pass (the `/links/sources` question), extract hosts and HEAD-check each of the ~230 once — it finds dead/parked domains in a minute, but remember domain-up ≠ URL-up; it is triage, not the audit.

## Triage the results

`fav check` status classes, ordered by how much skepticism they deserve:

| Class | Meaning | Action |
| --- | --- | --- |
| `ok` | serves content | none (that is the bar) |
| `bot-block-suspect` (403) | alive, refusing non-browser clients | none — medium.com, manager-tools.com, mastersofscale.com, hbr.org, queue.acm.org all 403 curl and are fine. Never flip from this alone |
| `error` transient (429/timeout/conn-refused) | almost always rate-limiting from the audit itself | retry serially (the lanes already do); only believe it after a serial failure |
| `error` DNS `no such host` (after serial retry) | dead domain | confirm twice, then moved-canonical search, then defunct flip |
| `not-found` (404) | maybe gone | verify SPA hosts in a browser first (see below) |
| `http-202` | Cloudflare/anti-bot challenge | alive; ignore |
| `http-206` | serving partial content | alive; ignore |
| `http-402` | CDN/paywall block | alive-ish; browser-verify before any change |
| `http-5xx` | server error | retry later; only flip after it persists across days |

## Redirect forensics

`results.json` carries `finalUrl` (end of redirect chain) and `canonical` (page's declared canonical). Compare hosts:

- **Same-content moves** — update `url` to the destination on the next content touch, or leave (they still resolve): subdomain shifts (`firstround.com/review` → `review.firstround.com`, `blog.golang.org` → `go.dev/blog`), rebrands keeping the path (`joincolossus.com` → `colossus.com`, `blog.ycombinator.com` → `ycombinator.com/blog`), acquisition moves (`segment.com/blog` → `twilio.com/.../blog`).
- **Redirect to unrelated content = defunct.** The tell: path and topic both change (`/post/emerging-architectures-of-llm-applications` → `/blog/building-ask-seeking-alpha`, an articles page → a substack `/about`). Apply the add-favorite defunct flip: Wayback snapshot becomes canonical `url`; if the entry's `archivedAt` already holds a good snapshot of the original, promote it.
- **Canonical = bare homepage on an SPA** (e.g. episode pages declaring canonical `https://show.example/`): the SPA router misreports, not rot. Judge that host only from a rendered browser check.
- Wayback captures declare odd `canonical` values (path rewrites, `if_` variants) — ignore canonical on web.archive.org results entirely.

## False-positive rules

Never mark defunct from audit data alone. Browser-verify (FetchUrl or agent-browser) first: any 404 on SPA podcast hosts (podcasters.spotify.com, art19.com, Simplecast sites — routers soft-404 and soft-200), any 403/202/402, and any redirect whose destination merely *looks* unrelated. A stored `archivedAt` pointing at a known-dead original (e.g. a pre-flip engineering.linkedin.com URL kept for provenance) will show as a 404 in every audit — that is expected archaeology, not a regression; keep the exclusions list in the findings report.

## Capture lookups during archive.org outages

Fetching captures for flipped/refreshed canonicals hits two independent archive.org backends, and they fail independently: during the 2026-09-30 fix run the CDX API served "Temporarily Offline" pages for a stretch while the availability API kept answering (intermittently rate-limited), and earlier the same day web.archive.org itself connection-refused under parallel load. When one API fails, switch to the other instead of skipping captures:

- **Availability API** (`https://archive.org/wayback/available?url=<url-no-scheme>`) → `archived_snapshots.closest`; normalize the URL to https. Closest-snapshot only, but usually enough.
- **CDX API** (`https://web.archive.org/cdx/search/cdx` with `limit=-1&filter=statuscode:200`) → latest 200 capture; full history when you need earliest for dating.

`fav wayback` already tries availability-then-CDX, but it buffers all output until the run ends and its 25s backoff loops are invisible mid-run — for a bulk capture batch during a flaky window, a hand-rolled paced loop (one request per 4s+, 30-60s backoff on 429/503/offline pages, retry the www/no-www variant on a miss, progress line per URL) is easier to watch and just as polite. Treat every NONE from an outage window as *unverifiable*, not absent — kalzumeus and medium.com both "returned" NONEs while throttled. If neither API cooperates after retries, omit the unverified captures, list them as backfill candidates in the PR body, and never guess snapshot URLs.

## Fix handoff and cadence

Confirmed defunct links follow add-favorite: search the title for a moved canonical on the same publisher, else latest 200-status Wayback capture becomes `url` (verify the capture serves the content), omit the bare broken original from `archivedAt`, flag it in the PR body. Batch fixes into their own PRs, separate from new-link batches. A monthly cadence keeps rot bounded; each run should end with the findings summarized to the requester (counts by class, confirmed dead, moved canonicals, false-positive explanations).

**Archive the original, not the destination.** When a fix changes the canonical, `archivedAt` should capture the *original* bookmarked URL whenever one exists — that capture is literally the archive of what was saved, while a capture of the new canonical may hold modified or unrelated content (last resort). CDX `filter=statuscode:200&limit=-1` on the old URL returns the last pre-redirect capture. When the bookmark was a file and the redirect lands on a container page (e.g. research.microsoft.com PDFs → microsoft.com publication pages), keep the landing page as canonical but archive the destination's *direct PDF link*, not the container. archive.today captures and author-site mirrors are valid `archivedAt` alternates.

First-run calibration (976 URLs, 2026-09-29): 778 ok, 99 bot-block-suspect, 89 errors of which 83 were audit-induced (46 archive.org conn-refused + 37 youtube 429), 5 not-found (2 later confirmed SPA/relocated, rest genuine), ~20 cross-host redirects of which 2 were unrelated-content rot.
