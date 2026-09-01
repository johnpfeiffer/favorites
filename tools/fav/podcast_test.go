package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseFeedDate(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Thu, 31 Jan 2019 22:54:00 -0500", "2019-01-31"}, // local date kept, no UTC shift
		{"Tue, 22 Jan 2024 01:59:00 +0000", "2024-01-22"},
		{"Mon, 2 Jan 2006 15:04:05 MST", "2006-01-02"}, // single-digit day
		{"2023-12-01T08:00:00Z", "2023-12-01"},
		{"2023-12-01", "2023-12-01"},
	}
	for _, tc := range cases {
		got := parseFeedDate(tc.in)
		if got == nil || *got != tc.want {
			t.Errorf("parseFeedDate(%q) = %v, want %q", tc.in, deref(got), tc.want)
		}
	}
	for _, bad := range []string{"", "not a date", "   "} {
		if got := parseFeedDate(bad); got != nil {
			t.Errorf("parseFeedDate(%q) = %q, want nil", bad, *got)
		}
	}
}

func TestParseFeed(t *testing.T) {
	raw := `<?xml version="1.0"?>
<rss version="2.0"><channel>
  <item>
    <title>  Episode  One
	with weird   spacing </title>
    <link>https://example.com/ep1</link>
    <pubDate>Thu, 31 Jan 2019 22:54:00 -0500</pubDate>
    <guid>abc123</guid>
  </item>
  <item>
    <title>Episode Two</title>
    <link></link>
    <pubDate>Fri, 01 Feb 2019 10:00:00 +0000</pubDate>
    <guid>https://example.com/ep2</guid>
  </item>
</channel></rss>`
	episodes, err := parseFeed([]byte(raw))
	if err != nil {
		t.Fatalf("parseFeed: %v", err)
	}
	if len(episodes) != 2 {
		t.Fatalf("got %d episodes, want 2", len(episodes))
	}
	if got := *episodes[0].Title; got != "Episode One with weird spacing" {
		t.Errorf("title collapse = %q", got)
	}
	if got := *episodes[0].Published; got != "2019-01-31" {
		t.Errorf("published = %q", got)
	}
	if got := *episodes[1].URL; got != "https://example.com/ep2" {
		t.Errorf("guid fallback = %q", got)
	}
}

func TestWriteIndexByteCompat(t *testing.T) {
	// Re-encoding a committed index through the Go writer must reproduce the
	// Python builder's byte format (indent 2, no HTML escaping, trailing \n).
	title := "R&D: Tom & Jerry <Special>"
	pub := "2019-01-31"
	url := "https://example.com/ep1"
	idx := &podcastIndex{
		Show:             "Show",
		Slug:             "show",
		AppleID:          42,
		FeedURL:          "https://example.com/feed",
		HomeURL:          "https://example.com",
		CanonicalPattern: "https://example.com/<slug>",
		EpisodeCount:     1,
		Episodes:         []episode{{Title: &title, Published: &pub, URL: &url}},
	}
	path := t.TempDir() + "/show.json"
	if err := writeIndex(path, idx); err != nil {
		t.Fatalf("writeIndex: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"title": "R&D: Tom & Jerry <Special>"`) {
		t.Errorf("HTML escaping leaked into output:\n%s", text)
	}
	if !strings.HasSuffix(text, "}\n") || !strings.HasPrefix(text, "{\n  \"show\": \"Show\"") {
		t.Errorf("format mismatch:\n%s", text)
	}
	// Round-trip: decode must give back the same struct.
	got, err := loadIndexFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.EpisodeCount != 1 || *got.Episodes[0].Title != title {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}
