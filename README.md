# favorites
The source of John's favorites 😹

JSON format of urls, a title/desc, tags, and published date if available

The filenames are general categories (top level json keys) for human sensibilities but the tags are more useful. 😅

Archive links used when the originals are unavailable. 🙏

## parallel evolution

I've been collecting these for years and didn't realize a much more famous resource exists (and there's some overlap, but not as much as you'd think).

Since the goal is accumulating and sharing...

<https://github.com/charlax/professional-programming>

<https://github.com/charlax/engineering-management>

<https://github.com/charlax/entrepreneurship-resources>

# Podcast indexes

`podcasts/` holds a registry of the podcasts that recur in this collection (`registry.json`) plus a generated episode index per show (`<slug>.json`: title, published date, canonical URL for every episode). Regenerate after adding new episodes or shows:

```bash
./update-podcast-indexes.sh
```

To resolve an Apple Podcasts link to its canonical page, grep the matching show's index instead of re-downloading RSS feeds.

# The app

<https://feneky.com/links> for more advanced features like filtering by tags and whatnot. 🦊

_(originally started as a simple list at <https://blog.john-pfeiffer.com>)_
