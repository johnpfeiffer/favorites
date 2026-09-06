// migrate-to-schema-org converts the favorites collection without editing sources.
// Run: go run tools/migrate-to-schema-org/main.go dry-run
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

var mediaTypes = map[string]string{
	"Podcast": "PodcastEpisode", "Blog": "BlogPosting", "Article": "Article",
	"Video": "VideoObject", "Book": "Book", "Paper": "ScholarlyArticle",
	"TechArticle": "TechArticle",
}

type item struct {
	Type       any             `json:"@type"`
	Name       json.RawMessage `json:"name"`
	URL        json.RawMessage `json:"url"`
	ArchivedAt json.RawMessage `json:"archivedAt,omitempty"`
	Published  json.RawMessage `json:"datePublished,omitempty"`
	Keywords   json.RawMessage `json:"keywords"`
}

type list struct {
	Context string `json:"@context"`
	Type    string `json:"@type"`
	Name    string `json:"name"`
	Items   []item `json:"itemListElement"`
}

// Only the stated Book > BlogPosting > Article preference suppresses types.
// Independent media types remain in their original tag order.
func classify(tags []string) (any, []string) {
	var candidates []string
	seen := map[string]bool{}
	for _, tag := range tags {
		if t, ok := mediaTypes[tag]; ok && !seen[t] {
			candidates = append(candidates, t)
			seen[t] = true
		}
	}
	var selected []string
	for _, t := range candidates {
		if t == "BlogPosting" && seen["Book"] || t == "Article" && (seen["Book"] || seen["BlogPosting"]) {
			continue
		}
		selected = append(selected, t)
	}
	if len(selected) == 0 {
		return "Article", candidates
	}
	if len(selected) == 1 {
		return selected[0], candidates
	}
	return selected, candidates
}

func convert(raw []byte) (list, []string, int, error) {
	var groups map[string][]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return list{}, nil, 0, err
	}
	if len(groups) != 1 {
		return list{}, nil, 0, fmt.Errorf("expected exactly one category, got %d", len(groups))
	}
	out := list{Context: "https://schema.org", Type: "ItemList", Items: []item{}}
	var warnings []string
	fallbacks := 0
	for name, entries := range groups {
		out.Name = name
		if entries == nil {
			return list{}, nil, 0, fmt.Errorf("category %q must be an array", name)
		}
		for index, e := range entries {
			fail := func(message string) (list, []string, int, error) {
				return list{}, nil, 0, fmt.Errorf("%s entry %d: %s", name, index+1, message)
			}
			for key := range e {
				switch key {
				case "title", "url", "alternate-url", "published", "tags":
				default:
					return fail(fmt.Sprintf("unmapped field %q; refusing to discard it", key))
				}
			}
			for _, key := range []string{"title", "url"} {
				var value string
				if err := json.Unmarshal(e[key], &value); err != nil || value == "" {
					return fail(key + " must be a nonempty string")
				}
			}
			for _, key := range []string{"alternate-url", "published"} {
				if value, exists := e[key]; exists {
					var s *string
					if err := json.Unmarshal(value, &s); err != nil {
						return fail(key + " must be a string or null")
					}
				}
			}
			var tags []string
			if err := json.Unmarshal(e["tags"], &tags); err != nil || tags == nil {
				return fail("tags must be an array of strings")
			}
			t, candidates := classify(tags)
			if len(candidates) == 0 {
				fallbacks++
			}
			if len(candidates) > 1 {
				encoded, _ := json.Marshal(t)
				warnings = append(warnings, fmt.Sprintf("entry %d %s: ambiguous media types %v -> %s", index+1, e["title"], candidates, encoded))
			}
			out.Items = append(out.Items, item{t, e["title"], e["url"], e["alternate-url"], e["published"], e["tags"]})
		}
	}
	return out, warnings, fallbacks, nil
}

func encode(value any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetIndent("", "  ")
	e.SetEscapeHTML(false)
	err := e.Encode(value)
	return b.Bytes(), err
}

// Stage in the destination directory so replacement is atomic per file.
func writeFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".schema-org-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(0644); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: migrate-to-schema-org <convert|dry-run|verify> [--input content] [--output DIR] [--dry-run]")
	}
	command := args[0]
	if command != "convert" && command != "dry-run" && command != "verify" {
		return fmt.Errorf("unknown command %q", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "content", "directory containing source .json files")
	output := flags.String("output", "", "destination directory (defaults to input)")
	dryRun := flags.Bool("dry-run", false, "report conversion without writing")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if command == "verify" && *dryRun {
		return fmt.Errorf("verify cannot be combined with --dry-run")
	}
	if *output == "" {
		*output = *input
	}
	files, err := filepath.Glob(filepath.Join(*input, "*.json"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .json files found in %s", *input)
	}
	type result struct {
		path string
		data []byte
	}
	var results []result
	total := 0
	// Validate every source before writing any outputs.
	for _, source := range files {
		raw, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		converted, warnings, fallbacks, err := convert(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
		data, err := encode(converted)
		if err != nil {
			return err
		}
		path := filepath.Join(*output, strings.TrimSuffix(filepath.Base(source), ".json")+".jsonld")
		results = append(results, result{path, data})
		total += len(converted.Items)
		fmt.Fprintf(stdout, "%s -> %s: %d entries, %d Article fallbacks, %d ambiguous entries\n", source, path, len(converted.Items), fallbacks, len(warnings))
		for _, warning := range warnings {
			fmt.Fprintf(stdout, "  %s\n", warning)
		}
	}
	if command == "dry-run" || *dryRun {
		fmt.Fprintf(stdout, "Dry run: %d files, %d entries; no files written.\n", len(results), total)
		return nil
	}
	if command == "verify" {
		for _, r := range results {
			raw, err := os.ReadFile(r.path)
			if err != nil {
				return err
			}
			var actual, expected any
			if err := json.Unmarshal(raw, &actual); err != nil {
				return fmt.Errorf("%s: %w", r.path, err)
			}
			if err := json.Unmarshal(r.data, &expected); err != nil {
				return err
			}
			if !reflect.DeepEqual(actual, expected) {
				return fmt.Errorf("%s: does not match source conversion (including values and array order)", r.path)
			}
		}
		fmt.Fprintf(stdout, "Verified %d files, %d entries.\n", len(results), total)
		return nil
	}
	if err := os.MkdirAll(*output, 0755); err != nil {
		return err
	}
	for _, r := range results {
		if err := writeFile(r.path, r.data); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "Converted %d files, %d entries. Source JSON files preserved.\n", len(results), total)
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
