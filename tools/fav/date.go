package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// dateCandidate is one piece of published-date evidence. The tool never picks
// a winner; it prints every candidate ranked by the skill's evidence ladder
// (cheapest/most-authoritative first) and flags conflicts for the caller.
type dateCandidate struct {
	Source    string `json:"source"`
	Date      string `json:"date"`      // YYYY-MM-DD (month precision uses day 01)
	Precision string `json:"precision"` // day | month | lower-bound
	Detail    string `json:"detail,omitempty"`
	Rank      int    `json:"-"`
}

type dateResult struct {
	Input      string          `json:"input"`
	Candidates []dateCandidate `json:"candidates,omitempty"`
	Notes      []string        `json:"notes,omitempty"`
	Error      string          `json:"error,omitempty"`
}

var (
	// /2001/03/23/ style path dates.
	urlPathDayRe = regexp.MustCompile(`/((?:19|20)\d{2})/([01]\d)/([0-3]\d)(?:/|[-_.?#]|$)`)
	// manager-tools-2011-03-07.mp3 style filename dates.
	urlFileDayRe = regexp.MustCompile(`(?:^|[-_/.])((?:19|20)\d{2})-([01]\d)-([0-3]\d)(?:[-_.]|$)`)
	// /2016/08/ month-only paths.
	urlPathMonthRe = regexp.MustCompile(`/((?:19|20)\d{2})/([01]\d)(?:/|$)`)
	// https://pdfs.example/x.pdf -> <xap:CreateDate>2004-05-03T...</xap:CreateDate>
	xmpCreateDateRe = regexp.MustCompile(`(?is)<xap?:CreateDate>\s*([^<]+)`)
	// /CreationDate (D:20040503094955-07'00')
	pdfCreationDateRe = regexp.MustCompile(`/CreationDate\s*\(\s*D:(\d{4})(\d{2})(\d{2})`)
	// <script type="application/ld+json">...</script>
	jsonLDRe = regexp.MustCompile(`(?is)<script[^>]*type\s*=\s*["']application/ld\+json["'][^>]*>(.*?)</script>`)
	// <time datetime="...">
	timeTagRe = regexp.MustCompile(`(?is)<time[^>]*\bdatetime\s*=\s*["']([^"']+)`)
	// mp3 hrefs carrying a date: files.example/manager-tools-2011-03-07.mp3
	mp3LinkRe = regexp.MustCompile(`(?:href|src)\s*=\s*["'][^"']*?((?:19|20)\d{2})-([01]\d)-([0-3]\d)\.mp3`)
	// ISO date prefix inside a metadata value.
	isoPrefixRe = regexp.MustCompile(`^((?:19|20)\d{2})-([01]\d)-([0-3]\d)`)
)

