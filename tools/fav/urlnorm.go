package main

import (
	"net/url"
	"strings"
)

// trackingParams are query keys stripped before comparison: they describe the
// click that fetched the page, not the content itself.
var trackingParams = map[string]bool{
	"utm_source": true, "utm_medium": true, "utm_campaign": true,
	"utm_term": true, "utm_content": true, "utm_id": true,
	"fbclid": true, "gclid": true, "dclid": true, "igshid": true,
	"mc_cid": true, "mc_eid": true, "si": true, "spm": true,
	"_ga": true, "ref_src": true,
}

// unwrapWayback reduces a web.archive.org snapshot URL to the URL it captured,
// e.g. web.archive.org/web/20260820031453/https://example.com/x -> the inner
// URL. Non-Wayback URLs are returned unchanged.
func unwrapWayback(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	host := strings.ToLower(u.Hostname())
	if host != "web.archive.org" && host != "archive.org" {
		return raw
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 3)
	if len(parts) != 3 || parts[0] != "web" {
		return raw
	}
	ts := strings.TrimRight(parts[1], "abcdefghijklmnopqrstuvwxyz_")
	if ts == "" {
		return raw
	}
	for i := 0; i < len(ts); i++ {
		if ts[i] < '0' || ts[i] > '9' {
			return raw
		}
	}
	inner := parts[2]
	if !strings.HasPrefix(inner, "http://") && !strings.HasPrefix(inner, "https://") {
		inner = "https://" + inner
	}
	return inner
}

// normalize reduces a URL to a canonical comparison form: scheme-insensitive
// (http == https), host lowercased with any leading "www." removed, trailing
// slashes trimmed, tracking query params dropped, remaining query sorted, and
// the fragment removed. Wayback snapshot URLs are unwrapped first, so an
// original URL compares equal to a snapshot of itself.
func normalize(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = unwrapWayback(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		u, err = url.Parse("https://" + raw)
		if err != nil {
			return strings.ToLower(raw)
		}
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	path := strings.TrimRight(u.Path, "/")

	var b strings.Builder
	b.WriteString(host)
	b.WriteString(path)
	q := u.Query()
	for key := range q {
		if trackingParams[strings.ToLower(key)] {
			delete(q, key)
		}
	}
	if len(q) > 0 {
		b.WriteString("?")
		b.WriteString(q.Encode()) // Encode sorts keys
	}
	return b.String()
}

// stopTokens are dropped before title matching, along with tokens shorter
// than three characters.
var stopTokens = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "with": true,
	"you": true, "your": true, "how": true, "why": true, "what": true,
	"that": true, "this": true, "from": true, "into": true, "our": true,
}

// titleTokens splits a title into a lowercase word set for fuzzy matching.
func titleTokens(s string) map[string]bool {
	out := map[string]bool{}
	var cur strings.Builder
	flush := func() {
		tok := cur.String()
		cur.Reset()
		if len(tok) >= 3 && !stopTokens[tok] {
			out[tok] = true
		}
	}
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// tokenContainment returns |intersection| / min(|a|, |b|): 1.0 when the
// smaller token set is fully contained in the larger one.
func tokenContainment(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	small, large := a, b
	if len(b) < len(a) {
		small, large = b, a
	}
	shared := 0
	for tok := range small {
		if large[tok] {
			shared++
		}
	}
	return float64(shared) / float64(len(small))
}
