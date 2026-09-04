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

// dedupeMatch is a stored entry whose url or alternate-url equals the input
// after normalization (scheme/www/trailing-slash/tracking-params/Wayback
// wrapping all ignored).
type dedupeMatch struct {
	Field string `json:"field"` // "url" or "alternate-url"
	File  string `json:"file"`
	Index int    `json:"index"`
	Entry entry  `json:"entry"`
}

// titleCandidate is a stored entry whose title lexically overlaps the
// optional title keywords given with the input.
type titleCandidate struct {
	Score float64 `json:"score"`
	File  string  `json:"file"`
	Index int     `json:"index"`
	Entry entry   `json:"entry"`
}

type dedupeResult struct {
	Input           string           `json:"input"`
	Title           string           `json:"title,omitempty"`
	Status          string           `json:"status"` // url-match | alternate-match | title-only | none
	Matches         []dedupeMatch    `json:"matches,omitempty"`
	TitleCandidates []titleCandidate `json:"titleCandidates,omitempty"`
}

func cmdDedupe(args []string) error {
	fs := flag.NewFlagSet("dedupe", flag.ContinueOnError)
	contentDir := fs.String("content", "content", "path to the content directory")
	asJSON := fs.Bool("json", false, "emit a JSON array instead of text")
	minScore := fs.Float64("min-score", 0.6, "minimum title token containment for title candidates (0-1)")
	maxCandidates := fs.Int("max-candidates", 5, "maximum title candidates per input")
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
	if res.Title != "" {
		fmt.Fprintf(w, "TITLE  %s\n", res.Title)
	}
	fmt.Fprintf(w, "STATUS %s\n", res.Status)
	for _, m := range res.Matches {
		fmt.Fprintf(w, "MATCH  field=%s %s[%d]\n", m.Field, m.File, m.Index)
		printEntryJSON(w, m.Entry, "  ")
	}
	for _, c := range res.TitleCandidates {
		fmt.Fprintf(w, "TITLE? score=%.2f %s[%d]\n", c.Score, c.File, c.Index)
		printEntryJSON(w, c.Entry, "  ")
	}
}

// normalizeStore pre-computes normalized [url, alternate-url] per entry.
func normalizeStore(store *contentStore) [][2]string {
	storedNorm := make([][2]string, len(store.entries))
	for i, e := range store.entries {
		storedNorm[i][0] = normalize(e.URL)
		if e.AlternateURL != "" {
			storedNorm[i][1] = normalize(e.AlternateURL)
		}
	}
	return storedNorm
}

// dedupeOne matches one input against the store: normalized URL equality
// against both stored URL fields, then (when title keywords were given) fuzzy
// title candidates ranked by token containment.
func dedupeOne(store *contentStore, storedNorm [][2]string, in inputLine, minScore float64, maxCandidates int) dedupeResult {
	res := dedupeResult{Input: in.Target, Title: in.Rest, Status: "none"}
	norm := normalize(in.Target)
	if norm != "" {
		for i, e := range store.entries {
			if storedNorm[i][0] != "" && storedNorm[i][0] == norm {
				res.Matches = append(res.Matches, dedupeMatch{"url", e.File, e.Index, e})
			} else if storedNorm[i][1] != "" && storedNorm[i][1] == norm {
				res.Matches = append(res.Matches, dedupeMatch{"alternate-url", e.File, e.Index, e})
			}
		}
	}
	if len(res.Matches) > 0 {
		if res.Matches[0].Field == "url" {
			res.Status = "url-match"
		} else {
			res.Status = "alternate-match"
		}
	}
	if in.Rest != "" {
		want := titleTokens(in.Rest)
		var cands []titleCandidate
		for _, e := range store.entries {
			score := tokenContainment(want, titleTokens(e.Title))
			if score >= minScore {
				cands = append(cands, titleCandidate{score, e.File, e.Index, e})
			}
		}
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].Score != cands[j].Score {
				return cands[i].Score > cands[j].Score
			}
			return cands[i].Entry.Title < cands[j].Entry.Title
		})
		if len(cands) > maxCandidates {
			cands = cands[:maxCandidates]
		}
		res.TitleCandidates = cands
		if res.Status == "none" && len(cands) > 0 {
			res.Status = "title-only"
		}
	}
	return res
}

// printEntryJSON prints the full stored record, indented, so a match report
// carries every value (title, url, alternate-url, published, tags) rather
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
