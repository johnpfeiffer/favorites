---
name: add-favorite
description: Add a new link to the favorites JSON collection with verified metadata. Use when adding one or more URLs to content/*.json, including sleuthing published dates, canonical podcast URLs via the committed podcasts/ episode indexes, archive.org alternates, and reusing existing tags.
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
- There is no `type` field. Media type is a capitalized tag, conventionally last in the array. The complete set in use: `Podcast`, `Blog`, `Article`, `Video`, `Book`, `Paper`.
- First tag is the file's category (`AI`, `Business`, `Engineering`, `History`, `People`). Person/source tags exist (e.g. `Manager Tools`, `Charity Majors`, `Leslie Lamport`).
- Titles often carry a source prefix (`Manager Tools: ...`, `Honeycomb: ...`) or are descriptive (`Leslie Lamport interviewed about ...`). Match nearby entries.
- If the requester includes shorthand notes about a link, use them: they capture why the link matters. Enrich the title with that nuance when the canonical title doesn't convey it (paraphrase, don't copy verbatim), and let the notes steer tags and placement.

## Placement

Five files: `ai.json`, `business.json`, `engineering.json`, `history.json`, `people.json`. Filenames are loose; tags carry the meaning. To place a link, grep for similar existing content (e.g. all hiring/interviewing content is in `people.json`, all Acquired episodes in `history.json`) and follow the precedent.

## Metadata sleuthing

Published date, in order of preference:
1. URL path (`stackoverflow.blog/2026/06/26/...`), page byline, or MP3 filename (`career-tools-2024-12-19.mp3`).
2. Podcast RSS feed `pubDate` (see below). For shows in `podcasts/registry.json`, grep the committed episode index instead of downloading the feed.
3. Web search index date (when the page doesn't render one statically).

Canonical URL for an Apple Podcasts link:
1. **Check the episode index first.** `podcasts/<slug>.json` holds `{title, published, url}` for every episode of each show the collection uses repeatedly (SE Radio, Manager Tools, Lenny's, ELC, Go Time, ILTB, Knowledge Project, YC, Managing Up, Darknet Diaries, Radical Candor, Developing Leadership, Engineering Unblocked). Grep it for title keywords:
   ```bash
   jq -r '.episodes[] | select(.title | test("rumelt"; "i"))' podcasts/lennys-podcast.json
   ```
   The `canonicalPattern` field shows the site's URL shape; `notes` records quirks (Cloudflare 403s, parked domains, which mirror to prefer). Regenerate with `./update-podcast-indexes.sh` when an episode is newer than the index.
2. For shows not yet indexed: `curl -s "https://itunes.apple.com/lookup?id=<PODCAST_ID>"` returns `feedUrl` (the RSS feed) and the show name/author. Fetch the RSS feed, find the episode `<item>` by title, read its `<link>` and `<pubDate>`. If the show recurs in the collection, add it to `podcasts/registry.json` and regenerate.
3. To identify an episode from a bare Apple `?i=<EPISODE_ID>` when the title is unknown, web-search `"i=<EPISODE_ID>"` — the Apple episode page is usually the top hit with title and date.
4. Check whether the host publishes episodes on their own site (e.g. The Peterman Pod episodes are canonical on `developing.dev`, not anchor.fm); a web search for `<site> <episode keywords>` confirms. Prefer the author's own site as `url`, matching existing entries from the same show.
5. Put the original Apple Podcasts URL in `alternate-url`.

Archive.org alternate (when the Apple/mirror slot isn't taken):
`https://archive.org/wayback/available?url=<url-without-scheme>` returns the closest snapshot; use `archived_snapshots.closest.url` as `alternate-url` (https scheme).

## Tags

Reuse the existing vocabulary. Survey before inventing:

```bash
grep -oh '"<Candidate Tag>"' content/*.json | wc -l
```

Only create a tag when no existing one fits and the pattern is established (e.g. person tags for notable people).

## Checks before committing

```bash
./validate-json.sh   # jq syntax check on every content/*.json
./count-urls.sh      # per-file counts, total vs unique URLs, duplicate list
```

The new URL must not already exist (count-urls.sh lists duplicates).

## Anomalies and discrepancies

Adding a link often surfaces pre-existing issues: invalid JSON, duplicate URLs, wrong metadata, inconsistent tags. Always surface every anomaly in your response and the PR body. Then, by category:

- **Fix and report** when the correction is objectively verifiable: JSON syntax errors, tag typos with a clear dominant form in the repo, casing that breaks tag filtering, a wrong URL provable from matching metadata (e.g. an identical published date on the correct article).
- **Ask first** when the resolution is ambiguous, destructive, or unverifiable: deleting an entry, choosing between conflicting metadata, merging or removing existing tags, or any value you cannot confirm from the source (e.g. a published date the page doesn't show and search can't confirm).
- Keep anomaly fixes in their own commit, separate from the new links, so they are easy to review or drop.

## PR

Branch off latest `main` (`git checkout main && git pull --ff-only && git checkout -b <branch>`), commit with a short lowercase message, push, and open a PR with `gh pr create --base main`. The PR body should include a table of added links (URL, published date, file, tags) and notes on date sources and placement reasoning.
