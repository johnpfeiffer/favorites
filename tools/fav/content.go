package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// entry mirrors the content/*.json record schema exactly.
type entry struct {
	Title        string   `json:"title"`
	URL          string   `json:"url"`
	AlternateURL string   `json:"alternate-url,omitempty"`
	Published    *string  `json:"published"`
	Tags         []string `json:"tags"`

	File  string `json:"-"` // source file basename, e.g. content/engineering.json
	Index int    `json:"-"` // position within the file's array
}

// contentStore is every entry in the collection, in stable file+index order.
type contentStore struct {
	entries []entry
}

func loadContent(dir string) (*contentStore, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("no .json files found in %s", dir)
	}
	s := &contentStore{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		// Each content file is {"Category": [entries...]}.
		var byCategory map[string][]entry
		if err := json.Unmarshal(raw, &byCategory); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, entries := range byCategory {
			for i := range entries {
				entries[i].File = filepath.Base(f)
				entries[i].Index = i
				s.entries = append(s.entries, entries[i])
			}
		}
	}
	return s, nil
}
