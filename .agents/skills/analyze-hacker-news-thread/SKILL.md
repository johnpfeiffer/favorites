---
name: analyze-hacker-news-thread
description: Semantically classify every comment in a Hacker News thread to surface astroturf-pattern cheerleading and the most insightful commenters. Use when analyzing an HN discussion for shill/bot patterns, undisclosed-affiliation praise, or building a favorite-commenters list — covers the flatten-chunk-classify pipeline, the vacuous-cheer taxonomy, author-level affiliation propagation (pre_disclosure), recall passes, and account-age/karma enrichment.
---

# Analyze a Hacker News thread

Classify every comment in a large HN thread to separate three things people conflate: coordinated-looking vacuous cheer, genuine enthusiasm with substance, and affiliated (employed-by) voices. Output is two JSON files per thread, stored in `HN/<item-id>/`. Fetching is owned by the fetch-hacker-news skill; use its Algolia `items` endpoint for the structured tree.

## Flatten

Walk the Algolia tree into JSONL, one comment per line: `id, author, created_at, depth, parent_author, text` (HTML stripped, links reduced to URLs). Trust a JSON parser, never regex or displayed bytes, for author fields — on thread 50073879, two comments' usernames rendered as asterisks through shell/display tools while the parsed values matched the live HN HTML; the parser is the source of truth.

## Classify in parallel chunks

Round-robin the JSONL into 4 plain-text chunk files (~205 comments each, one `### id=... author=... depth=... created_at=...` header per comment) and classify with parallel workers. Round-robin (not contiguous slices) spreads each subthread across chunks so no worker sees a biased run.

Categories (a comment can carry several):

- `cheerful_anecdote` — first-person "tried it, it was good" with no concrete task. Sharpened by user feedback, the vacuous-cheer pattern also covers: marketing-quote amplification (quoting the launch blurb with amazement — a token hedge like "if it works" does NOT neutralize it), educated-sounding empty comparisons ("on par with X but without the X-isms"), evidence-free competitor-nerf contrasts ("they neutered Sol 6.1", implicitly flattering the new model), and low-effort agreement replies amplifying any of these.
- `boosting` — promotional cheer/talking-points without the anecdote frame.
- `disclosed_googler` — states or asserts affiliation/insider knowledge. (Generalize to whichever vendor the thread is about.)
- `insightful` — concrete tasks, numbers, detailed comparisons, domain expertise. Specificity is the test: positive + specific is insightful, NOT cheerful_anecdote. Positive + vague is the pattern.

Workers return only flagged comments as JSON (`id, author, categories, confidence, why, signals`); "other" comments are dropped. Signals vocabulary: `early_comment` (≤2h after posting), `top_level`, `low_specificity`, `random_trial_framing`, `cheer_tone`, `talking_points`, `marketing_quote`, `nerf_contrast`, `empty_comparison`, `agree_reply`, `affiliation_disclosure`, `concrete_detail`, `critical_of_google`, `affiliated_author`, `pre_disclosure`.

## Pass 2: author-level affiliation propagation (do not skip)

Per-comment classification has no memory, and that misses the strongest shill pattern: praise that reads as unaffiliated because the disclosure comes later. After pass 1, for each affiliated author compute their first-disclosure timestamp, tag all their comments with `affiliated_author`, and tag anything posted before the disclosure with `pre_disclosure`. Then re-read every unflagged comment by every flagged author (a small set — most flagged authors comment once) and classify with the author context. On 50073879 this caught a Googler whose two positive testimonials sat undisclosed for 7.5 hours before the "I work at teh Goog" disclaimer.

## Pass 3: recall pass

Users will spot missed cheerleaders in the output — treat each as a definition bug, not a one-off. Sharpen the category definition with the example, then re-run all chunks asking workers for ONLY additional candidates (give them the already-flagged id list). On 50073879 the recall pass grew the file from 48 to 68 comments, all from established accounts — the miss pattern was hedged amplification, not new accounts.

## Enrich with account metadata

Fetch `https://news.ycombinator.com/user?id=<name>` (browser UA, 1s spacing) for flagged authors: `front?day=YYYY-MM-DD` in the created-cell href gives the ISO account birthday, `karma:</td><td>N` the karma. Account age is the cleanest bot-vs-fanboy discriminator: same-week-of-launch accounts with single-digit karma are the sockpuppet cluster; vacuous cheer from decade-old accounts is fanboy drive-by culture. Also compute per-author comment counts in-thread (429 of 571 authors commented exactly once on 50073879 — normal for a big launch).

## Outputs (`HN/<item-id>/`)

- `astroturf.json` — thread meta, methodology note, summary (counts by category/confidence, `pre_disclosure_comments` index, young-account list), and flagged comments with full text, signals, why, and account metadata. Sort: disclosed first, then anecdote, then boosting; confidence high→low.
- `insightful.json` — same shape plus an `author_index` (author → comment ids, most prolific first) for favorite-commenters graph building.

Conventions that keep the files honest: dual-listing (a comment can be insightful AND a cheer pattern; both files carry it, categories say so), confidence is per-entry and low means "pattern-shaped guess", and the methodology field records every pass so the file explains itself. Frame outputs as rhetorical patterns, never as proof an account is a bot.
