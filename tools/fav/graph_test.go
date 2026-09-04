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
	write("is_a_person-edges.json", `{
  "edges": []
}`)
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
	if got := len(g.entities); got != 3 {
		t.Errorf("entities=%d, want 3 (second add reused)", got)
	}
	_ = id1
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