func cmdDate(args []string) error {
	fs := flag.NewFlagSet("date", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root (for the podcast-index rung)")
	delay := fs.Duration("delay", 2*time.Second, "minimum delay between requests (be polite)")
	offline := fs.Bool("offline", false, "local rungs only (URL path, podcast index)")
	force := fs.Bool("force", false, "fetch the page even when an early rung found a day-precision date")
	asJSON := fs.Bool("json", false, "emit a JSON array instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	inputs, err := readInputs(fs.Args())
	if err != nil {
		return err
	}
	// Metadata sniffing should fail fast: one retry, modest per-request
	// timeout. Wayback capture serving in particular can stall for tens of
	// seconds, and a date query is better served by a NOTE than a hang.
	client := newPoliteClient(
		"Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0 fav/"+version+" (+https://github.com/johnpfeiffer/favorites)",
		*delay, 10*time.Second, 1, 20*time.Second)

	var indexes *podcastIndexes
	if !*offline {
		indexes = loadPodcastIndexesQuiet(*repo)
	}

	results := make([]dateResult, 0, len(inputs))
	for _, in := range inputs {
		results = append(results, resolveDate(client, indexes, in.Target, *offline, *force))
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
		printDateResult(os.Stdout, res)
	}
	return nil
}

func printDateResult(w io.Writer, res dateResult) {
	fmt.Fprintf(w, "INPUT  %s\n", res.Input)
	if res.Error != "" {
		fmt.Fprintf(w, "ERROR  %s\n", res.Error)
	}
	if len(res.Candidates) == 0 {
		fmt.Fprintln(w, "NO CANDIDATES")
	}
	for _, c := range res.Candidates {
		detail := ""
		if c.Detail != "" {
			detail = " (" + c.Detail + ")"
		}
		fmt.Fprintf(w, "  %-9s %s  %-22s%s\n", c.Precision, c.Date, c.Source, detail)
	}
	for _, n := range res.Notes {
		fmt.Fprintf(w, "NOTE   %s\n", n)
	}
}

// resolveDate runs the evidence ladder for one URL. Later (network) rungs are
// skipped once a day-precision candidate exists, unless --force: the ladder is
// cheapest-first, and conflicts worth seeing almost always come from rungs the
// caller can re-run explicitly.
func resolveDate(client *politeClient, indexes *podcastIndexes, rawurl string, offline, force bool) dateResult {
	res := dateResult{Input: rawurl}
	add := func(c dateCandidate) {
		for _, existing := range res.Candidates {
			if existing.Source == c.Source && existing.Date == c.Date {
				return
			}
		}
		res.Candidates = append(res.Candidates, c)
	}
	hasDay := func() bool {
		for _, c := range res.Candidates {
			if c.Precision == "day" && c.Rank <= 2 {
				return true
			}
		}
		return false
	}

	// Rung 1: URL path and filename dates.
	for _, c := range datesFromURL(rawurl) {
		add(c)
	}

	// Rung 2: committed podcast indexes (match by normalized episode URL).
	if indexes != nil {
		for _, c := range indexes.lookupByURL(rawurl) {
			add(c)
		}
	}

	if offline {
		sortCandidates(res.Candidates)
		return res
	}

	// HN threads: the item itself is the content; its timestamp is exact.
	if id := hnItemID(rawurl); id != "" {
		if c, err := hnItemDate(client, id); err == nil && c != nil {
			add(*c)
		} else if err != nil {
			res.Notes = append(res.Notes, "hn-item lookup failed: "+err.Error())
		}
	}

	// Bare Apple episode URLs: identify the episode via the iTunes lookup API.
	if trackID := appleTrackID(rawurl); trackID != "" {
		if c, err := itunesEpisodeDate(client, trackID); err == nil && c != nil {
			add(*c)
		} else if err != nil {
			res.Notes = append(res.Notes, "itunes lookup failed: "+err.Error())
		}
	}

	// Rung 3: page metadata (and PDF XMP). Skipped when an early rung already
	// pinned the day, unless --force.
	var fetch *fetchResult
	var fetchErr error
	if !hasDay() || force {
		fetch, fetchErr = client.get(rawurl, 4<<20)
		if fetchErr == nil && fetch.Status == http.StatusOK {
			before := len(res.Candidates)
			for _, c := range datesFromBody(rawurl, fetch) {
				add(c)
			}
			if len(res.Candidates) == before {
				res.Notes = append(res.Notes, "page fetched (HTTP 200) but contains no recognized date metadata")
			}
		} else if fetchErr != nil {
			res.Notes = append(res.Notes, "page fetch failed: "+fetchErr.Error())
		} else {
			res.Notes = append(res.Notes, fmt.Sprintf("page fetch: HTTP %d", fetch.Status))
		}
	} else {
		res.Notes = append(res.Notes, "page fetch skipped: day-precision candidate already found (--force to override)")
	}

	// Rung 4: Wayback snapshot content, when the live page failed or was
	// bot-blocked and we still lack a day-precision date.
	pageBlocked := fetchErr != nil || (fetch != nil && fetch.Status != http.StatusOK)
	if pageBlocked && !hasCandidateWithPrecision(res.Candidates, "day") {
		snap, err := findSnapshot(client, rawurl, "latest")
		switch {
		case err != nil:
			res.Notes = append(res.Notes, "wayback snapshot lookup: "+err.Error())
		case snap == nil:
			res.Notes = append(res.Notes, "wayback: no 200-status capture found")
		default:
			sfetch, serr := client.get(snap.URL, 4<<20)
			if serr != nil || sfetch.Status != http.StatusOK {
				res.Notes = append(res.Notes, "wayback snapshot fetch failed (ts "+snap.Timestamp+")")
			} else {
				before := len(res.Candidates)
				for _, c := range datesFromBody(rawurl, sfetch) {
					c.Source = "wayback:" + c.Source
					c.Detail = strings.TrimSpace(c.Detail + " snapshot " + snap.Timestamp)
					add(c)
				}
				if len(res.Candidates) == before {
					res.Notes = append(res.Notes, "wayback snapshot "+snap.Timestamp+" yielded no date metadata")
				}
			}
		}
	}

	// Rung 5: Crossref for ACM Queue detail pages and DOI links.
	if !hasCandidateWithPrecision(res.Candidates, "day") {
		if c, err := crossrefDate(client, rawurl); err == nil && c != nil {
			add(*c)
		} else if err != nil {
			res.Notes = append(res.Notes, "crossref: "+err.Error())
		}
	}

	// Rung 7: HN Algolia first submission (month precision) when no day known.
	if !hasCandidateWithPrecision(res.Candidates, "day") {
		if c, err := hnFirstSubmission(client, rawurl); err == nil && c != nil {
			add(*c)
		}
	}

	// Rung 8: Wayback earliest capture as a lower bound when nothing else.
	if len(res.Candidates) == 0 {
		if snap, err := findSnapshot(client, rawurl, "earliest"); err == nil && snap != nil {
			add(dateCandidate{Source: "wayback-earliest", Date: waybackTSDate(snap.Timestamp), Precision: "lower-bound",
				Detail: "earliest 200 capture; publication is no later", Rank: 8})
		} else if err != nil {
			res.Notes = append(res.Notes, "wayback earliest: "+err.Error())
		}
	}

	sortCandidates(res.Candidates)
	// Surface conflicting day-precision dates (the feed-vs-MP3-filename trap).
	seen := map[string]string{}
	var conflict []string
	for _, c := range res.Candidates {
		if c.Precision != "day" {
			continue
		}
		if prev, ok := seen[c.Date]; !ok {
			seen[c.Date] = c.Source
		} else {
			conflict = append(conflict, fmt.Sprintf("%s per %s vs %s per %s", c.Date, c.Source, seen[c.Date], prev))
		}
	}
	if len(conflict) > 0 {
		res.Notes = append(res.Notes, "conflicting day-precision dates: "+strings.Join(conflict, "; "))
	}
	return res
}

func sortCandidates(cs []dateCandidate) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Rank != cs[j].Rank {
			return cs[i].Rank < cs[j].Rank
		}
		return cs[i].Date < cs[j].Date
	})
}

