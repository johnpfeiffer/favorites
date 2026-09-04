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
		"engineering.json": `{"Engineering": [{
			"title": "Stripe: Scaling your API with rate limiters",
			"url": "https://stripe.com/blog/rate-limiters",
			"alternate-url": "https://web.archive.org/web/20260803014506/https://stripe.com/blog/rate-limiters",
			"published": "2017-03-30",
			"tags": ["Engineering", "Scalability", "Article"]
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
		"engineering.json": `{"Engineering": [
			{"title": "A post", "url": "https://example.com/a", "published": "2020-13-01", "tags": ["Engineering", "Article"]},
			{"title": "B post", "url": "https://example.com/a?utm_source=x", "published": null, "tags": ["Engineering", "Article"]},
			{"title": "Wrong first tag", "url": "https://example.com/c", "published": null, "tags": ["Scalability", "Engineering", "Article"]},
			{"title": "Media not last", "url": "https://example.com/d", "published": null, "tags": ["Engineering", "Article", "Scalability"]},
			{"title": "Year paren (1982 lecture)", "url": "https://example.com/e", "published": null, "tags": ["Engineering", "Video"]},
			{"title": "No media tag", "url": "https://example.com/f", "published": null, "tags": ["Engineering", "Scalability"]},
			{"title": "Same alt", "url": "https://example.com/g", "alternate-url": "https://www.example.com/g/", "published": null, "tags": ["Engineering", "Article"]}
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
		"engineering.json": `{"Engineering": [
			{"title": "HN Discussion: What are some examples of good database schema designs?", "url": "https://news.ycombinator.com/item?id=22324691", "published": "2020-02-14", "tags": ["Engineering", "Database"]}
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
		"engineering.json": `{"Engineering": [
			{"title": "One", "url": "https://example.com/1", "published": null, "tags": ["Engineering", "Golang", "Article"]},
			{"title": "Two", "url": "https://example.com/2", "published": null, "tags": ["Engineering", "golang", "Article"]}
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
