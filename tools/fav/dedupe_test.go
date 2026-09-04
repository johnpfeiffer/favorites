package main

import "testing"

func strptr(s string) *string { return &s }

func testStore() *contentStore {
	return &contentStore{entries: []entry{
		{
			Title:     "Stripe: Scaling your API with rate limiters",
			URL:       "https://stripe.com/blog/rate-limiters",
			Published: strptr("2017-03-30"),
			Tags:      []string{"Engineering", "Scalability", "Article"},
			File:      "engineering.json",
		},
		{
			Title:        "Bright Journey: Why Would OkCupid Write Their Own Web Server?",
			URL:          "https://web.archive.org/web/20250906201441/http://www.brightjourney.com/q/okcupid-write-web-server",
			AlternateURL: "https://www.brightjourney.com/q/okcupid-write-web-server",
			Published:    strptr("2011-03-10"),
			Tags:         []string{"Engineering", "Article"},
			File:         "engineering.json",
		},
		{
			Title:     "Kalzumeus: Salary Negotiation - Make More Money, Be More Valued (Patrick McKenzie)",
			URL:       "https://www.kalzumeus.com/2012/01/23/salary-negotiation/",
			Published: strptr("2012-01-23"),
			Tags:      []string{"People", "Career Development", "Blog"},
			File:      "people.json",
		},
		{
			Title:        "Lenny's Podcast: The future of AI in software development with Inbal Shani",
			URL:          "https://www.lennysnewsletter.com/p/the-future-of-ai-in-software-development",
			AlternateURL: "https://podcasts.apple.com/us/podcast/show/id1?i=1000637179313",
			Published:    strptr("2023-12-01"),
			Tags:         []string{"AI", "Podcast"},
			File:         "business.json",
		},
	}}
}

func TestDedupeOne(t *testing.T) {
	store := testStore()
	norms := normalizeStore(store)

	t.Run("exact url match returns full entry", func(t *testing.T) {
		res := dedupeOne(store, norms, inputLine{Target: "https://stripe.com/blog/rate-limiters"}, 0.6, 5)
		if res.Status != "url-match" || len(res.Matches) != 1 {
			t.Fatalf("status=%s matches=%d", res.Status, len(res.Matches))
		}
		m := res.Matches[0]
		if m.Entry.Title == "" || m.Entry.Published == nil || *m.Entry.Published != "2017-03-30" || len(m.Entry.Tags) == 0 {
			t.Errorf("match does not carry full entry values: %+v", m.Entry)
		}
	})

	t.Run("utm-tracked url normalizes to stored canonical", func(t *testing.T) {
		res := dedupeOne(store, norms, inputLine{Target: "https://www.stripe.com/blog/rate-limiters?utm_source=blog.quast"}, 0.6, 5)
		if res.Status != "url-match" {
			t.Errorf("status=%s, want url-match", res.Status)
		}
	})

	t.Run("wayback-flipped canonical matches submitted original", func(t *testing.T) {
		// The stored canonical is a Wayback snapshot of the defunct
		// brightjourney page; unwrapping makes the submitted original equal
		// the canonical's content, which is the stronger dupe signal.
		res := dedupeOne(store, norms, inputLine{Target: "http://www.brightjourney.com/q/okcupid-write-web-server"}, 0.6, 5)
		if res.Status != "url-match" || res.Matches[0].Entry.Title == "" {
			t.Errorf("status=%s matches=%v", res.Status, res.Matches)
		}
	})

	t.Run("mirror url matches stored alternate-url only", func(t *testing.T) {
		res := dedupeOne(store, norms, inputLine{Target: "https://podcasts.apple.com/us/podcast/show/id1?i=1000637179313"}, 0.6, 5)
		if res.Status != "alternate-match" || res.Matches[0].Field != "alternate-url" {
			t.Errorf("status=%s field=%v", res.Status, res.Matches)
		}
		if res.Matches[0].Entry.File != "business.json" {
			t.Errorf("wrong entry: %+v", res.Matches[0].Entry)
		}
	})

	t.Run("unknown url with title keywords yields title-only", func(t *testing.T) {
		res := dedupeOne(store, norms, inputLine{Target: "https://example.com/new", Rest: "salary negotiation make more money valued"}, 0.6, 5)
		if res.Status != "title-only" {
			t.Errorf("status=%s, want title-only", res.Status)
		}
		if len(res.TitleCandidates) == 0 || res.TitleCandidates[0].Entry.File != "people.json" {
			t.Errorf("candidates=%+v", res.TitleCandidates)
		}
	})

	t.Run("unknown url without keywords is none", func(t *testing.T) {
		res := dedupeOne(store, norms, inputLine{Target: "https://example.com/new"}, 0.6, 5)
		if res.Status != "none" || len(res.Matches) != 0 {
			t.Errorf("status=%s matches=%d", res.Status, len(res.Matches))
		}
	})
}
