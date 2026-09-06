# migrate-to-schema-org

Standalone Go tool using only the standard library. Run from the repository root:

```sh
go run tools/migrate-to-schema-org/main.go dry-run
go run tools/migrate-to-schema-org/main.go convert
go run tools/migrate-to-schema-org/main.go verify
```

Or build a binary:

```sh
go build -o /tmp/migrate-to-schema-org tools/migrate-to-schema-org/main.go
/tmp/migrate-to-schema-org dry-run
```

All commands accept `--input DIR` (default `content`) and `--output DIR`
(default the input directory). `convert --dry-run` is equivalent to `dry-run`.
Each source must contain exactly one category array. The tool reads `*.json`
and produces matching `*.jsonld` filenames containing a schema.org `ItemList`.
It does not fetch URLs or infer metadata from page contents.

`convert` preserves source files and replaces existing JSON-LD outputs on repeat
runs. It validates all sources before writing, then replaces each output
atomically. A filesystem failure can leave some outputs updated; rerun to finish.
`dry-run` writes nothing, reports entry counts and Article fallback counts, and
lists every entry with multiple distinct recognized media types, including its
selected type. `verify` compares generated files against the source conversion,
including every value and array order; JSON whitespace and object key order do
not affect verification. Missing, invalid, or mismatched outputs cause a nonzero
exit status. Unrelated files in the output directory are left alone.

## Mapping

| Source | JSON-LD |
| --- | --- |
| Category key | ItemList `name` |
| `title` | `name` |
| `url` | `url` |
| `alternate-url` | `archivedAt` (all alternates, including mirrors) |
| `published` | `datePublished` |
| `tags` | `keywords` |

Entry order, keyword order, duplicate keywords, strings, nulls, and absent
optional fields are preserved. Unknown fields are rejected so metadata cannot
silently disappear. Output uses two-space indentation and a trailing newline.

## Types

| Exact tag | Type |
| --- | --- |
| Podcast | PodcastEpisode |
| Blog | BlogPosting |
| Article | Article |
| Video | VideoObject |
| Book | Book |
| Paper | ScholarlyArticle |
| TechArticle | TechArticle |

All tags are inspected, wherever they appear in the array. When Book, Blog, and
Article compete, the preference is `Book > BlogPosting > Article`. Independent
types are retained in original tag order: Book + Podcast produces
`["Book", "PodcastEpisode"]`; Paper + Video produces
`["ScholarlyArticle", "VideoObject"]`. Duplicate types are emitted once, without
removing duplicate keywords. A single type is a string; multiple types are an
array. Entries with no recognized media tag fall back to `Article`.

Book framing is respected even when the URL points to a companion article.
TechArticle requires an explicit tag; technical subject matter alone does not
trigger reclassification. No source tags are edited by the tool.

## Tests

```sh
go test tools/migrate-to-schema-org/main.go tools/migrate-to-schema-org/main_test.go
```
