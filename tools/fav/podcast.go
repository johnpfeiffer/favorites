package main

import (
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// registryShow mirrors one entry in podcasts/registry.json.
type registryShow struct {
	Slug             string `json:"slug"`
	Name             string `json:"name"`
	AppleID          int64  `json:"appleId"`
	FeedURL          string `json:"feedUrl"`
	HomeURL          string `json:"homeUrl"`
	CanonicalPattern string `json:"canonicalPattern"`
	Notes            string `json:"notes"`
}

type podcastRegistry struct {
	Shows []registryShow `json:"shows"`
}

// episode and podcastIndex mirror podcasts/<slug>.json. Field order matches
// the Python generator so refresh output stays byte-compatible.
type episode struct {
	Title     *string `json:"title"`
	Published *string `json:"published"`
	URL       *string `json:"url"`
}

type podcastIndex struct {
	Show             string    `json:"show"`
	Slug             string    `json:"slug"`
	AppleID          int64     `json:"appleId"`
	FeedURL          string    `json:"feedUrl"`
	HomeURL          string    `json:"homeUrl"`
	CanonicalPattern string    `json:"canonicalPattern"`
	EpisodeCount     int       `json:"episodeCount"`
	Episodes         []episode `json:"episodes"`
}

func cmdPodcast(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("podcast needs a subcommand: lookup | refresh")
	}
	switch args[0] {
	case "lookup":
		return cmdPodcastLookup(args[1:])
	case "refresh":
		return cmdPodcastRefresh(args[1:])
	default:
		return fmt.Errorf("unknown podcast subcommand %q (want lookup | refresh)", args[0])
	}
}

func loadRegistry(repo string) (*podcastRegistry, error) {
	raw, err := os.ReadFile(filepath.Join(repo, "podcasts", "registry.json"))
	if err != nil {
		return nil, err
	}
	var reg podcastRegistry
	if err := json.Unmarshal(raw, &reg); err != nil {
		return nil, fmt.Errorf("registry.json: %w", err)
	}
	return &reg, nil
}

func loadIndex(repo, slug string) (*podcastIndex, error) {
	raw, err := os.ReadFile(filepath.Join(repo, "podcasts", slug+".json"))
	if err != nil {
		return nil, err
	}
	idx, err := loadIndexFromBytes(raw)
	if err != nil {
		return nil, fmt.Errorf("%s.json: %w", slug, err)
	}
	return idx, nil
}

