package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// snapshotInfo describes one usable Wayback capture.
type snapshotInfo struct {
	URL       string `json:"url"`
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"` // "availability" or "cdx"
	Variant   string `json:"variant,omitempty"`
}

type waybackResult struct {
	Input    string        `json:"input"`
	Status   string        `json:"status"` // found | none | error
	Latest   *snapshotInfo `json:"latest,omitempty"`
	Earliest *snapshotInfo `json:"earliest,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// availabilityResp mirrors archive.org/wayback/available.
type availabilityResp struct {
	ArchivedSnapshots struct {
		Closest struct {
			Available bool   `json:"available"`
			URL       string `json:"url"`
			Timestamp string `json:"timestamp"`
			Status    string `json:"status"`
		} `json:"closest"`
	} `json:"archived_snapshots"`
}

func cmdWayback(args []string) error {
	fs := flag.NewFlagSet("wayback", flag.ContinueOnError)
	mode := fs.String("mode", "latest", "which snapshot to find: latest | earliest | both")
	delay := fs.Duration("delay", 8*time.Second, "minimum delay between requests (be polite)")
	backoff := fs.Duration("backoff", 25*time.Second, "backoff after a 429/503 or network error")
	retries := fs.Int("retries", 3, "retries per request after transient failures")
	timeout := fs.Duration("timeout", 30*time.Second, "per-request timeout")
	asJSON := fs.Bool("json", false, "emit a JSON array instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *mode != "latest" && *mode != "earliest" && *mode != "both" {
		return fmt.Errorf("invalid --mode %q (want latest|earliest|both)", *mode)
	}
	inputs, err := readInputs(fs.Args())
	if err != nil {
		return err
	}
	client := newPoliteClient(
		"fav/"+version+" (favorites repo wayback checker; +https://github.com/johnpfeiffer/favorites)",
		*delay, *backoff, *retries, *timeout)

	modes := []string{*mode}
	if *mode == "both" {
		modes = []string{"latest", "earliest"}
	}

	results := make([]waybackResult, 0, len(inputs))
	for _, in := range inputs {
		res := waybackResult{Input: in.Target, Status: "none"}
		for _, m := range modes {
			snap, err := findSnapshot(client, in.Target, m)
			if err != nil {
				res.Status = "error"
				res.Error = err.Error()
				break
			}
			if snap != nil {
				res.Status = "found"
				if m == "latest" {
					res.Latest = snap
				} else {
					res.Earliest = snap
				}
			}
		}
		results = append(results, res)
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
		printWaybackResult(os.Stdout, res)
	}
	return nil
}

func printWaybackResult(w io.Writer, res waybackResult) {
	fmt.Fprintf(w, "INPUT  %s\nSTATUS %s\n", res.Input, res.Status)
	if res.Error != "" {
		fmt.Fprintf(w, "ERROR  %s\n", res.Error)
	}
	for _, pair := range []struct {
		label string
		snap  *snapshotInfo
	}{{"LATEST", res.Latest}, {"EARLIEST", res.Earliest}} {
		if pair.snap == nil {
			continue
		}
		variant := ""
		if pair.snap.Variant != "" {
			variant = " (variant " + pair.snap.Variant + ")"
		}
		fmt.Fprintf(w, "%s %s\n       ts=%s source=%s%s\n",
			pair.label, pair.snap.URL, pair.snap.Timestamp, pair.snap.Source, variant)
	}
}

// findSnapshot locates the latest or earliest 200-status capture of rawurl,
// trying URL variants (www toggle, trailing-slash toggle) when the exact form
// has none.
func findSnapshot(client *politeClient, rawurl, mode string) (*snapshotInfo, error) {
	for i, variant := range urlVariants(rawurl) {
		stripped := stripScheme(variant)
		snap, err := availabilityLookup(client, stripped)
		if err != nil {
			return nil, err
		}
		if snap == nil {
			snap, err = cdxLookup(client, stripped, mode)
			if err != nil {
				return nil, err
			}
		}
		if snap != nil {
			if i > 0 {
				snap.Variant = variant
			}
			return snap, nil
		}
	}
	return nil, nil
}

// stripScheme removes "http://" or "https://" per the skill's convention of
// querying Wayback APIs with the scheme-less URL.
func stripScheme(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	return raw
}

// urlVariants returns the input followed by fallback spellings: www toggled,
// then trailing slash toggled. Duplicates are dropped.
func urlVariants(raw string) []string {
	raw = strings.TrimSpace(raw)
	variants := []string{raw}
	seen := map[string]bool{raw: true}
	add := func(v string) {
		if !seen[v] {
			seen[v] = true
			variants = append(variants, v)
		}
	}
	if strings.Contains(raw, "://www.") {
		add(strings.Replace(raw, "://www.", "://", 1))
	} else if strings.Contains(raw, "://") {
		add(strings.Replace(raw, "://", "://www.", 1))
	}
	if strings.HasSuffix(raw, "/") {
		add(strings.TrimRight(raw, "/"))
	} else {
		add(raw + "/")
	}
	return variants
}

func availabilityLookup(client *politeClient, stripped string) (*snapshotInfo, error) {
	u := "https://archive.org/wayback/available?url=" + url.QueryEscape(stripped)
	res, err := client.get(u, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("availability API: %w", err)
	}
	if res.Status != http.StatusOK {
		return nil, fmt.Errorf("availability API: HTTP %d", res.Status)
	}
	var parsed availabilityResp
	if err := json.Unmarshal(res.Body, &parsed); err != nil {
		return nil, fmt.Errorf("availability API: %w", err)
	}
	c := parsed.ArchivedSnapshots.Closest
	if !c.Available || c.URL == "" || c.Status != "200" {
		return nil, nil
	}
	return &snapshotInfo{
		URL:       httpsWayback(c.URL),
		Timestamp: c.Timestamp,
		Source:    "availability",
	}, nil
}

// cdxLookup queries the CDX API for the latest (limit=-1) or earliest
// (limit=1) capture with status 200.
func cdxLookup(client *politeClient, stripped, mode string) (*snapshotInfo, error) {
	limit := "-1"
	if mode == "earliest" {
		limit = "1"
	}
	u := "https://web.archive.org/cdx/search/cdx?output=json&filter=statuscode:200" +
		"&fl=timestamp,original,statuscode&limit=" + limit +
		"&url=" + url.QueryEscape(stripped)
	res, err := client.get(u, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("CDX API: %w", err)
	}
	if res.Status != http.StatusOK {
		return nil, fmt.Errorf("CDX API: HTTP %d", res.Status)
	}
	var rows [][]string
	if err := json.Unmarshal(res.Body, &rows); err != nil {
		return nil, fmt.Errorf("CDX API: %w", err)
	}
	if len(rows) < 2 || len(rows[1]) < 2 {
		return nil, nil
	}
	ts, original := rows[1][0], rows[1][1]
	return &snapshotInfo{
		URL:       "https://web.archive.org/web/" + ts + "/" + original,
		Timestamp: ts,
		Source:    "cdx",
	}, nil
}

// httpsWayback enforces the https scheme on snapshot URLs (the skill stores
// https alternates; the availability API often returns http).
func httpsWayback(u string) string {
	return strings.Replace(u, "http://web.archive.org", "https://web.archive.org", 1)
}
