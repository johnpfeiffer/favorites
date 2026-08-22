---
name: resolve-published-date
description: Resolve the published date of a web link with evidence. Use when a favorites entry (or any web resource) needs its `published` date found or verified — covers URL-path and byline dates, podcast episode indexes and Apple/iTunes lookups, page metadata (og/JSON-LD), Medium, ACM Queue via Crossref, classic papers and books, Wayback snapshot forensics, bot-block workarounds, precision fallbacks (YYYY-MM-01), and living-reference nulls.
---

# Resolve a published date

Fill `published` with the **initial distribution/event date** of the linked content, at the best precision the evidence supports.

## Semantics and precision

- Initial distribution, not the file or upload timestamp. For recorded live events (conference talks, panels, meetups) use the event date when known; uploads can lag by months. For interviews released as both podcast and video, the podcast release is the initial distribution.
- Original airing over re-release: podcast feeds re-run old episodes (fs.blog re-releases Knowledge Project classics with new numbers/dates), pages get re-platformed with fresh `datePublished` stamps. Use the original date and note the re-release in the PR body.
- Precision ladder: exact day when verified → month-only becomes `YYYY-MM-01` → truly unverifiable stays `null` with the evidence noted in the PR body (earliest Wayback capture, HN submission, copyright year).
- Living references keep `null`: blog homepages, index/landing pages, continuously revised docs and textbooks, Wikipedia articles, GitHub READMEs.
- A date printed on the artifact itself beats site metadata: PDFs carry XMP `CreateDate` (`strings file.pdf | grep -i xap`), scanned papers carry their own date lines.

## Resolution order, cheapest first

1. **URL path**: exact day (`/2024/06/28/`, `/2015/12/23/`) or month (`/2016/08/` → fetch the page for the day, else `YYYY-MM-01`). MP3 filenames too (`career-tools-2024-12-19.mp3`).
2. **Committed podcast indexes**: `podcasts/<slug>.json` holds `{title, published, url}` per episode for recurring shows. Grep by title keyword, not number — feed and site numbering can drift (a Radical Candor episode is S3E7 in the feed but `season-3-episode-8` on the site). A transcript-mirror entry shares its episode's date.
   ```bash
   jq -r '.episodes[] | select(.title | test("keyword"; "i")) | [.published, .title] | @tsv' podcasts/<slug>.json
   ```
3. **Direct page fetch** (`curl -sL --compressed` with a browser UA; many sites 403 plain curl): grep the HTML for, in rough order of reliability:
   - JSON-LD `"datePublished"` / `"dateCreated"`
   - `article:published_time` meta
   - `<time datetime="...">` or itemprop
   - `"uploadDate"` (YouTube/video pages)
   - visible-text dates (`(Jan|Feb|...) \d{1,2},? \d{4}`), but verify context — see pitfalls below.
4. **Wayback snapshot content** for bot-blocked or defunct pages (see fetch playbooks). Wayback archives the original HTML verbatim, so its meta/JSON-LD values are the publisher's own; identical values across two independent snapshots corroborate.
5. **Crossref** for ACM/academic: `https://api.crossref.org/works?query.bibliographic=<title+words>&rows=3` returns `issued` date-parts. For `queue.acm.org/detail.cfm?id=<N>` pages (which 403 curl), the Crossref record whose DOI ends in `.<N>` is the same article; Queue issues are bimonthly so `issued: [2018, 2]` → `2018-02-01`.
6. **Bibliographic records** for books and classic papers: publisher page, Springer/MARC records, Wikipedia/Open Library (often year-only). Classic CS papers use the journal issue month (BSTJ July 1948 for Shannon; CACM 12(10) Oct 1969 for Hoare; SIGACT News 32(4) Dec 2001 for Paxos Made Simple; SOSP/NSDI conference month for systems papers). A best-seller-list debut (e.g. NYT) pins the on-sale month when no day-level source exists.
7. **HN Algolia** first submission for undated blogs: `https://hn.algolia.com/api/v1/search?query=<url-or-title>` — treat the first submission date as the publication month (`YYYY-MM-01`), noting it in the PR body.
8. **Wayback earliest capture** as a lower bound when nothing else exists:
   ```bash
   curl -s --get --data-urlencode "url=<url-without-scheme>" \
     --data "fl=timestamp&filter=statuscode:200&limit=1" \
     "https://web.archive.org/cdx/search/cdx"
   ```
9. **Git**: for content developed in a repo, the source file's first commit date.