func hasCandidateWithPrecision(cs []dateCandidate, precision string) bool {
	for _, c := range cs {
		if c.Precision == precision {
			return true
		}
	}
	return false
}

func validYMD(y, m, d string) (string, bool) {
	yi, _ := strconv.Atoi(y)
	mi, _ := strconv.Atoi(m)
	di, _ := strconv.Atoi(d)
	if yi < 1970 || yi > 2100 || mi < 1 || mi > 12 || di < 1 || di > 31 {
		return "", false
	}
	return fmt.Sprintf("%04d-%02d-%02d", yi, mi, di), true
}

// datesFromURL extracts rung-1 candidates from the URL path and filename.
func datesFromURL(rawurl string) []dateCandidate {
	var out []dateCandidate
	if m := urlPathDayRe.FindStringSubmatch(rawurl); m != nil {
		if d, ok := validYMD(m[1], m[2], m[3]); ok {
			out = append(out, dateCandidate{Source: "url-path", Date: d, Precision: "day", Detail: "day in URL path", Rank: 1})
		}
	}
	if m := urlFileDayRe.FindStringSubmatch(rawurl); m != nil {
		if d, ok := validYMD(m[1], m[2], m[3]); ok {
			out = append(out, dateCandidate{Source: "url-filename", Date: d, Precision: "day", Detail: "date in filename", Rank: 1})
		}
	}
	if len(out) == 0 {
		if m := urlPathMonthRe.FindStringSubmatch(rawurl); m != nil {
			if d, ok := validYMD(m[1], m[2], "01"); ok {
				out = append(out, dateCandidate{Source: "url-path", Date: d, Precision: "month", Detail: "month-only path; fetch page for the day", Rank: 1})
			}
		}
	}
	return out
}

