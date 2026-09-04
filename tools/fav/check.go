package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// checkResult reports what a URL actually serves: redirect chain, final
// status, page title, declared canonical, and a classification. The caller
// judges defunct-vs-bot-blocked; fav only gathers the evidence.
type checkResult struct {
	Input            string   `json:"input"`
	Status           string   `json:"status"` // ok | bot-block-suspect | forbidden | not-found | server-error | error
	HTTPStatus       int      `json:"httpStatus,omitempty"`
	FinalURL         string   `json:"finalUrl,omitempty"`
	Redirects        []hop    `json:"redirects,omitempty"`
	Title            string   `json:"title,omitempty"`
	Canonical        string   `json:"canonical,omitempty"`
	CanonicalDiffers bool     `json:"canonicalDiffers,omitempty"`
	Notes            []string `json:"notes,omitempty"`
	Error            string   `json:"error,omitempty"`
}

// botBlockMarkers are body signatures of an anti-bot interstitial rather than
// a genuinely dead page (Cloudflare, Distill, etc.). On HTTP 200 they only
// count when the page is small or the <title> itself is the interstitial:
// real article pages routinely embed reCAPTCHA/challenge script URLs, so a
// bare marker in a large page means nothing.
var botBlockMarkers = []string{
	"attention required",
	"just a moment",
	"cf-chl",
	"checking your browser",
	"checking if the site connection is secure",
	"verify you are a human",
	"are you a robot",
}

// interstitialTitles are <title> texts of challenge pages, matched lowercase
// as substrings (e.g. "Just a moment...", "Attention Required! | Cloudflare").
var interstitialTitles = []string{
	"just a moment",
	"attention required",
	"checking your browser",
	"verify you are a human",
	"are you a robot",
	"security check",
	"access denied",
}

var (
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	linkRe  = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	metaRe  = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	attrRe  = regexp.MustCompile(`(?is)(\w[\w-]*)\s*=\s*("[^"]*"|'[^']*')`)
	headEnd = regexp.MustCompile(`(?is)</head>`)
)

func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	delay := fs.Duration("delay", 2*time.Second, "minimum delay between requests (be polite)")
	backoff := fs.Duration("backoff", 15*time.Second, "backoff after a 429/503 or network error")
	retries := fs.Int("retries", 2, "retries per request after transient failures")
	timeout := fs.Duration("timeout", 30*time.Second, "per-request timeout")
	maxBody := fs.Int64("max-body", 1<<20, "maximum response bytes read (head tags live at the top)")
	asJSON := fs.Bool("json", false, "emit a JSON array instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	inputs, err := readInputs(fs.Args())
	if err != nil {
		return err
	}
	// A browser UA per the skill's fetch playbook: many sites 403 plain curl.
	client := newPoliteClient(
		"Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0 fav/"+version+" (+https://github.com/johnpfeiffer/favorites)",
		*delay, *backoff, *retries, *timeout)

	results := make([]checkResult, 0, len(inputs))
	for _, in := range inputs {
		results = append(results, checkOne(client, in.Target, *maxBody))
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
		printCheckResult(os.Stdout, res)
	}
	return nil
}

func checkOne(client *politeClient, rawurl string, maxBody int64) checkResult {
	res := checkResult{Input: rawurl}
	fetch, err := client.get(rawurl, maxBody)
	if fetch != nil {
		if len(fetch.Chain) > 1 {
			res.Redirects = fetch.Chain[:len(fetch.Chain)-1]
		}
		res.FinalURL = fetch.FinalURL
		res.HTTPStatus = fetch.Status
	}
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		return res
	}

	body := string(fetch.Body)
	res.Title = extractTitle(body)
	res.Status = classify(fetch.Status, body, res.Title)
	if res.Status == "ok" || res.Status == "bot-block-suspect" || fetch.Truncated {
		res.Canonical = extractCanonical(body)
	}
	if res.Canonical != "" && normalize(res.Canonical) != normalize(res.FinalURL) {
		res.CanonicalDiffers = true
		res.Notes = append(res.Notes, "rel=canonical differs from final URL: "+res.Canonical)
	}
	if res.Canonical != "" && normalize(res.Canonical) != normalize(rawurl) {
		res.CanonicalDiffers = true
		res.Notes = append(res.Notes, "rel=canonical differs from submitted URL: "+res.Canonical)
	}
	if len(fetch.Chain) > 1 {
		firstHost := hostOf(fetch.Chain[0].URL)
		finalHost := hostOf(fetch.FinalURL)
		if firstHost != "" && finalHost != "" && firstHost != finalHost {
			res.Notes = append(res.Notes, fmt.Sprintf("redirected across hosts: %s -> %s", firstHost, finalHost))
		}
	}
	if fetch.Truncated {
		res.Notes = append(res.Notes, "body truncated at --max-body; head metadata may be incomplete")
	}
	return res
}