func loadIndexFromBytes(raw []byte) (*podcastIndex, error) {
	var idx podcastIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// selectShows matches a --show filter case-insensitively against slug or name.
// An empty filter selects every show.
func selectShows(reg *podcastRegistry, filter string) ([]registryShow, error) {
	if filter == "" {
		return reg.Shows, nil
	}
	needle := strings.ToLower(filter)
	var matches []registryShow
	for _, s := range reg.Shows {
		if strings.Contains(strings.ToLower(s.Slug), needle) || strings.Contains(strings.ToLower(s.Name), needle) {
			matches = append(matches, s)
		}
	}
	if len(matches) == 0 {
		var slugs []string
		for _, s := range reg.Shows {
			slugs = append(slugs, s.Slug)
		}
		sort.Strings(slugs)
		return nil, fmt.Errorf("no show matching %q; available: %s", filter, strings.Join(slugs, ", "))
	}
	return matches, nil
}

// ---------------------------------------------------------------------------
// lookup
// ---------------------------------------------------------------------------

type lookupMatch struct {
	Published *string `json:"published"`
	Title     *string `json:"title"`
	URL       *string `json:"url"`
}

type lookupResult struct {
	Show             string        `json:"show"`
	Slug             string        `json:"slug"`
	AppleID          int64         `json:"appleId"`
	CanonicalPattern string        `json:"canonicalPattern"`
	Matches          []lookupMatch `json:"matches"`
}

func cmdPodcastLookup(args []string) error {
	fs := flag.NewFlagSet("podcast lookup", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root (contains podcasts/)")
	showFilter := fs.String("show", "", "show slug or name substring (default: search every show)")
	asJSON := fs.Bool("json", false, "emit a JSON array instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	keywords := fs.Args()
	if len(keywords) == 0 {
		return fmt.Errorf("lookup needs at least one title keyword")
	}
	reg, err := loadRegistry(*repo)
	if err != nil {
		return err
	}
	shows, err := selectShows(reg, *showFilter)
	if err != nil {
		return err
	}

	var results []lookupResult
	for _, show := range shows {
		idx, err := loadIndex(*repo, show.Slug)
		if err != nil {
			return err
		}
		res := lookupResult{
			Show:             idx.Show,
			Slug:             idx.Slug,
			AppleID:          idx.AppleID,
			CanonicalPattern: idx.CanonicalPattern,
		}
		for _, ep := range idx.Episodes {
			if ep.Title == nil {
				continue
			}
			if titleMatchesAll(*ep.Title, keywords) {
				res.Matches = append(res.Matches, lookupMatch{ep.Published, ep.Title, ep.URL})
			}
		}
		if len(res.Matches) > 0 || *showFilter != "" {
			results = append(results, res)
		}
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(results)
	}
	if len(results) == 0 {
		fmt.Println("no matches")
		return nil
	}
	for i, res := range results {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("SHOW   %s (slug=%s appleId=%d)\n", res.Show, res.Slug, res.AppleID)
		fmt.Printf("CANON  %s\n", res.CanonicalPattern)
		for _, m := range res.Matches {
			fmt.Printf("  %s | %s\n         %s\n", deref(m.Published), deref(m.Title), deref(m.URL))
		}
	}
	return nil
}

// titleMatchesAll reports whether every keyword appears in the title,
// case-insensitively.
func titleMatchesAll(title string, keywords []string) bool {
	lower := strings.ToLower(title)
	for _, kw := range keywords {
		if !strings.Contains(lower, strings.ToLower(kw)) {
			return false
		}
	}
	return true
}

func deref(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

// ---------------------------------------------------------------------------
// refresh
// ---------------------------------------------------------------------------

// rssItem mirrors the RSS fields the Python index builder reads.
type rssItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	PubDate string `xml:"pubDate"`
	GUID    string `xml:"guid"`
}

type rssFeed struct {
	Items []rssItem `xml:"channel>item"`
}

// dateLayouts covers the RFC 822/1123 variants and ISO shapes seen in podcast
// feeds. Naive timestamps parse as UTC, matching parsedate_to_datetime's
// default; zoned timestamps keep their local date (no UTC shift).
var dateLayouts = []string{
	time.RFC1123Z, // Mon, 02 Jan 2006 15:04:05 -0700
	time.RFC1123,  // Mon, 02 Jan 2006 15:04:05 MST
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	time.RFC822Z,
	time.RFC822,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// parseFeedDate converts a pubDate to an ISO YYYY-MM-DD date, or nil when
// unparseable (mirroring the Python builder's lenient None).
func parseFeedDate(pub string) *string {
	pub = strings.TrimSpace(pub)
	if pub == "" {
		return nil
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, pub); err == nil {
			d := t.Format("2006-01-02")
			return &d
		}
	}
	return nil
}

// parseFeed builds the episode list in feed document order, exactly like the
// Python builder: whitespace-collapsed title, ISO local date, link with a
// guid fallback when the guid is an http(s) URL.
func parseFeed(raw []byte) ([]episode, error) {
	var feed rssFeed
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return nil, err
	}
	episodes := make([]episode, 0, len(feed.Items))
	for _, item := range feed.Items {
		var title, link *string
		if t := strings.Join(strings.Fields(item.Title), " "); t != "" {
			title = &t
		}
		l := strings.TrimSpace(item.Link)
		if l == "" && strings.HasPrefix(strings.TrimSpace(item.GUID), "http") {
			l = strings.TrimSpace(item.GUID)
		}
		if l != "" {
			link = &l
		}
		episodes = append(episodes, episode{Title: title, Published: parseFeedDate(item.PubDate), URL: link})
	}
	return episodes, nil
}

func cmdPodcastRefresh(args []string) error {
	fs := flag.NewFlagSet("podcast refresh", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root (contains podcasts/)")
	showFilter := fs.String("show", "", "only refresh shows whose slug or name contains this")
	check := fs.Bool("check", false, "compare feeds against committed indexes without writing; exit 1 when stale")
	delay := fs.Duration("delay", 2*time.Second, "minimum delay between feed fetches (be polite)")
	backoff := fs.Duration("backoff", 15*time.Second, "backoff after a 429/503 or network error")
	retries := fs.Int("retries", 2, "retries per feed after transient failures")
	timeout := fs.Duration("timeout", 60*time.Second, "per-feed timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("refresh takes no positional arguments (use --show to filter)")
	}
	reg, err := loadRegistry(*repo)
	if err != nil {
		return err
	}
	shows, err := selectShows(reg, *showFilter)
	if err != nil {
		return err
	}
	client := newPoliteClient(
		"fav/"+version+" (favorites repo podcast-index-builder; +https://github.com/johnpfeiffer/favorites)",
		*delay, *backoff, *retries, *timeout)

	stale := false
	failed := false
	for _, show := range shows {
		res, err := client.get(show.FeedURL, 64<<20)
		if err != nil {
			fmt.Printf("FAIL  %s: %v\n", show.Slug, err)
			failed = true
			continue
		}
		if res.Status != 200 {
			fmt.Printf("FAIL  %s: HTTP %d\n", show.Slug, res.Status)
			failed = true
			continue
		}
		episodes, err := parseFeed(res.Body)
		if err != nil {
			fmt.Printf("FAIL  %s: feed parse: %v\n", show.Slug, err)
			failed = true
			continue
		}
		index := podcastIndex{
			Show:             show.Name,
			Slug:             show.Slug,
			AppleID:          show.AppleID,
			FeedURL:          show.FeedURL,
			HomeURL:          show.HomeURL,
			CanonicalPattern: show.CanonicalPattern,
			EpisodeCount:     len(episodes),
			Episodes:         episodes,
		}
		if *check {
			diff, err := diffIndex(*repo, show.Slug, episodes)
			if err != nil {
				fmt.Printf("FAIL  %s: %v\n", show.Slug, err)
				failed = true
				continue
			}
			switch {
			case len(diff.missing) > 0 || len(diff.retitled) > 0 || len(diff.redated) > 0:
				stale = true
				fmt.Printf("STALE %s: index=%d feed=%d missing=%d retitled=%d redated=%d\n",
					show.Slug, diff.committed, len(episodes), len(diff.missing), len(diff.retitled), len(diff.redated))
				for _, ep := range diff.missing {
					fmt.Printf("  NEW   %s | %s\n          %s\n", deref(ep.Published), deref(ep.Title), deref(ep.URL))
				}
				for _, r := range diff.retitled {
					fmt.Printf("  TITLED %s\n    was %s\n          %s\n", deref(r.feed.Title), deref(r.committed.Title), deref(r.feed.URL))
				}
				for _, r := range diff.redated {
					fmt.Printf("  REDATED %s -> %s\n          %s\n", deref(r.committed.Published), deref(r.feed.Published), deref(r.feed.URL))
				}
			case diff.committed != len(episodes):
				fmt.Printf("NOTE  %s: index=%d feed=%d (feed dropped episodes? no missing ones)\n", show.Slug, diff.committed, len(episodes))
			default:
				fmt.Printf("FRESH %s: %d episodes\n", show.Slug, len(episodes))
			}
			continue
		}
		target := filepath.Join(*repo, "podcasts", show.Slug+".json")
		if err := writeIndex(target, &index); err != nil {
			fmt.Printf("FAIL  %s: %v\n", show.Slug, err)
			failed = true
			continue
		}
		fmt.Printf("OK    %s: %d episodes -> %s\n", show.Slug, len(episodes), show.Slug+".json")
	}
	if failed || stale {
		return errReported
	}
	return nil
}

var errReported = fmt.Errorf("one or more shows failed or are stale")

// episodeDrift is a feed episode that matches a committed one by URL but
// whose title or published date has since changed upstream (rebrands retitle
// old episodes; feeds occasionally restamp dates).
type episodeDrift struct {
	feed      episode
	committed episode
}

type indexDiff struct {
	missing   []episode // in the feed, absent from the committed index
	retitled  []episodeDrift
	redated   []episodeDrift
	committed int
}

// diffIndex compares the freshly parsed feed against the committed index.
// Identity: equal URL when both have one, else equal title.
func diffIndex(repo, slug string, feedEpisodes []episode) (*indexDiff, error) {
	idx, err := loadIndex(repo, slug)
	if err != nil {
		return nil, err
	}
	diff := &indexDiff{committed: len(idx.Episodes)}
	for _, fe := range feedEpisodes {
		var match *episode
		for i := range idx.Episodes {
			ce := &idx.Episodes[i]
			if fe.URL != nil && ce.URL != nil && *fe.URL == *ce.URL {
				match = ce
				break
			}
			if fe.Title != nil && ce.Title != nil && *fe.Title == *ce.Title {
				match = ce
				break
			}
		}
		if match == nil {
			diff.missing = append(diff.missing, fe)
			continue
		}
		if fe.Title != nil && match.Title != nil && *fe.Title != *match.Title {
			diff.retitled = append(diff.retitled, episodeDrift{feed: fe, committed: *match})
		}
		if fe.Published != nil && match.Published != nil && *fe.Published != *match.Published {
			diff.redated = append(diff.redated, episodeDrift{feed: fe, committed: *match})
		}
	}
	return diff, nil
}

// writeIndex encodes exactly like the Python builder: two-space indent, no
// HTML escaping, UTF-8 passthrough, single trailing newline.
func writeIndex(path string, idx *podcastIndex) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(idx); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