// datesFromBody extracts metadata candidates from a fetched page or PDF.
func datesFromBody(rawurl string, fetch *fetchResult) []dateCandidate {
	body := fetch.Body
	contentType := ""
	if strings.HasSuffix(strings.ToLower(strings.Split(rawurl, "?")[0]), ".pdf") {
		contentType = "pdf"
	}
	if contentType == "pdf" || looksLikePDF(body) {
		return datesFromPDF(body)
	}
	return datesFromHTML(string(body))
}

func looksLikePDF(body []byte) bool {
	return len(body) > 5 && string(body[:5]) == "%PDF-"
}

func datesFromPDF(body []byte) []dateCandidate {
	var out []dateCandidate
	text := string(body)
	if m := xmpCreateDateRe.FindStringSubmatch(text); m != nil {
		if d, ok := isoDatePrefix(m[1]); ok {
			out = append(out, dateCandidate{Source: "pdf-xmp", Date: d, Precision: "day", Detail: "XMP CreateDate", Rank: 3})
		}
	}
	if m := pdfCreationDateRe.FindStringSubmatch(text); m != nil {
		if d, ok := validYMD(m[1], m[2], m[3]); ok {
			out = append(out, dateCandidate{Source: "pdf-info", Date: d, Precision: "day", Detail: "PDF CreationDate", Rank: 3})
		}
	}
	return out
}

// isoDatePrefix pulls a YYYY-MM-DD date from the front of an ISO-ish value.
func isoDatePrefix(v string) (string, bool) {
	v = strings.TrimSpace(v)
	m := isoPrefixRe.FindStringSubmatch(v)
	if m == nil {
		return "", false
	}
	return validYMD(m[1], m[2], m[3])
}

func datesFromHTML(body string) []dateCandidate {
	var out []dateCandidate
	head := headSection(body)

	// JSON-LD datePublished / dateCreated / uploadDate (in that trust order).
	jsonldKeys := []struct {
		key    string
		source string
	}{
		{"datePublished", "json-ld:datePublished"},
		{"dateCreated", "json-ld:dateCreated"},
		{"uploadDate", "json-ld:uploadDate"},
	}
	found := map[string]string{}
	for _, block := range jsonLDRe.FindAllStringSubmatch(head, -1) {
		var v any
		if err := json.Unmarshal([]byte(strings.TrimSpace(block[1])), &v); err != nil {
			continue
		}
		walkJSONDates(v, found)
	}
	for _, k := range jsonldKeys {
		if val, ok := found[k.key]; ok {
			if d, ok2 := isoDatePrefix(val); ok2 {
				out = append(out, dateCandidate{Source: k.source, Date: d, Precision: "day", Rank: 3})
			}
		}
	}

	// meta tags: article:published_time, then dc/date fallbacks.
	for _, tag := range metaRe.FindAllString(head, -1) {
		attrs := tagAttrs(tag)
		content := attrs["content"]
		if content == "" {
			continue
		}
		var source string
		switch {
		case strings.EqualFold(attrs["property"], "article:published_time"):
			source = "meta:article:published_time"
		case strings.EqualFold(attrs["name"], "date") || strings.EqualFold(attrs["name"], "dc.date"):
			source = "meta:" + strings.ToLower(attrs["name"])
		}
		if source != "" {
			if d, ok := isoDatePrefix(content); ok {
				out = append(out, dateCandidate{Source: source, Date: d, Precision: "day", Rank: 3})
			}
		}
	}

	// First <time datetime="...">.
	if m := timeTagRe.FindStringSubmatch(head + body[len(head):]); m != nil {
		if d, ok := isoDatePrefix(m[1]); ok {
			out = append(out, dateCandidate{Source: "time-datetime", Date: d, Precision: "day", Rank: 3})
		}
	}

	// Dated MP3 links (Manager Tools rule: the cast's own filename date wins).
	if m := mp3LinkRe.FindStringSubmatch(body); m != nil {
		if d, ok := validYMD(m[1], m[2], m[3]); ok {
			out = append(out, dateCandidate{Source: "mp3-link", Date: d, Precision: "day", Detail: "date in linked audio filename", Rank: 3})
		}
	}
	return out
}

