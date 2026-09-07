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
	datePublishedRe = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
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

	files, err := filepath.Glob(filepath.Join(dir, "*.jsonld"))
	if err != nil {
		return nil, stats, err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, stats, fmt.Errorf("no .jsonld files found in %s", dir)
	}
	stats.files = len(files)

	// Cross-entry state for duplicate and tag-vocabulary checks.
	type loc struct {
		file  string
		index int
	}
	urlSeen := map[string]loc{}                  // normalized url -> first location
	altSeen := map[string]loc{}                  // normalized archivedAt -> first location
	tagSpellings := map[string]map[string]bool{} // folded tag -> actual spellings

	for _, f := range files {
		base := filepath.Base(f)
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, stats, err
		}
		var list itemList
		if err := json.Unmarshal(raw, &list); err != nil {
			findings = append(findings, lintFinding{"error", base, -1, "json", err.Error()})
			continue
		}
		if list.Type != "ItemList" {
			findings = append(findings, lintFinding{"error", base, -1, "list-type",
				fmt.Sprintf("expected @type ItemList, found %q", list.Type)})
		}
		category := list.Name
		if category == "" {
			findings = append(findings, lintFinding{"error", base, -1, "list-name",
				"ItemList name (the category) is empty"})
		} else if !knownCategories[category] {
			findings = append(findings, lintFinding{"warning", base, -1, "unknown-category",
				"ItemList name " + category + " is not one of AI/Business/Engineering/History/People"})
		}

		for idx := range list.Items {
			e := list.Items[idx].toEntry(base, idx)
			stats.entries++
			at := fmt.Sprintf("%q", truncate(e.Name, 60))

			// Schema.
			if strings.TrimSpace(e.Name) == "" {
				findings = append(findings, lintFinding{"error", base, idx, "name-empty", "name is empty"})
			}
			u, err := url.Parse(e.URL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				findings = append(findings, lintFinding{"error", base, idx, "url-invalid", "url is not absolute http(s): " + at})
			}
			if e.DatePublished != nil {
				m := datePublishedRe.FindStringSubmatch(*e.DatePublished)
				if m == nil {
					findings = append(findings, lintFinding{"error", base, idx, "date-published-format",
						"datePublished is not YYYY-MM-DD: " + *e.DatePublished})
				} else if _, err := time.Parse("2006-01-02", *e.DatePublished); err != nil {
					findings = append(findings, lintFinding{"error", base, idx, "date-published-format",
						"datePublished is not a valid date: " + *e.DatePublished})
				}
			}
			if len(e.Keywords) == 0 {
				findings = append(findings, lintFinding{"error", base, idx, "keywords-empty", at})
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
			if e.ArchivedAt != "" {
				anorm := normalize(e.ArchivedAt)
				// A Wayback snapshot of the url itself is the standard backup
				// pattern, so only flag when the archivedAt is the same *address*
				// (copy-paste errors), not merely a snapshot of the same page.
				if normalizeKeepWayback(e.ArchivedAt) == normalizeKeepWayback(e.URL) {
					findings = append(findings, lintFinding{"warning", base, idx, "archivedat-equals-url",
						"archivedAt is the same address as url (a Wayback backup should differ): " + at})
				}
				if prev, ok := altSeen[anorm]; ok {
					findings = append(findings, lintFinding{"error", base, idx, "duplicate-archived-at",
						fmt.Sprintf("archivedAt same as %s[%d]: %s", prev.file, prev.index, e.ArchivedAt)})
				} else {
					altSeen[anorm] = loc{base, idx}
				}
				if prev, ok := urlSeen[anorm]; ok && !(prev.file == base && prev.index == idx) {
					findings = append(findings, lintFinding{"warning", base, idx, "archivedat-matches-other-url",
						fmt.Sprintf("archivedAt equals the canonical url of %s[%d]", prev.file, prev.index)})
				}
			}

			// Tag conventions.
			if len(e.Keywords) > 0 && e.Keywords[0] != category {
				findings = append(findings, lintFinding{"warning", base, idx, "category-first",
					fmt.Sprintf("first tag is %q, want file category %q: %s", e.Keywords[0], category, at)})
			}
			mediaCount := 0
			lastIsMedia := false
			for i, tag := range e.Keywords {
				if mediaTypeTags[tag] {
					mediaCount++
					lastIsMedia = i == len(e.Keywords)-1
				}
				folded := foldTag(tag)
				if tagSpellings[folded] == nil {
					tagSpellings[folded] = map[string]bool{}
				}
				tagSpellings[folded][tag] = true
			}
			isHN := strings.HasPrefix(e.Name, hnDiscussionPre)
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

			// Name conventions.
			if yearParenRe.MatchString(e.Name) {
				findings = append(findings, lintFinding{"warning", base, idx, "name-year-parenthetical",
					"year parenthetical in name (the UI appends the year): " + at})
			}
			if strings.Contains(strings.ToLower(u.Host), "news.ycombinator.com") && !isHN {
				findings = append(findings, lintFinding{"warning", base, idx, "hn-name-convention",
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
