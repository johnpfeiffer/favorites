package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTypes(t *testing.T) {
	for _, tc := range []struct {
		tags []string
		want any
	}{
		{[]string{"History", "Podcast"}, "PodcastEpisode"},
		{[]string{"Blog"}, "BlogPosting"},
		{[]string{"Video"}, "VideoObject"},
		{[]string{"Paper"}, "ScholarlyArticle"},
		{[]string{"TechArticle"}, "TechArticle"},
		{[]string{"Engineering"}, "Article"},
		{[]string{"Book", "Blog", "Article"}, "Book"},
		{[]string{"Article", "Blog"}, "BlogPosting"},
		{[]string{"Paper", "Video"}, []string{"ScholarlyArticle", "VideoObject"}},
		{[]string{"Book", "Podcast"}, []string{"Book", "PodcastEpisode"}},
		{[]string{"Blog", "Podcast"}, []string{"BlogPosting", "PodcastEpisode"}},
		{[]string{"Podcast", "History", "Podcast"}, "PodcastEpisode"},
	} {
		got, _ := classify(tc.tags)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%v: got %v, want %v", tc.tags, got, tc.want)
		}
	}
}

const fixture = `{"History":[{"title":"Book & podcast","url":"https://example.com/?a=1&b=2","alternate-url":"https://podcasts.apple.com/example","published":null,"tags":["History","Book","Podcast"]},{"title":"Other","url":"https://example.com/other","tags":[]}]}`

func TestFieldPreservation(t *testing.T) {
	got, warnings, fallbacks, err := convert([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || fallbacks != 1 {
		t.Fatalf("warnings=%v fallbacks=%d", warnings, fallbacks)
	}
	raw, err := encode(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"@context":"https://schema.org","@type":"ItemList","name":"History","itemListElement":[{"@type":["Book","PodcastEpisode"],"name":"Book & podcast","url":"https://example.com/?a=1&b=2","archivedAt":"https://podcasts.apple.com/example","datePublished":null,"keywords":["History","Book","Podcast"]},{"@type":"Article","name":"Other","url":"https://example.com/other","keywords":[]}]}`
	var actual, expected any
	if err := json.Unmarshal(raw, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("unexpected output: %s", raw)
	}
}

func TestMigrationWorkflow(t *testing.T) {
	input := t.TempDir()
	output := filepath.Join(t.TempDir(), "generated")
	source := filepath.Join(input, "history.json")
	if err := os.WriteFile(source, []byte(fixture), 0644); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	invoke := func(command string, extra ...string) error {
		args := append([]string{command, "--input", input, "--output", output}, extra...)
		return run(args, &log, &log)
	}
	if err := invoke("dry-run"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("dry run created output directory")
	}
	if !strings.Contains(log.String(), "ambiguous media types") {
		t.Fatal("missing ambiguity report")
	}
	if err := invoke("convert", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("--dry-run wrote output")
	}
	if err := invoke("convert"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(output, "history.jsonld")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := invoke("verify"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := invoke("verify"); err == nil {
		t.Fatal("verification accepted corrupted output")
	}
	if err := invoke("convert"); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("repeat conversion changed output")
	}
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != fixture {
		t.Fatal("source modified")
	}
}

func TestRejectsUnmappedFieldsAndMalformedInput(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"A":[],"B":[]}`, `{"A":null}`,
		`{"A":[{"title":"x","url":"https://example.com","tags":[],"extra":123}]}`,
		`{"A":[{"title":"x","url":"https://example.com","tags":null}]}`,
		`{"A":[{"title":"x","url":"https://example.com","tags":[],"published":123}]}`,
	} {
		if _, _, _, err := convert([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestValidatesAllSourcesBeforeWriting(t *testing.T) {
	input := t.TempDir()
	output := filepath.Join(t.TempDir(), "output")
	for name, raw := range map[string]string{"a.json": fixture, "z.json": "invalid"} {
		if err := os.WriteFile(filepath.Join(input, name), []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var log bytes.Buffer
	if err := run([]string{"convert", "--input", input, "--output", output}, &log, &log); err == nil {
		t.Fatal("accepted invalid source")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("wrote before validating all sources")
	}
}
