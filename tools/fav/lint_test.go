package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLintFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func hasRule(findings []lintFinding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func TestLintCleanFile(t *testing.T) {
	dir := writeLintFixture(t, map[string]string{
		"engineering.jsonld": `{"@context": "https://schema.org", "@type": "ItemList", "name": "Engineering", "itemListElement": [{
			"@type": "Article",
			"name": "Stripe: Scaling your API with rate limiters",
			"url": "https://stripe.com/blog/rate-limiters",
			"archivedAt": "https://web.archive.org/web/20260803014506/https://stripe.com/blog/rate-limiters",
			"datePublished": "2017-03-30",
			"keywords": ["Engineering", "Scalability", "Article"]
		}]}`,
	})
	findings, _, err := lintContent(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf("clean fixture produced findings: %+v", findings)
	}
}

func TestLintRules(t *testing.T) {
	dir := writeLintFixture(t, map[string]string{
		"engineering.jsonld": `{"@context": "https://schema.org", "@type": "ItemList", "name": "Engineering", "itemListElement": [
			{"@type": "Article", "name": "A post", "url": "https://example.com/a", "datePublished": "2020-13-01", "keywords": ["Engineering", "Article"]},
			{"@type": "Article", "name": "B post", "url": "https://example.com/a?utm_source=x", "datePublished": null, "keywords": ["Engineering", "Article"]},
			{"@type": "Article", "name": "Wrong first tag", "url": "https://example.com/c", "datePublished": null, "keywords": ["Scalability", "Engineering", "Article"]},
			{"@type": "Article", "name": "Media not last", "url": "https://example.com/d", "datePublished": null, "keywords": ["Engineering", "Article", "Scalability"]},
			{"@type": "VideoObject", "name": "Year paren (1982 lecture)", "url": "https://example.com/e", "datePublished": null, "keywords": ["Engineering", "Video"]},
			{"@type": "Article", "name": "No media tag", "url": "https://example.com/f", "datePublished": null, "keywords": ["Engineering", "Scalability"]},
			{"@type": "Article", "name": "Same alt", "url": "https://example.com/g", "archivedAt": "https://www.example.com/g/", "datePublished": null, "keywords": ["Engineering", "Article"]}
		]}`,
	})
	findings, _, err := lintContent(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{
		"published-format", "duplicate-url", "category-first",
		"media-type-last", "title-year-parenthetical", "media-type-missing",
		"alternate-equals-url",
	} {
		if !hasRule(findings, rule) {
			t.Errorf("rule %s did not fire; findings: %+v", rule, findings)
		}
	}
}

func TestLintHNExemption(t *testing.T) {
	dir := writeLintFixture(t, map[string]string{
		"engineering.jsonld": `{"@context": "https://schema.org", "@type": "ItemList", "name": "Engineering", "itemListElement": [
			{"@type": "Article", "name": "HN Discussion: What are some examples of good database schema designs?", "url": "https://news.ycombinator.com/item?id=22324691", "datePublished": "2020-02-14", "keywords": ["Engineering", "Database"]}
		]}`,
	})
	findings, _, err := lintContent(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hasRule(findings, "media-type-missing") || hasRule(findings, "hn-title-convention") {
		t.Errorf("HN convention entry flagged: %+v", findings)
	}
}

func TestLintTagCaseCollision(t *testing.T) {
	dir := writeLintFixture(t, map[string]string{
		"engineering.jsonld": `{"@context": "https://schema.org", "@type": "ItemList", "name": "Engineering", "itemListElement": [
			{"@type": "Article", "name": "One", "url": "https://example.com/1", "datePublished": null, "keywords": ["Engineering", "Golang", "Article"]},
			{"@type": "Article", "name": "Two", "url": "https://example.com/2", "datePublished": null, "keywords": ["Engineering", "golang", "Article"]}
		]}`,
	})
	findings, _, err := lintContent(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRule(findings, "tag-case-collision") {
		t.Errorf("tag-case-collision did not fire: %+v", findings)
	}
}
