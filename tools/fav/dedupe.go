package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// dedupeMatch is a stored entry whose url or archivedAt equals the input
// after normalization (scheme/www/trailing-slash/tracking-params/Wayback
// wrapping all ignored).
type dedupeMatch struct {
	Field string `json:"field"` // "url" or "archivedAt"
	File  string `json:"file"`
	Index int    `json:"index"`
	Entry entry  `json:"entry"`
}

// nameCandidate is a stored entry whose name lexically overlaps the
// optional name keywords given with the input.
type nameCandidate struct {
	Score float64 `json:"score"`
	File  string  `json:"file"`
	Index int     `json:"index"`
	Entry entry   `json:"entry"`
}

type dedupeResult struct {
	Input          string          `json:"input"`
	Name           string          `json:"name,omitempty"`
	Status         string          `json:"status"` // url-match | archivedAt-match | name-only | none
	Matches        []dedupeMatch   `json:"matches,omitempty"`
	NameCandidates []nameCandidate `json:"nameCandidates,omitempty"`
}

func cmdDedupe(args []string) error {
	fs := flag.NewFlagSet("dedupe", flag.ContinueOnError)
	contentDir := fs.String("content", "content", "path to the content directory")
	asJSON := fs.Bool("json", false, "emit a JSON array instead of text")
	minScore := fs.Float64("min-score", 0.6, "minimum name token containment for name candidates (0-1)")
	maxCandidates := fs.Int("max-candidates", 5, "maximum name candidates per input")
	if err := fs.Parse(args); err != nil {
		return err
	}
	inputs, err := readInputs(fs.Args())
	if err != nil {
		return err
	}
	store, err := loadContent(*contentDir)
	if err != nil {
		return err
	}

	// Pre-normalize stored URLs once.
	storedNorm := normalizeStore(store)

	results := make([]dedupeResult, 0, len(inputs))
	for _, in := range inputs {
		results = append(results, dedupeOne(store, storedNorm, in, *minScore, *maxCandidates))
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(results)
	}
	for i, res := range results {
		if i > 0 {
			fmt.Println()
		}
		printDedupeResult(os.Stdout, res)
	}
	return nil
}

func printDedupeResult(w io.Writer, res dedupeResult) {
	fmt.Fprintf(w, "INPUT  %s\n", res.Input)
	if res.Name != "" {
		fmt.Fprintf(w, "NAME   %s\n", res.Name)
	}
	fmt.Fprintf(w, "STATUS %s\n", res.Status)
	for _, m := range res.Matches {
		fmt.Fprintf(w, "MATCH  field=%s %s[%d]\n", m.Field, m.File, m.Index)
		printEntryJSON(w, m.Entry, "  ")
	}
	for _, c := range res.NameCandidates {
		fmt.Fprintf(w, "NAME?  score=%.2f %s[%d]\n", c.Score, c.File, c.Index)
		printEntryJSON(w, c.Entry, "  ")
	}
}

// normalizeStore pre-computes normalized [url, archivedAt] per entry.
func normalizeStore(store *contentStore) [][2]string {
	storedNorm := make([][2]string, len(store.entries))
	for i, e := range store.entries {
		storedNorm[i][0] = normalize(e.URL)
		if e.ArchivedAt != "" {
			storedNorm[i][1] = normalize(e.ArchivedAt)
		}
	}
	return storedNorm
}

// dedupeOne matches one input against the store: normalized URL equality
// against both stored URL fields, then (when name keywords were given) fuzzy
// name candidates ranked by token containment.
func dedupeOne(store *contentStore, storedNorm [][2]string, in inputLine, minScore float64, maxCandidates int) dedupeResult {
	res := dedupeResult{Input: in.Target, Name: in.Rest, Status: "none"}
	norm := normalize(in.Target)
	if norm != "" {
		for i, e := range store.entries {
			if storedNorm[i][0] != "" && storedNorm[i][0] == norm {
				res.Matches = append(res.Matches, dedupeMatch{"url", e.File, e.Index, e})
			} else if storedNorm[i][1] != "" && storedNorm[i][1] == norm {
				res.Matches = append(res.Matches, dedupeMatch{"archivedAt", e.File, e.Index, e})
			}
		}
	}
	if len(res.Matches) > 0 {
		if res.Matches[0].Field == "url" {
			res.Status = "url-match"
		} else {
			res.Status = "archivedAt-match"
		}
	}
	if in.Rest != "" {
		want := nameTokens(in.Rest)
		var cands []nameCandidate
		for _, e := range store.entries {
			score := tokenContainment(want, nameTokens(e.Name))
			if score >= minScore {
				cands = append(cands, nameCandidate{score, e.File, e.Index, e})
			}
		}
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].Score != cands[j].Score {
				return cands[i].Score > cands[j].Score
			}
			return cands[i].Entry.Name < cands[j].Entry.Name
		})
		if len(cands) > maxCandidates {
			cands = cands[:maxCandidates]
		}
		res.NameCandidates = cands
		if res.Status == "none" && len(cands) > 0 {
			res.Status = "name-only"
		}
	}
	return res
}

// printEntryJSON prints the full stored record, indented, so a match report
// carries every value (name, url, archivedAt, datePublished, keywords) rather
// than a bare "duplicate" verdict.
func printEntryJSON(w io.Writer, e entry, indent string) {
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		fmt.Fprintf(w, "%s<marshal error: %v>\n", indent, err)
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fmt.Fprintf(w, "%s%s\n", indent, line)
	}
}
