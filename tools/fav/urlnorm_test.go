package main

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"scheme and www collapse", "https://www.example.com/x", "example.com/x"},
		{"http equals https", "http://example.com/x", "example.com/x"},
		{"trailing slash", "https://example.com/x/", "example.com/x"},
		{"root slash", "https://example.com/", "example.com"},
		{"tracking params stripped", "https://example.com/x?utm_source=blog.quast&utm_medium=email", "example.com/x"},
		{"real query kept and sorted", "https://example.com/x?b=2&utm_source=y&a=1", "example.com/x?a=1&b=2"},
		{"fragment dropped", "https://example.com/x#section", "example.com/x"},
		{"host case", "HTTPS://WWW.Example.COM/X", "example.com/X"},
		{
			"wayback unwrap",
			"https://web.archive.org/web/20260820031453/https://www.salon.com/2001/03/23/wizards/",
			"salon.com/2001/03/23/wizards",
		},
		{
			"wayback http host and flags suffix",
			"http://web.archive.org/web/20250906201441id_/http://www.brightjourney.com/q/okcupid-write-web-server",
			"brightjourney.com/q/okcupid-write-web-server",
		},
		{
			"utm suffix matches stored clean url",
			"https://intronetworks.cs.luc.edu/current/html/tokenbucket.html?utm_source=blog.quast",
			"intronetworks.cs.luc.edu/current/html/tokenbucket.html",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalize(tc.in); got != tc.want {
				t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeEquivalence(t *testing.T) {
	pairs := [][2]string{
		{"https://www.kalzumeus.com/2012/01/23/salary-negotiation/", "http://kalzumeus.com/2012/01/23/salary-negotiation"},
		{"https://web.archive.org/web/20260803014506/https://stripe.com/blog/rate-limiters", "https://stripe.com/blog/rate-limiters"},
	}
	for _, pair := range pairs {
		if normalize(pair[0]) != normalize(pair[1]) {
			t.Errorf("expected %q and %q to normalize equal, got %q vs %q",
				pair[0], pair[1], normalize(pair[0]), normalize(pair[1]))
		}
	}
}

func TestNormalizeYouTube(t *testing.T) {
	bare := "youtube.com/watch?v=EDZBYbEwhm8"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"www watch", "https://www.youtube.com/watch?v=EDZBYbEwhm8", bare},
		{"mobile host", "https://m.youtube.com/watch?v=EDZBYbEwhm8", bare},
		{"short host with timestamp", "https://youtu.be/EDZBYbEwhm8?t=899", bare},
		{"short host with si", "https://youtu.be/EDZBYbEwhm8?si=abc123", bare},
		{"junk params dropped", "https://www.youtube.com/watch?v=EDZBYbEwhm8&ra=m", bare},
		{"playlist params dropped", "https://www.youtube.com/watch?v=EDZBYbEwhm8&list=PLabc&index=10", bare},
		{"shorts", "https://www.youtube.com/shorts/EDZBYbEwhm8", bare},
		{"embed", "https://www.youtube-nocookie.com/embed/EDZBYbEwhm8", bare},
		{"music host", "https://music.youtube.com/watch?v=EDZBYbEwhm8", bare},
		// Non-video YouTube URLs keep generic normalization.
		{"channel page untouched", "https://www.youtube.com/@Deeplearningai/playlists", "youtube.com/@Deeplearningai/playlists"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalize(tc.in); got != tc.want {
				t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeApplePodcasts(t *testing.T) {
	episode := "podcasts.apple.com/id1482889491?i=1000574913111"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"show slug", "https://podcasts.apple.com/us/podcast/level-up-engineering/id1482889491?i=1000574913111", episode},
		{"episode slug (same identity)", "https://podcasts.apple.com/us/podcast/guide-to-building-cross-functional-collaboration-in/id1482889491?i=1000574913111", episode},
		{"extra params dropped", "https://podcasts.apple.com/us/podcast/acquired/id1050462261?i=1000710802439&uo=4", "podcasts.apple.com/id1050462261?i=1000710802439"},
		{"show page has no i", "https://podcasts.apple.com/us/podcast/your-next-tech-startup/id1809567381", "podcasts.apple.com/id1809567381"},
		// Show page and episode of the same show stay distinct.
		{"episode differs from show", "https://podcasts.apple.com/us/podcast/your-next-tech-startup/id1809567381?i=1000746330337", "podcasts.apple.com/id1809567381?i=1000746330337"},
		// Genre pages carry an /id too and reduce the same way.
		{"genre page", "https://podcasts.apple.com/us/genre/podcasts-technology/id1318", "podcasts.apple.com/id1318"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalize(tc.in); got != tc.want {
				t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestTokenContainment(t *testing.T) {
	a := titleTokens("Scaling your API with rate limiters")
	b := titleTokens("Stripe: Scaling your API with rate limiters")
	if got := tokenContainment(a, b); got != 1.0 {
		t.Errorf("containment = %v, want 1.0 (subset)", got)
	}
	c := titleTokens("Death to the Minotaur")
	if got := tokenContainment(a, c); got != 0 {
		t.Errorf("containment = %v, want 0 (disjoint)", got)
	}
	if got := tokenContainment(a, map[string]bool{}); got != 0 {
		t.Errorf("containment with empty set = %v, want 0", got)
	}
}
