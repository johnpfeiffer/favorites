package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGraphFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	graphDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(graphDir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(graphDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("entities.json", `{
  "entities": [
    {
      "id": "B82DE2C3-1545-46AF-803D-B9A68E8901DC",
      "name": "Person"
    },
    {
      "id": "0D5C0F7E-3E43-4E63-9C38-4C1E6A7B2F10",
      "name": "Tag"
    },
    {
      "id": "6A1B8E52-9D7F-4B0C-8E3A-2F4D5C6B7A81",
      "name": "Company"
    },
    {
      "id": "92664F80-065B-4640-909A-7AB25D524FB0",
      "name": "Wizards of the Coast"
    }
  ]
}`)
	write("edges.json", `{
  "edges": [
    {
      "source": "C7ECBE6B-5256-422F-A7CC-46E870A9C300",
      "target": "92664F80-065B-4640-909A-7AB25D524FB0",
      "type": "Founder_of"
    }
  ]
}`)
	for _, spec := range classEdgeSpecs {
		write(spec.File, `{
  "edges": []
}`)
	}
	contentDir := filepath.Join(dir, "content")
	if err := os.MkdirAll(contentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "history.jsonld"), []byte(`{
  "@context": "https://schema.org",
  "@type": "ItemList",
  "name": "History",
  "itemListElement": [
    {"@type": "Article", "name": "WotC", "url": "https://example.com/w", "datePublished": null, "keywords": ["History", "Wizards of the Coast", "Article"]}
  ]
}`), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGraphAddEdge(t *testing.T) {
	dir := writeGraphFixture(t)
	g, err := loadGraph(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Reuse existing entity case-insensitively; create the missing one.
	if id, created, err := g.ensureEntity("wizards of the coast"); err != nil || created || id != "92664F80-065B-4640-909A-7AB25D524FB0" {
		t.Errorf("reuse failed: id=%s created=%v err=%v", id, created, err)
	}
	id1, created, err := g.ensureEntity("Peter Adkison")
	if err != nil || !created {
		t.Fatalf("create failed: %v %v", err, created)
	}
	if _, _, err := g.ensureEntity("peter adkison"); err != nil {
		t.Fatal(err)
	}
	if got := len(g.entities); got != 5 {
		t.Errorf("entities=%d, want 5 (second add reused)", got)
	}
	_ = id1
}

func readEdges(t *testing.T, dir, name string) []graphEdge {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "graph", name))
	if err != nil {
		t.Fatal(err)
	}
	var f edgeFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f.Edges
}

func TestGraphAddEdgeRoutesClassEdges(t *testing.T) {
	dir := writeGraphFixture(t)
	err := cmdGraphAddEdge([]string{"--repo", dir,
		"Wizards of the Coast", "Is_a_Tag", "Tag",
		"Wizards of the Coast", "Is_a_Company", "Company",
		"Peter Adkison", "Is_a_Person", "Person",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range classEdgeSpecs {
		edges := readEdges(t, dir, spec.File)
		if len(edges) != 1 || edges[0].Type != spec.Type {
			t.Errorf("%s: got %+v, want one %s edge", spec.File, edges, spec.Type)
		}
	}
	if got := readEdges(t, dir, "edges.json"); len(got) != 1 {
		t.Errorf("edges.json changed: %+v", got)
	}
}

func TestGraphAddEdgeChecks(t *testing.T) {
	cases := []struct {
		name    string
		triple  []string
		wantErr bool
	}{
		{"tag not in content", []string{"Peter Adkison", "Is_a_Tag", "Tag"}, true},
		{"wrong class target", []string{"Wizards of the Coast", "Is_a_Company", "Tag"}, true},
		{"class entity as source", []string{"Tag", "Is_a_Company", "Company"}, true},
		// Lowercase input resolves to the stored entity spelling, which is the
		// exact content tag.
		{"stored spelling satisfies tag check", []string{"wizards of the coast", "Is_a_Tag", "Tag"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := writeGraphFixture(t)
			err := cmdGraphAddEdge(append([]string{"--repo", dir}, c.triple...))
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, c.wantErr)
			}
			raw, _ := os.ReadFile(filepath.Join(dir, "graph", "entities.json"))
			if strings.Contains(string(raw), "Peter Adkison") {
				t.Error("rejected triple left a stray entity")
			}
		})
	}
}

func TestWritePyJSONFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "entities.json")
	v := entityFile{[]graphEntity{{ID: "X", Name: "Łukasz & friends <3"}}}
	if err := writePyJSON(path, v); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	// Python ensure_ascii style: non-ASCII escaped, <>& raw, indent 2,
	// no trailing newline.
	if !strings.Contains(text, `"\u0141ukasz & friends <3"`) {
		t.Errorf("escaping mismatch:\n%s", text)
	}
	if strings.HasSuffix(text, "\n") {
		t.Errorf("unexpected trailing newline")
	}
	if !strings.HasPrefix(text, "{\n  \"entities\": [") {
		t.Errorf("indent mismatch:\n%s", text)
	}
	// And it must still be valid JSON that decodes back.
	var back entityFile
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Entities[0].Name != "Łukasz & friends <3" {
		t.Errorf("round-trip got %q", back.Entities[0].Name)
	}
}

func TestNewUUID4(t *testing.T) {
	id, err := newUUID4()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 || id[14] != '4' {
		t.Errorf("bad uuid4: %q", id)
	}
	if id != strings.ToUpper(id) {
		t.Errorf("uuid not uppercase: %q", id)
	}
}
