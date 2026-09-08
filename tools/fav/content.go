package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// entry is one favorites record. The on-disk form is content/*.jsonld
// (schema.org ItemList); the field names and JSON tags here follow the
// JSON-LD properties so printed records read like the stored ones.
type entry struct {
	Name          string   `json:"name"`
	URL           string   `json:"url"`
	ArchivedAt    string   `json:"archivedAt,omitempty"`
	DatePublished *string  `json:"datePublished"`
	Keywords      []string `json:"keywords"`

	File  string `json:"-"` // source file basename, e.g. content/engineering.jsonld
	Index int    `json:"-"` // position within the file's itemListElement array
}

// contentStore is every entry in the collection, in stable file+index order.
type contentStore struct {
	entries []entry
}

// itemList mirrors the content/*.jsonld envelope: a schema.org ItemList whose
// itemListElement array holds one object per entry, in order.
type itemList struct {
	Type  string          `json:"@type"`
	Name  string          `json:"name"`
	Items []jsonldElement `json:"itemListElement"`
}

// jsonldElement mirrors one stored entry. The entry's @type (PodcastEpisode,
// BlogPosting, ...) is structural metadata the CLI does not inspect — the
// media-type keyword carries that meaning for lint and tagging.
type jsonldElement struct {
	Name          string   `json:"name"`
	URL           string   `json:"url"`
	ArchivedAt    string   `json:"archivedAt"`
	DatePublished *string  `json:"datePublished"`
	Keywords      []string `json:"keywords"`
}

func (e jsonldElement) toEntry(file string, index int) entry {
	return entry{
		Name:          e.Name,
		URL:           e.URL,
		ArchivedAt:    e.ArchivedAt,
		DatePublished: e.DatePublished,
		Keywords:      e.Keywords,
		File:          file,
		Index:         index,
	}
}

func loadContent(dir string) (*contentStore, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonld"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("no .jsonld files found in %s", dir)
	}
	s := &contentStore{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var list itemList
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		base := filepath.Base(f)
		for i := range list.Items {
			s.entries = append(s.entries, list.Items[i].toEntry(base, i))
		}
	}
	return s, nil
}