If the exact day stays unverifiable after this ladder, use `YYYY-MM-01` (month verified) or `null`, and write the evidence chain in the PR body. Never guess a day.

## Podcast specifics

- Show ID discovery: `curl -s "https://itunes.apple.com/search?term=<show+name>&entity=podcast"` → `collectionId`. Episode listing: `https://itunes.apple.com/lookup?id=<collectionId>&media=podcast&entity=podcastEpisode&limit=200` gives `trackName`/`releaseDate`/`trackViewUrl`.
- The lookup returns only the ~200 most recent episodes. For older ones, fetch the Apple episode page directly (it shows the release date), or the show's RSS feed if it carries full history (Simplecast feeds often do).
- The RSS `feedUrl` from `lookup?id=<showId>` may redirect after a rebrand — follow redirects and match episodes by topic, not just title (see pitfalls).
- Early-access windows: some shows (Wondery+ like 10% Happier) release a week early on Spotify. Use the wide/Apple release date when the entry's canonical is the Apple link.
- Transcript-canonical lectures: an entry whose canonical is a transcript/annotation page (e.g. a Genius page) still takes the original talk's date, found via the official upload (e.g. YC Root Access uploaded CS183B lectures on lecture day).

## Fetch playbooks for hostile sites

- **403/Cloudflare (Medium, queue.acm.org, big news sites like Wired)**: not defunct. Try the FetchUrl tool, then a browser UA, then a Wayback snapshot of the same URL (`archive.org/wayback/available?url=...` → `archived_snapshots.closest.url`).
- **Medium** (via snapshot): prefer JSON-LD `datePublished` and visible date; treat `firstPublishedAt` (ms epoch) and `article:published_time` with suspicion — re-imports and updates pollute them. When fields conflict, the value that agrees with the visible byline wins.
- **YouTube**: curl gets 429'd quickly. Web-search the video ID (`"urarTyKn9cg"`) — the result snippet carries the upload date. oEmbed (`youtube.com/oembed?url=...`) gives title/channel but no date. For conference talks, the event's own site (sfelc.com, gotopia.tech) gives the event date; if those pages are gone, use the upload date and say so.
- **Wayback APIs rate-limit hard** (429s, multi-minute "Temporarily Offline" outages): space requests ~8s, back off ~25s on 429, fall back to CDX (or vice versa), and retry — outages so far have been transient. When both are down, leave the alternate/date unset and note it rather than guessing.
- **TLS/timeout failures** (some small blogs): try `--http1.1`, then the FetchUrl tool, then a Wayback snapshot.

## Trust pitfalls (all observed in production)

- **CMS migrations restamp metadata**: a personal site's og/JSON-LD can show a migration year (2025) on a 2015 essay. Sanity-check metadata against what the content says about itself (a "Happy 10th Birthday to X" essay dates itself from X's birthday, which the same site lists).
- **Wayback chrome leaks its own snapshot date** into visible-text scans. The snapshot timestamp is never the publication date; match the date adjacent to the article title/byline.
- **Related-posts sidebars pollute visible-date scans**: pages list other articles' dates. The post's own date sits next to its title/byline; multi-date pages (martinfowler.com) show a "published thru" history footer — take the earliest publication line.
- **Show rebrands retitle episodes and orphan old URLs**: art19 episode URLs 404 after the move to Omny ("Think Like an Economist" → "Platypus Economics"); the current feed shows new titles. Match by unique topic and curriculum position, use the surviving feed's date, and flag the title mismatch in the PR body.
- **Conflicting dates on one page** happen constantly: Medium `datePublished` (2017) vs `article:published_time` (update), tomtunguz og vs `<time>` from a widget. Pick the field that the visible byline corroborates; document the conflict.
- **Impossible dates**: if a postmortem is dated before the outage it describes, one of the fields is an artifact (a Medium import claiming the original blog's date). Use the timestamp that makes causal sense and flag the discrepancy.

## Bulk backfills

For repo-wide `published: null` sweeps, one PR per content file:

1. Inventory nulls by index (`python3` over `content/*.json`), and partition up front: URL-path dates, committed-index episodes, living references (stay null), needs-fetch.
2. Resolve in the order above; keep a per-entry evidence note for the PR body table.
3. Apply with an index-keyed Python script that asserts `published is None` before writing and asserts the expected living-null set afterward; then verify the diff touches only `published` lines (plus any intentional, separately-committed anomaly fixes).
4. `./validate-json.sh` and `./count-urls.sh` before committing; dates and anomaly fixes go in separate commits.