// walkJSONDates collects date-ish JSON-LD fields from any nesting shape
// (object, array, or @graph).
func walkJSONDates(v any, found map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for _, key := range []string{"datePublished", "dateCreated", "uploadDate"} {
			if _, ok := found[key]; !ok {
				if s, ok2 := t[key].(string); ok2 {
					found[key] = s
				}
			}
		}
		for _, val := range t {
			walkJSONDates(val, found)
		}
	case []any:
		for _, item := range t {
			walkJSONDates(item, found)
		}
	}
}

// hnItemID extracts the id from a news.ycombinator.com/item?id=N URL.
func hnItemID(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil || !strings.Contains(strings.ToLower(u.Host), "news.ycombinator.com") {
		return ""
	}
	return u.Query().Get("id")
}

func hnItemDate(client *politeClient, id string) (*dateCandidate, error) {
	res, err := client.get("https://hacker-news.firebaseio.com/v0/item/"+id+".json", 1<<20)
	if err != nil {
		return nil, err
	}
	var item struct {
		Time  int64   `json:"time"`
		Title *string `json:"title"`
	}
	if err := json.Unmarshal(res.Body, &item); err != nil {
		return nil, err
	}
	if item.Time == 0 {
		return nil, nil
	}
	d := time.Unix(item.Time, 0).UTC().Format("2006-01-02")
	c := &dateCandidate{Source: "hn-item", Date: d, Precision: "day", Detail: "HN submission timestamp", Rank: 2}
	if item.Title != nil {
		c.Detail += ": " + *item.Title
	}
	return c, nil
}

// appleTrackID extracts the episode id (?i=...) from a podcasts.apple.com URL.
func appleTrackID(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil || !strings.Contains(strings.ToLower(u.Host), "podcasts.apple.com") {
		return ""
	}
	return u.Query().Get("i")
}

func itunesEpisodeDate(client *politeClient, trackID string) (*dateCandidate, error) {
	res, err := client.get("https://itunes.apple.com/lookup?id="+trackID, 1<<20)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Results []struct {
			TrackName   string `json:"trackName"`
			ReleaseDate string `json:"releaseDate"`
		} `json:"results"`
	}
	if err := json.Unmarshal(res.Body, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Results) == 0 {
		return nil, fmt.Errorf("track %s not in catalog (delisted?)", trackID)
	}
	d, ok := isoDatePrefix(parsed.Results[0].ReleaseDate)
	if !ok {
		return nil, nil
	}
	return &dateCandidate{Source: "itunes-lookup", Date: d, Precision: "day",
		Detail: parsed.Results[0].TrackName, Rank: 2}, nil
}

// crossrefDate handles queue.acm.org/detail.cfm?id=N (the Crossref record
// whose DOI ends in .N is the same article) and doi.org links directly.
func crossrefDate(client *politeClient, rawurl string) (*dateCandidate, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, nil
	}
	host := strings.ToLower(u.Host)
	var doi string
	if host == "doi.org" {
		doi = strings.TrimPrefix(u.Path, "/")
	} else if strings.Contains(host, "queue.acm.org") {
		id := u.Query().Get("id")
		if id == "" {
			return nil, nil
		}
		found, err := crossrefByQueueID(client, id)
		if err != nil || found == "" {
			return nil, err
		}
		doi = found
	} else {
		return nil, nil
	}
	res, err := client.get("https://api.crossref.org/works/"+url.PathEscape(doi), 1<<20)
	if err != nil {
		return nil, err
	}
	if res.Status != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for DOI %s", res.Status, doi)
	}
	var work struct {
		Message struct {
			Issued struct {
				DateParts [][]int `json:"date-parts"`
			} `json:"issued"`
		} `json:"message"`
	}
	if err := json.Unmarshal(res.Body, &work); err != nil {
		return nil, err
	}
	parts := work.Message.Issued.DateParts
	if len(parts) == 0 || len(parts[0]) == 0 {
		return nil, nil
	}
	p := parts[0]
	if len(p) >= 3 {
		return &dateCandidate{Source: "crossref", Date: fmt.Sprintf("%04d-%02d-%02d", p[0], p[1], p[2]),
			Precision: "day", Detail: "DOI " + doi, Rank: 5}, nil
	}
	if len(p) == 2 {
		return &dateCandidate{Source: "crossref", Date: fmt.Sprintf("%04d-%02d-01", p[0], p[1]),
			Precision: "month", Detail: "DOI " + doi + " (issue month)", Rank: 5}, nil
	}
	return nil, nil
}

