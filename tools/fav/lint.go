package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// lintFinding is one rule violation. Errors fail the run (exit 1); warnings
// are advisory (convention drift the caller may accept).
type lintFinding struct {
	Level  string `json:"level"` // error | warning
	File   string `json:"file"`
	Index  int    `json:"index"` // -1 = file-level or cross-file
	Rule   string `json:"rule"`
	Detail string `json:"detail"`
}

// mediaTypeTags is the complete set in use per the skill; conventionally the
// last tag. HN Discussion entries conventionally carry none.
var mediaTypeTags = map[string]bool{
	"Podcast": true, "Blog": true, "Article": true,
	"Video": true, "Book": true, "Paper": true,
}

var knownCategories = map[string]bool{
	"AI": true, "Business": true, "Engineering": true, "History": true, "People": true,
}

var (
	publishedRe     = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	yearParenRe     = regexp.MustCompile(`\((?:19|20)\d{2}[^)]*\)`)
	hnDiscussionPre = "HN Discussion:"
)

func cmdLint(args []string) error {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	contentDir := fs.String("content", "content", "path to the content directory")
	asJSON := fs.Bool("json", false, "emit a JSON array instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("lint takes no positional arguments")
	}

	findings, stats, err := lintContent(*contentDir)
	if err != nil {
		return err
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(findings)
	}
	printLintFindings(os.Stdout, findings, stats)
	for _, f := range findings {
		if f.Level == "error" {
			return errLintErrors
		}
	}
	return nil
}

var errLintErrors = fmt.Errorf("lint found errors")

type lintStats struct {
	files   int
	entries int
	urls    int
	dupes   int
}

func printLintFindings(w io.Writer, findings []lintFinding, stats lintStats) {
	fmt.Fprintf(w, "scanned %d files, %d entries (%d urls, %d normalized dupes)\n",
		stats.files, stats.entries, stats.urls, stats.dupes)
	if len(findings) == 0 {
		fmt.Fprintln(w, "no findings")
		return
	}
	byFile := map[string][]lintFinding{}
	var files []string
	for _, f := range findings {
		if _, ok := byFile[f.File]; !ok {
			files = append(files, f.File)
		}
		byFile[f.File] = append(byFile[f.File], f)
	}
	sort.Strings(files)
	for _, file := range files {
		fmt.Fprintf(w, "\n%s\n", file)
		for _, f := range byFile[file] {
			loc := ""
			if f.Index >= 0 {
				loc = fmt.Sprintf("[%d]", f.Index)
			}
			fmt.Fprintf(w, "  %-7s %-4s %-24s %s\n", f.Level, loc, f.Rule, f.Detail)
		}
	}
	errs, warns := 0, 0
	for _, f := range findings {
		if f.Level == "error" {
			errs++
		} else {
			warns++
		}
	}
	fmt.Fprintf(w, "\n%d errors, %d warnings\n", errs, warns)
}