// classify maps an HTTP status plus body/title markers to a coarse verdict.
func classify(status int, body, title string) string {
	switch {
	case status == http.StatusOK:
		lowerTitle := strings.ToLower(title)
		for _, it := range interstitialTitles {
			if strings.Contains(lowerTitle, it) {
				return "bot-block-suspect"
			}
		}
		if len(body) < 8<<10 && hasBotBlockMarker(body) {
			return "bot-block-suspect" // small page that is only a challenge
		}
		return "ok"
	case status == http.StatusNotFound || status == http.StatusGone:
		return "not-found"
	case status == http.StatusForbidden || status == http.StatusUnauthorized || status == http.StatusTooManyRequests:
		if hasBotBlockMarker(body) || status == http.StatusForbidden {
			return "bot-block-suspect"
		}
		return "forbidden"
	case status >= 500:
		return "server-error"
	default:
		return fmt.Sprintf("http-%d", status)
	}
}

func hasBotBlockMarker(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range botBlockMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// headSection returns the document up to and including </head> (or the whole
// body when no </head> exists).
func headSection(body string) string {
	loc := headEnd.FindStringIndex(body)
	if loc == nil {
		return body
	}
	return body[:loc[1]]
}

func extractTitle(body string) string {
	m := titleRe.FindStringSubmatch(headSection(body))
	if m == nil {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(m[1]))
}

func extractCanonical(body string) string {
	head := headSection(body)
	for _, tag := range linkRe.FindAllString(head, -1) {
		attrs := tagAttrs(tag)
		if strings.EqualFold(attrs["rel"], "canonical") && attrs["href"] != "" {
			return html.UnescapeString(attrs["href"])
		}
	}
	for _, tag := range metaRe.FindAllString(head, -1) {
		attrs := tagAttrs(tag)
		if strings.EqualFold(attrs["property"], "og:url") && attrs["content"] != "" {
			return html.UnescapeString(attrs["content"])
		}
	}
	return ""
}

func tagAttrs(tag string) map[string]string {
	attrs := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
		val := strings.Trim(m[2], `"'`)
		attrs[strings.ToLower(m[1])] = val
	}
	return attrs
}

func hostOf(rawurl string) string {
	rest := stripScheme(rawurl)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return strings.ToLower(rest)
}

func printCheckResult(w io.Writer, res checkResult) {
	fmt.Fprintf(w, "INPUT  %s\nSTATUS %s", res.Input, res.Status)
	if res.HTTPStatus != 0 {
		fmt.Fprintf(w, " (HTTP %d)", res.HTTPStatus)
	}
	fmt.Println()
	if res.Error != "" {
		fmt.Fprintf(w, "ERROR  %s\n", res.Error)
	}
	if len(res.Redirects) > 0 {
		for _, h := range res.Redirects {
			fmt.Fprintf(w, "REDIR  %d %s\n", h.Status, h.URL)
		}
		fmt.Fprintf(w, "FINAL  %s\n", res.FinalURL)
	}
	if res.Title != "" {
		fmt.Fprintf(w, "TITLE  %s\n", res.Title)
	}
	if res.Canonical != "" {
		flag := ""
		if res.CanonicalDiffers {
			flag = "  <-- differs from submitted/final URL"
		}
		fmt.Fprintf(w, "CANON  %s%s\n", res.Canonical, flag)
	}
	for _, note := range res.Notes {
		fmt.Fprintf(w, "NOTE   %s\n", note)
	}
}
