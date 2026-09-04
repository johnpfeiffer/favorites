package main

import "testing"

func findCandidate(cs []dateCandidate, source string) *dateCandidate {
	for i := range cs {
		if cs[i].Source == source {
			return &cs[i]
		}
	}
	return nil
}

func TestDatesFromURL(t *testing.T) {
	t.Run("day path", func(t *testing.T) {
		cs := datesFromURL("https://www.kalzumeus.com/2012/01/23/salary-negotiation/")
		c := findCandidate(cs, "url-path")
		if c == nil || c.Date != "2012-01-23" || c.Precision != "day" {
			t.Errorf("got %+v", cs)
		}
	})
	t.Run("mp3 filename date", func(t *testing.T) {
		cs := datesFromURL("https://files.manager-tools.com/private/podcast/mp3/manager-tools-2011-03-07.mp3")
		c := findCandidate(cs, "url-filename")
		if c == nil || c.Date != "2011-03-07" {
			t.Errorf("got %+v", cs)
		}
	})
	t.Run("month-only path", func(t *testing.T) {
		cs := datesFromURL("https://example.com/2016/08/some-post")
		c := findCandidate(cs, "url-path")
		if c == nil || c.Date != "2016-08-01" || c.Precision != "month" {
			t.Errorf("got %+v", cs)
		}
	})
	t.Run("no date", func(t *testing.T) {
		if cs := datesFromURL("https://stripe.com/blog/rate-limiters"); len(cs) != 0 {
			t.Errorf("got %+v", cs)
		}
	})
	t.Run("invalid month rejected", func(t *testing.T) {
		if cs := datesFromURL("https://example.com/2016/13/x"); len(cs) != 0 {
			t.Errorf("got %+v", cs)
		}
	})
}

func TestDatesFromHTML(t *testing.T) {
	body := `<html><head>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Article","datePublished":"2023-08-17T07:00:00.000Z"}</script>
<meta property="article:published_time" content="2023-08-17T12:00:00Z">
</head><body><time datetime="2023-08-17">August 17, 2023</time></body></html>`
	cs := datesFromHTML(body)
	if c := findCandidate(cs, "json-ld:datePublished"); c == nil || c.Date != "2023-08-17" {
		t.Errorf("json-ld: %+v", cs)
	}
	if c := findCandidate(cs, "meta:article:published_time"); c == nil || c.Date != "2023-08-17" {
		t.Errorf("meta: %+v", cs)
	}
	if c := findCandidate(cs, "time-datetime"); c == nil || c.Date != "2023-08-17" {
		t.Errorf("time: %+v", cs)
	}
}

func TestDatesFromHTMLGraphArray(t *testing.T) {
	body := `<html><head><script type="application/ld+json">[{"@type":"WebPage"},{"@type":"Article","datePublished":"2020-02-14"}]</script></head></html>`
	cs := datesFromHTML(body)
	if c := findCandidate(cs, "json-ld:datePublished"); c == nil || c.Date != "2020-02-14" {
		t.Errorf("got %+v", cs)
	}
}

func TestDatesFromHTMLMP3Link(t *testing.T) {
	body := `<html><head></head><body><a href="https://files.manager-tools.com/private/podcast/mp3/manager-tools-2011-03-07.mp3">Download this cast</a></body></html>`
	cs := datesFromHTML(body)
	if c := findCandidate(cs, "mp3-link"); c == nil || c.Date != "2011-03-07" {
		t.Errorf("got %+v", cs)
	}
}

func TestDatesFromPDF(t *testing.T) {
	pdf := append([]byte("%PDF-1.4\n"), []byte(`<x:xmpmeta><rdf:Description><xap:CreateDate>2004-05-03T14:22:00Z</xap:CreateDate></rdf:Description></x:xmpmeta>`)...)
	cs := datesFromPDF(pdf)
	if c := findCandidate(cs, "pdf-xmp"); c == nil || c.Date != "2004-05-03" {
		t.Errorf("got %+v", cs)
	}
	pdf2 := append([]byte("%PDF-1.4\n"), []byte(`<< /CreationDate (D:20040503094955-07'00') >>`)...)
	cs2 := datesFromPDF(pdf2)
	if c := findCandidate(cs2, "pdf-info"); c == nil || c.Date != "2004-05-03" {
		t.Errorf("got %+v", cs2)
	}
}

func TestHNItemID(t *testing.T) {
	if got := hnItemID("https://news.ycombinator.com/item?id=22324691"); got != "22324691" {
		t.Errorf("got %q", got)
	}
	if got := hnItemID("https://example.com/item?id=5"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestAppleTrackID(t *testing.T) {
	if got := appleTrackID("https://podcasts.apple.com/us/podcast/show/id1120964487?i=1000634833899"); got != "1000634833899" {
		t.Errorf("got %q", got)
	}
	if got := appleTrackID("https://changelog.com/gotime/297"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestWikidataTime(t *testing.T) {
	if got := wikidataTime("+2023-01-01T00:00:00Z"); got != "2023-01-01" {
		t.Errorf("got %q", got)
	}
	if got := wikidataTime("+2023-00-00T00:00:00Z"); got != "2023" {
		t.Errorf("got %q", got)
	}
}
