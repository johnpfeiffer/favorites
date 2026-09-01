package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// hop records one step of a redirect chain.
type hop struct {
	URL    string `json:"url"`
	Status int    `json:"status"`
}

// fetchResult is the outcome of fetching one URL, redirects included.
type fetchResult struct {
	FinalURL  string `json:"finalUrl"`
	Status    int    `json:"status"`
	Chain     []hop  `json:"chain"`
	Body      []byte `json:"-"`
	Truncated bool   `json:"truncated"`
}

// politeClient issues sequential, rate-limited GETs. A minimum delay separates
// consecutive requests, 429/503 responses and network errors are retried with
// a longer backoff, and redirects are followed manually so the chain can be
// reported. There is no concurrency anywhere: batch tools are not DDoS tools.
type politeClient struct {
	clients []*http.Client // [default, http1.1 fallback]
	delay   time.Duration
	backoff time.Duration
	retries int
	ua      string
	lastReq time.Time
}

func newPoliteClient(ua string, delay, backoff time.Duration, retries int, timeout time.Duration) *politeClient {
	jar, _ := cookiejar.New(nil)
	noRedirect := func(req *http.Request, via []*http.Request) error {
		// Redirects are followed manually so callers can see the chain.
		return http.ErrUseLastResponse
	}
	clients := []*http.Client{{Jar: jar, Timeout: timeout, CheckRedirect: noRedirect}}
	// Some servers drop HTTP/2 streams from non-browser clients; the
	// resolve-published-date playbook falls back to --http1.1, so the second
	// client disables HTTP/2 and is tried on alternate retry attempts.
	t1 := http.DefaultTransport.(*http.Transport).Clone()
	t1.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	clients = append(clients, &http.Client{Jar: jar, Timeout: timeout, Transport: t1, CheckRedirect: noRedirect})
	return &politeClient{
		clients: clients,
		delay:   delay,
		backoff: backoff,
		retries: retries,
		ua:      ua,
	}
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

func (p *politeClient) throttle() {
	if !p.lastReq.IsZero() {
		if wait := p.delay - time.Since(p.lastReq); wait > 0 {
			time.Sleep(wait)
		}
	}
	p.lastReq = time.Now()
}

// request performs one throttled GET, retrying transient failures (network
// errors, 429, 503) up to retries times with backoff between attempts.
func (p *politeClient) request(rawurl string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= p.retries; attempt++ {
		if attempt > 0 {
			time.Sleep(p.backoff)
		}
		p.throttle()
		req, err := http.NewRequest(http.MethodGet, rawurl, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", p.ua)
		req.Header.Set("Accept", "*/*")
		resp, err := p.clients[attempt%len(p.clients)].Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			lastErr = fmt.Errorf("transient HTTP %d", resp.StatusCode)
			resp.Body.Close()
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

// get fetches rawurl, following up to 10 redirects and recording the chain.
// At most maxBody bytes of the final response body are read.
func (p *politeClient) get(rawurl string, maxBody int64) (*fetchResult, error) {
	res := &fetchResult{}
	current := rawurl
	for redirects := 0; ; redirects++ {
		if redirects > 10 {
			return res, fmt.Errorf("too many redirects (last: %s)", current)
		}
		resp, err := p.request(current)
		if err != nil {
			return res, err
		}
		res.Chain = append(res.Chain, hop{URL: current, Status: resp.StatusCode})
		if isRedirect(resp.StatusCode) {
			loc := resp.Header.Get("Location")
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
			if loc == "" {
				return res, fmt.Errorf("redirect %d without Location header from %s", resp.StatusCode, current)
			}
			next, err := resp.Request.URL.Parse(loc)
			if err != nil {
				return res, fmt.Errorf("bad redirect Location %q: %w", loc, err)
			}
			current = next.String()
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		resp.Body.Close()
		if err != nil {
			return res, err
		}
		if int64(len(body)) > maxBody {
			body = body[:maxBody]
			res.Truncated = true
		}
		res.FinalURL = current
		res.Status = resp.StatusCode
		res.Body = body
		return res, nil
	}
}