// lintContent runs every rule over the content directory.
func lintContent(dir string) ([]lintFinding, lintStats, error) {
	var findings []lintFinding
	var stats lintStats

	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, stats, err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, stats, fmt.Errorf("no .json files found in %s", dir)
	}
	stats.files = len(files)

	// Cross-entry state for duplicate and tag-vocabulary checks.
	type loc struct {
		file  string
		index int
	}
	urlSeen := map[string]loc{}                  // normalized url -> first location
	altSeen := map[string]loc{}                  // normalized alternate-url -> first location
	tagSpellings := map[string]map[string]bool{} // folded tag -> actual spellings

	for _, f := range files {
		base := filepath.Base(f)
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, stats, err
		}
		var byCategory map[string][]entry
		if err := json.Unmarshal(raw, &byCategory); err != nil {
			findings = append(findings, lintFinding{"error", base, -1, "json", err.Error()})
			continue
		}
		if len(byCategory) != 1 {
			findings = append(findings, lintFinding{"error", base, -1, "top-level-keys",
				fmt.Sprintf("expected exactly one top-level category key, found %d", len(byCategory))})
		}
		var category string
		for k := range byCategory {
			category = k
		}
		if category != "" && !knownCategories[category] {
			findings = append(findings, lintFinding{"warning", base, -1, "unknown-category",
				"top-level key " + category + " is not one of AI/Business/Engineering/History/People"})
		}

		for idx, e := range byCategory[category] {
			stats.entries++
			at := fmt.Sprintf("%q", truncate(e.Title, 60))

			// Schema.
			if strings.TrimSpace(e.Title) == "" {
				findings = append(findings, lintFinding{"error", base, idx, "title-empty", "title is empty"})
			}
			u, err := url.Parse(e.URL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				findings = append(findings, lintFinding{"error", base, idx, "url-invalid", "url is not absolute http(s): " + at})
			}
			if e.Published != nil {
				m := publishedRe.FindStringSubmatch(*e.Published)
				if m == nil {
					findings = append(findings, lintFinding{"error", base, idx, "published-format",
						"published is not YYYY-MM-DD: " + *e.Published})
				} else if _, err := time.Parse("2006-01-02", *e.Published); err != nil {
					findings = append(findings, lintFinding{"error", base, idx, "published-format",
						"published is not a valid date: " + *e.Published})
				}
			}
			if len(e.Tags) == 0 {
				findings = append(findings, lintFinding{"error", base, idx, "tags-empty", at})
			}

			// Duplicate detection on normalized URLs (absorbs count-urls.sh,
			// but stricter: utm/www/scheme variants collide too).
			stats.urls++
			norm := normalize(e.URL)
			if prev, ok := urlSeen[norm]; ok {
				stats.dupes++
				findings = append(findings, lintFinding{"error", base, idx, "duplicate-url",
					fmt.Sprintf("same as %s[%d]: %s", prev.file, prev.index, e.URL)})
			} else {
				urlSeen[norm] = loc{base, idx}
			}
			if e.AlternateURL != "" {
				anorm := normalize(e.AlternateURL)
				// A Wayback snapshot of the url itself is the standard backup
				// pattern, so only flag when the alternate is the same *address*
				// (copy-paste errors), not merely a snapshot of the same page.
				if normalizeKeepWayback(e.AlternateURL) == normalizeKeepWayback(e.URL) {
					findings = append(findings, lintFinding{"warning", base, idx, "alternate-equals-url",
						"alternate-url is the same address as url (a Wayback backup should differ): " + at})
				}
				if prev, ok := altSeen[anorm]; ok {
					findings = append(findings, lintFinding{"error", base, idx, "duplicate-alternate",
						fmt.Sprintf("alternate-url same as %s[%d]: %s", prev.file, prev.index, e.AlternateURL)})
				} else {
					altSeen[anorm] = loc{base, idx}
				}
				if prev, ok := urlSeen[anorm]; ok && !(prev.file == base && prev.index == idx) {
					findings = append(findings, lintFinding{"warning", base, idx, "alternate-matches-other-url",
						fmt.Sprintf("alternate-url equals the canonical url of %s[%d]", prev.file, prev.index)})
				}
			}

			// Tag conventions.
			if len(e.Tags) > 0 && e.Tags[0] != category {
				findings = append(findings, lintFinding{"warning", base, idx, "category-first",
					fmt.Sprintf("first tag is %q, want file category %q: %s", e.Tags[0], category, at)})
			}
			mediaCount := 0
			lastIsMedia := false
			for i, tag := range e.Tags {
				if mediaTypeTags[tag] {
					mediaCount++
					lastIsMedia = i == len(e.Tags)-1
				}
				folded := foldTag(tag)
				if tagSpellings[folded] == nil {
					tagSpellings[folded] = map[string]bool{}
				}
				tagSpellings[folded][tag] = true
			}
			isHN := strings.HasPrefix(e.Title, hnDiscussionPre)
			if mediaCount == 0 && !isHN {
				findings = append(findings, lintFinding{"warning", base, idx, "media-type-missing",
					"no media-type tag (Podcast/Blog/Article/Video/Book/Paper): " + at})
			}
			if mediaCount > 0 && !lastIsMedia {
				findings = append(findings, lintFinding{"warning", base, idx, "media-type-last",
					"media-type tag is not last: " + at})
			}
			if mediaCount > 1 {
				findings = append(findings, lintFinding{"warning", base, idx, "media-type-multiple", at})
			}

			// Title conventions.
			if yearParenRe.MatchString(e.Title) {
				findings = append(findings, lintFinding{"warning", base, idx, "title-year-parenthetical",
					"year parenthetical in title (the UI appends the year): " + at})
			}
			if strings.Contains(strings.ToLower(u.Host), "news.ycombinator.com") && !isHN {
				findings = append(findings, lintFinding{"warning", base, idx, "hn-title-convention",
					"HN thread without the 'HN Discussion:' prefix: " + at})
			}
		}
	}

	// Tag spelling collisions that differ only by case/punctuation
	// (the "Mauboussi vs Mauboussin" class of drift is out of scope; this
	// catches "golang" vs "Golang").
	folded := make([]string, 0, len(tagSpellings))
	for k := range tagSpellings {
		folded = append(folded, k)
	}
	sort.Strings(folded)
	for _, k := range folded {
		spellings := tagSpellings[k]
		if len(spellings) > 1 {
			var list []string
			for s := range spellings {
				list = append(list, s)
			}
			sort.Strings(list)
			findings = append(findings, lintFinding{"warning", "-", -1, "tag-case-collision",
				"tag spellings differ only by case/punctuation: " + strings.Join(list, " | ")})
		}
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Index < findings[j].Index
	})
	return findings, stats, nil
}

// foldTag reduces a tag to a comparison form: lowercase, alphanumerics only.
func foldTag(tag string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(tag) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