func crossrefByQueueID(client *politeClient, id string) (string, error) {
	u := "https://api.crossref.org/works?query.bibliographic=" + url.QueryEscape(id) + "&rows=10"
	res, err := client.get(u, 1<<20)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Message struct {
			Items []struct {
				DOI string `json:"DOI"`
			} `json:"items"`
		} `json:"message"`
	}
	if err := json.Unmarshal(res.Body, &parsed); err != nil {
		return "", err
	}
	suffix := "." + id
	for _, item := range parsed.Message.Items {
		if strings.HasSuffix(item.DOI, suffix) {
			return item.DOI, nil
		}
	}
	return "", nil
}

// hnFirstSubmission returns the earliest HN submission month for the URL.
func hnFirstSubmission(client *politeClient, rawurl string) (*dateCandidate, error) {
	u := "https://hn.algolia.com/api/v1/search?restrictSearchableAttributes=url&query=" + url.QueryEscape(rawurl)
	res, err := client.get(u, 1<<20)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Hits []struct {
			CreatedAt time.Time `json:"created_at"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(res.Body, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Hits) == 0 {
		return nil, nil
	}
	earliest := parsed.Hits[0].CreatedAt
	for _, h := range parsed.Hits[1:] {
		if h.CreatedAt.Before(earliest) {
			earliest = h.CreatedAt
		}
	}
	return &dateCandidate{Source: "hn-first-submission", Date: earliest.UTC().Format("2006-01") + "-01",
		Precision: "month", Detail: "first HN submission; publication likely earlier or same month", Rank: 7}, nil
}

// waybackTSDate converts a 14-digit Wayback timestamp to YYYY-MM-DD.
func waybackTSDate(ts string) string {
	if len(ts) >= 8 {
		return ts[:4] + "-" + ts[4:6] + "-" + ts[6:8]
	}
	return ts
}

// ---------------------------------------------------------------------------
// podcast index lookup (rung 2)
// ---------------------------------------------------------------------------

type podcastIndexes struct {
	byNormURL map[string][]dateCandidate
}

// loadPodcastIndexesQuiet loads every committed index; failures are skipped
// (this rung is best-effort — lookup never fails a date query).
func loadPodcastIndexesQuiet(repo string) *podcastIndexes {
	reg, err := loadRegistry(repo)
	if err != nil {
		return &podcastIndexes{byNormURL: map[string][]dateCandidate{}}
	}
	pi := &podcastIndexes{byNormURL: map[string][]dateCandidate{}}
	for _, show := range reg.Shows {
		idx, err := loadIndex(repo, show.Slug)
		if err != nil {
			continue
		}
		for _, ep := range idx.Episodes {
			if ep.URL == nil || ep.Published == nil {
				continue
			}
			c := dateCandidate{
				Source:    "podcast-index:" + show.Slug,
				Date:      *ep.Published,
				Precision: "day",
				Rank:      2,
			}
			if ep.Title != nil {
				c.Detail = *ep.Title
			}
			norm := normalize(*ep.URL)
			pi.byNormURL[norm] = append(pi.byNormURL[norm], c)
		}
	}
	return pi
}

func (pi *podcastIndexes) lookupByURL(rawurl string) []dateCandidate {
	if pi == nil {
		return nil
	}
	return pi.byNormURL[normalize(rawurl)]
}
