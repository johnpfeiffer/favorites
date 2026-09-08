package main

import "testing"

func strptr(s string) *string { return &s }

func testStore() *contentStore {
	return &contentStore{entries: []entry{
		{
			Name:          "Stripe: Scaling your API with rate limiters",
			URL:           "https://stripe.com/blog/rate-limiters",
			DatePublished: strptr("2017-03-30"),
			Keywords:      []string{"Engineering", "Scalability", "Article"},
			File:          "engineering.jsonld",
		},
		{
			Name:          "Bright Journey: Why Would OkCupid Write Their Own Web Server?",
			URL:           "https://web.archive.org/web/20250906201441/http://www.brightjourney.com/q/okcupid-write-web-server",
			ArchivedAt:    "https://www.brightjourney.com/q/okcupid-write-web-server",
			DatePublished: strptr("2011-03-10"),
			Keywords:      []string{"Engineering", "Article"},
			File:          "engineering.jsonld",
		},
		{
			Name:          "Kalzumeus: Salary Negotiation - Make More Money, Be More Valued (Patrick McKenzie)",
			URL:           "https://www.kalzumeus.com/2012/01/23/salary-negotiation/",
			DatePublished: strptr("2012-01-23"),
			Keywords:      []string{"People", "Career Development", "Blog"},
			File:          "people.jsonld",
		},
		{
			Name:          "Lenny's Podcast: The future of AI in software development with Inbal Shani",
			URL:           "https://www.lennysnewsletter.com/p/the-future-of-ai-in-software-development",
			ArchivedAt:    "https://podcasts.apple.com/us/podcast/show/id1?i=1000637179313",
			DatePublished: strptr("2023-12-01"),
			Keywords:      []string{"AI", "Podcast"},
			File:          "business.jsonld",
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
		if m.Entry.Name == "" || m.Entry.DatePublished == nil || *m.Entry.DatePublished != "2017-03-30" || len(m.Entry.Keywords) == 0 {
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
		if res.Status != "url-match" || res.Matches[0].Entry.Name == "" {
			t.Errorf("status=%s matches=%v", res.Status, res.Matches)
		}
	})

	t.Run("mirror url matches stored archivedAt only", func(t *testing.T) {
		res := dedupeOne(store, norms, inputLine{Target: "https://podcasts.apple.com/us/podcast/show/id1?i=1000637179313"}, 0.6, 5)
		if res.Status != "archivedAt-match" || res.Matches[0].Field != "archivedAt" {
			t.Errorf("status=%s field=%v", res.Status, res.Matches)
		}
		if res.Matches[0].Entry.File != "business.jsonld" {
			t.Errorf("wrong entry: %+v", res.Matches[0].Entry)
		}
	})

	t.Run("unknown url with name keywords yields name-only", func(t *testing.T) {
		res := dedupeOne(store, norms, inputLine{Target: "https://example.com/new", Rest: "salary negotiation make more money valued"}, 0.6, 5)
		if res.Status != "name-only" {
			t.Errorf("status=%s, want name-only", res.Status)
		}
		if len(res.NameCandidates) == 0 || res.NameCandidates[0].Entry.File != "people.jsonld" {
			t.Errorf("candidates=%+v", res.NameCandidates)
		}
	})

	t.Run("unknown url without keywords is none", func(t *testing.T) {
		res := dedupeOne(store, norms, inputLine{Target: "https://example.com/new"}, 0.6, 5)
		if res.Status != "none" || len(res.Matches) != 0 {
			t.Errorf("status=%s matches=%d", res.Status, len(res.Matches))
		}
	})
}
