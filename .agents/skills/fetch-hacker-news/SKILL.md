---
name: fetch-hacker-news
description: Fetch a Hacker News thread (story plus full comment tree) as HTML or JSON. Use when retrieving, reading, or archiving an HN discussion, counting its comments, or confirming a fetched thread is complete — covers the browser user-agent pattern, single-page thread rendering, comment-count reconciliation across HTML/Algolia/Firebase, and the favorites entry conventions for HN threads.
---

# Fetch Hacker News

Retrieve an HN thread (`https://news.ycombinator.com/item?id=<ID>`) reliably and verify the fetch is complete. HN is one of the easy sites: no bot-blocking, no JS rendering, no auth.

## HTML thread

Use a browser user-agent (the fetch playbook pattern: a browser UA keeps sites that gate on user-agent serving the same SSR HTML a browser would get; HN doesn't gate today, but the UA costs nothing and is insurance against a Cloudflare-style flip):

```bash
curl -s -A "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36" \
  "https://news.ycombinator.com/item?id=<ID>" -o thread.html
```

The **whole thread ships in one document** — an ~800-comment thread is a single ~1.2 MB page. Item pages do not paginate; only listing pages (front page, /newest, /ask) have `morelink`. Verify completeness instead of assuming it:

- `grep -c morelink thread.html` → 0 on a complete item page.
- The file ends with the standard footer (the `hn.algolia.com` search form), not mid-comment.
- `<title>` carries the headline with a ` | Hacker News` suffix.

## Counting comments

```bash
grep -o 'class="athing comtr' thread.html | wc -l
```

Match the `class="athing comtr` *prefix*, not the exact string: downranked comments render as `comtr noshow` and collapsed threads as `comtr coll`, and both are real comments (one thread: 813 exact matches vs 822 with variants). The story row is `athing submission`, so comtr counts are pure comments.

Reconcile counts across sources with a tolerance, not an exact match. On one 800-comment verification thread: page header said 821, rendered rows 822, Algolia tree 822, Firebase `descendants` 824. `descendants` counts dead/deleted comments that the renderer and Algolia drop, and the stored header counter lags moderation. Deltas of ±~1% are dead/deleted/moderation artifacts, not a truncated fetch; the real truncation signal is a big shortfall *plus* a `morelink` in the page.

## JSON tree (Algolia, preferred)

```bash
curl -s "https://hn.algolia.com/api/v1/items/<ID>"
```

One request returns the story plus the full nested comment tree (`children`, each with `author`, `text` as HTML, `created_at`). No auth, generous rate limits. The story's `created_at` is the submission timestamp — use its date part for `datePublished`.

To find a thread id from a story URL or title: `https://hn.algolia.com/api/v1/search?query=<url-or-title>` (the same endpoint resolve-published-date uses for HN corroboration of undated pages).

## Official Firebase API (fallback)

`https://hacker-news.firebaseio.com/v0/item/<ID>.json` returns one item per request; assembling a comment tree needs recursive `kids` traversal, so prefer Algolia for whole threads. Firebase is still useful for two story fields: `descendants` (total comment count including dead) and `time` (submission time as unix seconds).

## Entry conventions (favorites collection)

- Name: `HN Discussion: <headline>`, and HN threads conventionally carry **no media-type keyword** (per add-favorite), so `@type` falls back to `Article`. Apply the usual name sanitizing (no double quotes, `and` for `&`).
- `datePublished`: the submission date (Algolia `created_at`), never the fetch date. resolve-published-date owns date semantics and precision.
- `archivedAt`: a Wayback capture of the item URL, via the normal add-favorite flow; HN pages archive cleanly.
- Threads keep accruing comments after you fetch them, so saved HTML goes stale: for a durable record prefer the Wayback snapshot, or note the fetch date in the PR body.
