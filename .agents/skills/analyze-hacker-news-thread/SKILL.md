---
name: analyze-hacker-news-thread
description: Semantically classify every comment in a Hacker News thread to surface astroturf-pattern cheerleading, the most insightful commenters, or signal-vs-noise around a reader-chosen focus. Use when analyzing an HN discussion for shill/bot patterns, undisclosed-affiliation praise, topic-focused insight mining (e.g. employers/hiring/management), or building a favorite-commenters list — covers the flatten-chunk-classify pipeline, the vacuous-cheer taxonomy, author-level affiliation and insider propagation, cross-thread repeat-offender matching, and account-age/karma enrichment.
---

# Analyze a Hacker News thread

Classify every comment in a large HN thread so the reader can skip to what they care about. Two modes, same pipeline:

- **Launch/vendor threads** (model releases, product launches): separate vacuous cheer, promotional boosting, and affiliated voices from genuine insight. Outputs: `astroturf.json` + `insightful.json`.
- **Signal-to-noise threads** (layoffs, policy, industry news): the reader names focus topics (e.g. tech_industry, employers, hiring, management); classify `insightful` with a `topics` list, and bucket noise as `noise_culture_war` (flame wars per the reader's exclusions) and `noise_vacuous` (empty hot takes, doom one-liners, low-effort snark). Outputs: `insightful.json` (focus-first) + `noise.json`.

Output lives in `HN/<item-id>/`. Fetching is owned by the fetch-hacker-news skill; use its Algolia `items` endpoint.

## Flatten

Walk the Algolia tree into JSONL, one comment per line: `id, author, created_at, depth, parent_author, text` (HTML stripped, links reduced to URLs). Trust a JSON parser, never regex or displayed bytes, for author fields — on thread 50073879, two comments' usernames rendered as asterisks through shell/display tools while the parsed values matched the live HN HTML; the parser is the source of truth.

## Classify in parallel chunks

Round-robin the JSONL into plain-text chunk files of ~190-205 comments (4 chunks for a ~700-820-comment thread, 6 for ~1150), one `### id=... author=... depth=... created_at=...` header per comment, and classify with parallel workers. Round-robin (not contiguous slices) spreads each subthread across chunks so no worker sees a biased run.

Launch-thread categories (a comment can carry several):

- `cheerful_anecdote` — the vacuous-cheer pattern, in sharpened form (bake all of these into pass 1; with them included, the Carbon thread needed no recall pass): first-person "tried it, it was good" with no concrete task; marketing-quote amplification (quoting the launch blurb with amazement — a token hedge like "if it works" does NOT neutralize it); educated-sounding empty comparisons ("on par with X but without the X-isms"); evidence-free competitor-nerf contrasts ("they nerfed Sol 6.1", implicitly flattering the new model); low-effort agreement replies amplifying any of these.
- `boosting` — promotional cheer/talking-points without the anecdote frame.
- `disclosed_googler` — states or asserts affiliation/insider knowledge. (Generalize to the vendor the thread is about.)
- `insightful` — concrete tasks, numbers, detailed comparisons, domain expertise. Specificity is the test: positive + specific is insightful, NOT cheerful_anecdote; positive + vague is the pattern.

Signal-to-noise runs replace the cheer categories with topic-tagged insightful plus the two noise buckets. The critical nuance rule: sober, specific policy discussion (visa-fee mechanics with numbers, WARN Act rules) is insightful, NOT culture war — culture war is team-sport grievance, inflammatory generalizations, and replies that just keep a grievance loop spinning. On the MS-buyouts thread the culture-war volume was 128 comments but remarkably concentrated: one inclusive-language subthread and one offshoring grievance cluster.

Workers return only flagged comments as JSON (`id, author, categories, topics (when insightful), confidence, why, signals`); "other" comments are dropped. Signals vocabulary: `early_comment` (≤2h after posting), `top_level`, `low_specificity`, `random_trial_framing`, `cheer_tone`, `talking_points`, `marketing_quote`, `nerf_contrast`, `empty_comparison`, `agree_reply`, `affiliation_disclosure`, `concrete_detail`, `critical_of_google`, `affiliated_author`, `pre_disclosure`, plus signal-side signals for S/N runs: `first_hand_ms` (or whichever org), `first_hand_layoff`, `numbers_data`, `policy_specifics`, `management_detail`, `hiring_detail`, `industry_analysis`, `actionable_advice`.

## Pass 2: author-level propagation (do not skip)

Per-comment classification has no memory, and that misses patterns that only exist at the author level. After pass 1:

- **Affiliation propagation** (launch threads): for each affiliated author compute their first-disclosure timestamp, tag all their comments `affiliated_author`, tag anything posted before it `pre_disclosure`. On 50073879 this caught a Googler whose two positive testimonials sat undisclosed for 7.5 hours before the "I work at teh Goog" disclaimer.
- **Insider propagation** (S/N threads): the same mechanic boosts signal, not just suspicion — on the MS-buyouts thread, self-identified current/former employees (`ms_insider`) produced the richest comments (the manager who delivered the performance terminations, leaked severance terms). Verify regex disclosure candidates by reading the sentence: third-person mentions ("arrogant Microsoft employees") are false positives; "Ex-MSFT here" and "we were told in an internal keynote" are real.
- **Rescan**: re-read every unflagged comment by every flagged author with the author context (a small set — most flagged authors comment once).

## Pass 3: recall pass (as needed)

Users will spot missed comments in the output — treat each as a definition bug, not a one-off. Sharpen the category definition with the example, then re-run all chunks asking workers for ONLY additional candidates (give them the already-flagged id list). On 50073879 the recall pass grew the file from 48 to 68 comments. Once the sharpened definitions are in pass 1, this pass is only for genuinely novel patterns.

## Pass 4: cross-thread repeat-offender matching (launch threads)

Match flagged authors against every prior `HN/<id>/` directory. A `repeat_argon_flag`-style signal on the same rhetorical pattern in two launches of one product line is the strongest sockpuppet evidence available (LewisOrton: account created 2 days before the Argon launch, vacuous cheer in both Argon and Carbon). But calibrate honestly: 10 of 11 Argon-flagged authors who reappeared in Carbon posted normally — flags are per-comment behavior, not a scarlet letter. Report repeaters and non-repeaters both.

## Enrich with account metadata

Fetch `https://news.ycombinator.com/user?id=<name>` (browser UA, 1-1.2s spacing) for flagged authors and, on S/N runs, top insightful authors (≥2 insightful or high-confidence). `front?day=YYYY-MM-DD` in the created-cell href gives the ISO account birthday, `karma:</td><td>N` the karma. HN rate-limits past ~60 rapid requests (null pages, not errors) — retry failures at 2.5s spacing, which recovers nearly all (92/96, 80/82 on two runs). Never let unresolved profiles into the young-account list: a null `account_created` with a sentinel comparison silently includes them (bit me on Carbon — `None` mapped to '9999' passed the `>=` test). Account age is the cleanest bot-vs-fanboy discriminator: same-week-of-launch accounts with single-digit karma are the sockpuppet cluster; vacuous cheer from decade-old accounts is fanboy drive-by culture. Also compute per-author comment counts in-thread (roughly 2/3 of authors comment exactly once on a big thread — normal).

## Outputs (`HN/<item-id>/`)

- `astroturf.json` (launch threads) — thread meta, methodology note, summary (counts by category/confidence, `pre_disclosure_comments` index, young-account list, repeat-offender list, comparison with sibling-launch threads), and flagged comments with full text, signals, why, and account metadata. Sort: disclosed first, then anecdote, then boosting; confidence high→low.
- `insightful.json` — same shape plus an `author_index` (author → comment ids, most prolific first) for favorite-commenters graph building. On S/N runs, sort focus-topics first and include a topic distribution in the summary.
- `noise.json` (S/N threads) — the excluded buckets with counts and a share-of-thread ratio, so the reader can see what was filtered without wading through it (19% noise on the MS thread).

Conventions that keep the files honest: dual-listing (a comment can be insightful AND a noise/cheer pattern; both files carry it, categories say so), confidence is per-entry and low means "pattern-shaped guess", and the methodology field records every pass so the file explains itself. Frame outputs as rhetorical patterns, never as proof an account is a bot.
