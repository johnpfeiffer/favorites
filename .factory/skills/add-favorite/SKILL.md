---
name: add-favorite
description: Add a new link to the favorites JSON collection with verified metadata. Use when adding one or more URLs to content/*.json, including canonical podcast URLs via the committed podcasts/ episode indexes, archive.org alternates, and reusing existing tags. Published-date resolution is owned by the resolve-published-date skill.
---

# Add a favorite

Add links to `content/*.json` so they pass `./validate-json.sh`, dedupe cleanly against `./count-urls.sh`, and match existing conventions.

## Entry schema

```json
{
  "title": "Source: Title or descriptive title",
  "url": "https://canonical-url",
  "alternate-url": "https://web.archive.org/web/<ts>/<url> or podcast mirror",
  "published": "YYYY-MM-DD or null",
  "tags": ["<FileCategory>", "Tag", "Podcast|Blog|Article|Video|Book|Paper"]
}
```

- `alternate-url` is optional; omit it when none exists. `published` is ISO `YYYY-MM-DD` or `null`.
- `published` records the initial distribution/event date, not the file or upload timestamp. For blogs and web articles the two are highly correlated, but for videos of live events (conference talks, panels, recorded meetups) there can be a lag between the event and the upload — use the event date when known (month-precision `YYYY-MM-01` when only the month is verifiable), and note the upload date in the PR body. Resolve dates with the `resolve-published-date` skill, which owns the full evidence ladder (URL path → podcast indexes → page metadata → Wayback → Crossref → HN) and the precision rules.
- `alternate-url` usually holds an archive.org snapshot or podcast mirror, but can hold a canonical reference page (e.g. Wikipedia for a book) when the requester asks for it.
- When the original link is defunct (dead site, error page, or redirect to unrelated content), flip the two fields: the archive.org snapshot becomes the canonical `url` and the original source link becomes the `alternate-url`. Flag the defunct link in the PR body.
- There is no `type` field. Media type is a capitalized tag, conventionally last in the array. The complete set in use: `Podcast`, `Blog`, `Article`, `Video`, `Book`, `Paper`.
- First tag is the file's category (`AI`, `Business`, `Engineering`, `History`, `People`). Person/source tags exist (e.g. `Manager Tools`, `Charity Majors`, `Leslie Lamport`).
- Titles often carry a source prefix (`Manager Tools: ...`, `Honeycomb: ...`) or are descriptive (`Leslie Lamport interviewed about ...`). Match nearby entries.
- The UI auto-appends the entry's year from `published`, so don't add year parentheticals like `(1982 lecture)` to titles.
- Hacker News threads use the prefix `HN Discussion: <headline>` and conventionally carry no media-type tag.
- If the requester includes shorthand notes about a link, use them: they capture why the link matters. Enrich the title with that nuance when the canonical title doesn't convey it (paraphrase, don't copy verbatim), and let the notes steer tags and placement.
- An entry can be framed as a `Book` even when the canonical URL is the author's companion article about it: title and tags reflect the book, `published` is the book's release date, and the framing is noted in the PR body.

## Placement

Five files: `ai.json`, `business.json`, `engineering.json`, `history.json`, `people.json`. Filenames are loose; tags carry the meaning. To place a link, grep for similar existing content (e.g. all hiring/interviewing content is in `people.json`, all Acquired episodes in `history.json`) and follow the precedent.

## Metadata sleuthing

**Published dates**: use the `resolve-published-date` skill. It owns the evidence ladder (URL path → committed podcast indexes → page metadata → Wayback snapshot forensics → Crossref → bibliographic records → HN corroboration), the semantics (initial distribution/event date, original airing over re-release), the precision rules (exact day → `YYYY-MM-01` month fallback → `null` with evidence), bot-block fetch workarounds, and the living-reference `null` convention (blog homepages, index pages, continuously revised docs, Wikipedia articles, GitHub READMEs).

Canonical URL and `alternate-url` work stays here. Before trusting a URL, verify the live page still serves the expected content: when the original link is defunct (a 301 redirect to unrelated content, an error page, or a JS-heavy page that renders blank), use the Wayback snapshot as the canonical `url`, put the original source link in `alternate-url`, and flag it in the PR.

Distinguish bot-blocking from defunct. A 403 or a Cloudflare "Attention Required" page (NYT, Medium, hbr.org, and nsa.gov all bot-block curl) usually means the site is alive but refusing non-browser clients — retry with FetchUrl or a browser user-agent, and keep the original as canonical (paywalled-but-live stays canonical too; the Wayback snapshot goes in `alternate-url`). Only flip fields when the content is actually gone: 404, dead or parked domain, redirect to unrelated content. When a link 404s, first web-search the article title for a moved canonical on the same site (publishers re-slug posts, and shows rebrand — e.g. an art19 show moved to omny.fm); use the moved URL as canonical if it serves the content, and only apply the defunct flip if the moved URL is dead too. If a slug now serves a retitled version of the same article, keep the URL and reflect the current page title (the submitter's framing can survive as a parenthetical).

The Wayback APIs occasionally return 503s for long stretches. Retry once or twice, then omit the unverified alternates rather than guessing, note the outage in the PR body, and backfill later. When the API does cooperate, add the latest 200 capture as `alternate-url` even for healthy live links — sites rot eventually.

Canonical URL for an Apple Podcasts link:
1. **Check the episode index first.** `podcasts/<slug>.json` holds `{title, published, url}` for every episode of each show the collection uses repeatedly (SE Radio, Manager Tools, Lenny's, ELC, Go Time, ILTB, Knowledge Project, YC, Managing Up, Darknet Diaries, Radical Candor, Developing Leadership, Engineering Unblocked). Grep it for title keywords:
   ```bash
   jq -r '.episodes[] | select(.title | test("rumelt"; "i"))' podcasts/lennys-podcast.json
   ```
   The `canonicalPattern` field shows the site's URL shape; `notes` records quirks (Cloudflare 403s, parked domains, which mirror to prefer). Regenerate with `./update-podcast-indexes.sh` when an episode is newer than the index.
2. For shows not yet indexed: `curl -s "https://itunes.apple.com/lookup?id=<PODCAST_ID>"` returns `feedUrl` (the RSS feed) and the show name/author. Fetch the RSS feed, find the episode `<item>` by title, read its `<link>` and `<pubDate>`. If the show recurs in the collection, add it to `podcasts/registry.json` and regenerate.
3. The fastest path from a video or other mirror to the Apple episode link: find the show with `curl -s "https://itunes.apple.com/search?term=<show+name>&entity=podcast"` (returns `collectionId`), then list episodes with `curl -s "https://itunes.apple.com/lookup?id=<collectionId>&entity=podcastEpisode&limit=200"` — each result has `trackName`, `releaseDate`, and `trackViewUrl` (the Apple episode URL; strip the `&uo=4` suffix). Two gotchas: auto-generated transcripts mangle show names (a transcript said "Gradient Descent" for the real "Gradient Dissent"), so confirm the show name via the search API; and the podcast episode title often differs from the video title.
4. To identify an episode from a bare Apple `?i=<EPISODE_ID>` when the title is unknown, web-search `"i=<EPISODE_ID>"` — the Apple episode page is usually the top hit with title and date.
5. Check whether the host publishes episodes on their own site (e.g. The Peterman Pod episodes are canonical on `developing.dev`, not anchor.fm); a web search for `<site> <episode keywords>` confirms. Prefer the author's own site as `url`, matching existing entries from the same show.
6. Put the original Apple Podcasts URL in `alternate-url`.

Archive.org alternate (when the Apple/mirror slot isn't taken):
`https://archive.org/wayback/available?url=<url-without-scheme>` returns the closest snapshot; use `archived_snapshots.closest.url` as `alternate-url` (https scheme).

The availability API rate-limits aggressively (429s even with spacing between requests). Fallback: the CDX API, which also answers "earliest snapshot" for dating undated pages:

```bash
curl -s --get --data-urlencode "url=<url-without-scheme>" \
  --data "output=json&limit=-1&filter=statuscode:200" \
  "https://web.archive.org/cdx/search/cdx"
```

`limit=-1` returns the latest snapshot, `limit=1` the earliest. Always pass the URL via `--data-urlencode`, throttle to one request every few seconds, and retry variants (with/without `www`, trailing slash) when a lookup comes back empty.

## Tags

Reuse the existing vocabulary. Survey before inventing:

```bash
grep -oh '"<Candidate Tag>"' content/*.json | wc -l
```

Only create a tag when no existing one fits and the pattern is established (e.g. person tags for notable people).

Tag generously within the established patterns: an interview or talk earns a person tag for its subject (`Adrian Cockcroft`), a link centered on a company earns the company tag (`FireworksAI`, `Palantir`), and the requester's shorthand notes often name the precise topic tag (`Forward Deployed Engineering`, `Probability`). Person and company tags go after topic tags, before the media-type tag.

## Importing old-format exports

When the requester pastes records from an older export (`{"url", "title", "type", "tags"}` with string-valued tags), convert each record to the current schema: `type` becomes the trailing media-type tag, `tags` strings map onto the existing vocabulary (e.g. `Software Engineering` → `Engineering`, `History/Computing` → `History` plus specific tags), and titles get normalized to the title conventions above (fix submitter typos, e.g. "crockford" → Cockcroft). Every imported entry still goes through the full metadata sleuthing — don't trust the export's dates, which it usually lacks anyway.

## Graph

When the repo has `graph/` and the `maintain-favorites-graph` skill, extract evidence-backed entities and edges from the new entries in a separate commit on the same branch: `Founder_of`, `Author_of`, `Host_of`, and employment edges only when the linked source explicitly supports them. Interviewees and talk speakers are not authors of the interview article. List plausible entities you omitted for lack of evidence in the PR body, and run `python3 .agents/skills/maintain-favorites-graph/scripts/validate_graph.py .` before committing.

## Checks before committing

```bash
./validate-json.sh   # jq syntax check on every content/*.json
./count-urls.sh      # per-file counts, total vs unique URLs, duplicate list
```

The new URL must not already exist (count-urls.sh lists duplicates). Grep `content/` for the URL and title keywords before adding; list any skipped duplicates in the PR body.

## Anomalies and discrepancies

Adding a link often surfaces pre-existing issues: invalid JSON, duplicate URLs, wrong metadata, inconsistent tags. Always surface every anomaly in your response and the PR body. Then, by category:

- **Fix and report** when the correction is objectively verifiable: JSON syntax errors, tag typos with a clear dominant form in the repo, casing that breaks tag filtering, a wrong URL provable from matching metadata (e.g. an identical published date on the correct article).
- **Ask first** when the resolution is ambiguous, destructive, or unverifiable: deleting an entry, choosing between conflicting metadata, merging or removing existing tags, or any value you cannot confirm from the source (e.g. a published date the page doesn't show and search can't confirm).
- Keep anomaly fixes in their own commit, separate from the new links, so they are easy to review or drop.

## PR

Branch off latest `main` (`git checkout main && git pull --ff-only && git checkout -b <branch>`), commit with a short lowercase message, push, and open a PR with `gh pr create --base main`. The PR body should include a table of added links (URL, published date, file, tags) and notes on date sources and placement reasoning.

Open the PR early as a draft when the requester wants the intent captured up front (or when a batch will take a while): create the branch, `git commit --allow-empty` a marker commit, push, and `gh pr create --draft --base main` with a body that records the intent — the submitted links with the requester's annotations, the planned handling per link, and a checklist (resolve → dedupe → tags/placement → graph → validators). Push real commits as they land, and replace the intent section with the final evidence tables before marking the PR ready for review.

- Push auth: `git config credential.helper "!gh auth git-credential"` reuses the authenticated `gh` session; set a repo-local `user.name`/`user.email` if git has no identity.
- If the previous PR was merged and its branch deleted before you push follow-up commits, pushing recreates a stray branch. Instead cherry-pick the commits onto a fresh branch off `origin/main`, open a new PR, and delete the stray branch after verifying by diff that it holds nothing unique.
