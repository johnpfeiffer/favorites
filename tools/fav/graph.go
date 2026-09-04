package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// graphEntity and graphEdge mirror graph/*.json exactly (two fields each).
type graphEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type graphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

type entityFile struct {
	Entities []graphEntity `json:"entities"`
}

type edgeFile struct {
	Edges []graphEdge `json:"edges"`
}

// allowedEdgeTypes is the closed ontology from the maintain-favorites-graph
// skill. Is_a_Person lives only in is_a_person-edges.json; all other types
// live only in edges.json.
var allowedEdgeTypes = map[string]bool{
	"Founder_of": true, "Author_of": true, "Host_of": true,
	"Current_Employee_of": true, "Previous_Employee_of": true,
	"Is_a_Person": true,
}

type graph struct {
	entities    []graphEntity
	edges       []graphEdge
	personEdges []graphEdge
	byName      map[string]string // lowercased name -> id
	edgeSet     map[string]bool   // "src|type|tgt"
	personSet   map[string]bool
	personID    string
}

func loadGraph(repo string) (*graph, error) {
	g := &graph{
		byName:    map[string]string{},
		edgeSet:   map[string]bool{},
		personSet: map[string]bool{},
	}
	read := func(name string, v any) error {
		raw, err := os.ReadFile(filepath.Join(repo, "graph", name))
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, v)
	}
	var ef entityFile
	if err := read("entities.json", &ef); err != nil {
		return nil, fmt.Errorf("entities.json: %w", err)
	}
	var efEdges, efPerson edgeFile
	if err := read("edges.json", &efEdges); err != nil {
		return nil, fmt.Errorf("edges.json: %w", err)
	}
	if err := read("is_a_person-edges.json", &efPerson); err != nil {
		return nil, fmt.Errorf("is_a_person-edges.json: %w", err)
	}
	g.entities = ef.Entities
	g.edges = efEdges.Edges
	g.personEdges = efPerson.Edges
	for _, e := range g.entities {
		g.byName[strings.ToLower(e.Name)] = e.ID
		if e.Name == "Person" {
			g.personID = e.ID
		}
	}
	for _, e := range g.edges {
		g.edgeSet[edgeKey(e)] = true
	}
	for _, e := range g.personEdges {
		g.personSet[edgeKey(e)] = true
	}
	if g.personID == "" {
		return nil, fmt.Errorf("graph has no Person class entity")
	}
	return g, nil
}

func edgeKey(e graphEdge) string {
	return e.Source + "|" + e.Type + "|" + e.Target
}

// newUUID4 returns an uppercase hyphenated UUID4, matching existing entities.
func newUUID4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])), nil
}

// ensureEntity reuses an existing entity case-insensitively or creates it.
func (g *graph) ensureEntity(name string) (id string, created bool, err error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if id, ok := g.byName[key]; ok {
		return id, false, nil
	}
	id, err = newUUID4()
	if err != nil {
		return "", false, err
	}
	g.entities = append(g.entities, graphEntity{ID: id, Name: strings.TrimSpace(name)})
	g.byName[key] = id
	return id, true, nil
}

// edgeTriple is one requested edge: Source Name | Type | Target Name.
type edgeTriple struct {
	Source, Type, Target string
}

type addEdgeOutcome struct {
	Triple        edgeTriple `json:"triple"`
	SourceCreated bool       `json:"sourceCreated"`
	TargetCreated bool       `json:"targetCreated"`
	EdgeAdded     bool       `json:"edgeAdded"`
	Error         string     `json:"error,omitempty"`
}

func cmdGraph(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("graph needs a subcommand: add-edge | bio")
	}
	switch args[0] {
	case "add-edge":
		return cmdGraphAddEdge(args[1:])
	case "bio":
		return cmdGraphBio(args[1:])
	default:
		return fmt.Errorf("unknown graph subcommand %q (want add-edge | bio)", args[0])
	}
}

// parseTriples accepts args in groups of three, or stdin lines split on tabs
// or " | ".
func parseTriples(args []string) ([]edgeTriple, error) {
	if len(args) > 0 && !(len(args) == 1 && args[0] == "-") {
		if len(args)%3 != 0 {
			return nil, fmt.Errorf("add-edge args must be Source Type Target triples (got %d args)", len(args))
		}
		var out []edgeTriple
		for i := 0; i < len(args); i += 3 {
			out = append(out, edgeTriple{args[i], args[i+1], args[i+2]})
		}
		return out, nil
	}
	lines, err := readRawLines(nil)
	if err != nil {
		return nil, err
	}
	var out []edgeTriple
	for _, line := range lines {
		var parts []string
		if strings.Contains(line, "\t") {
			parts = strings.Split(line, "\t")
		} else {
			parts = strings.Split(line, " | ")
		}
		if len(parts) != 3 {
			return nil, fmt.Errorf("bad triple line %q (want Source<TAB>Type<TAB>Target or Source | Type | Target)", line)
		}
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		out = append(out, edgeTriple{parts[0], parts[1], parts[2]})
	}
	return out, nil
}

func cmdGraphAddEdge(args []string) error {
	fs := flag.NewFlagSet("graph add-edge", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root (contains graph/)")
	dryRun := fs.Bool("dry-run", false, "report what would change without writing")
	asJSON := fs.Bool("json", false, "emit JSON instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	triples, err := parseTriples(fs.Args())
	if err != nil {
		return err
	}
	g, err := loadGraph(*repo)
	if err != nil {
		return err
	}

	var outcomes []addEdgeOutcome
	edgesChanged, personChanged := false, false
	failed := false
	for _, t := range triples {
		oc := addEdgeOutcome{Triple: t}
		if !allowedEdgeTypes[t.Type] {
			oc.Error = "unknown edge type " + t.Type
			outcomes = append(outcomes, oc)
			failed = true
			continue
		}
		src, createdSrc, err := g.ensureEntity(t.Source)
		if err != nil {
			oc.Error = err.Error()
			outcomes = append(outcomes, oc)
			failed = true
			continue
		}
		oc.SourceCreated = createdSrc
		var tgt string
		if t.Type == "Is_a_Person" {
			// The target of Is_a_Person is always the single Person class.
			if !strings.EqualFold(strings.TrimSpace(t.Target), "Person") {
				oc.Error = "Is_a_Person target must be the Person class entity"
				outcomes = append(outcomes, oc)
				failed = true
				continue
			}
			tgt = g.personID
		} else {
			var createdTgt bool
			tgt, createdTgt, err = g.ensureEntity(t.Target)
			if err != nil {
				oc.Error = err.Error()
				outcomes = append(outcomes, oc)
				failed = true
				continue
			}
			oc.TargetCreated = createdTgt
		}
		edge := graphEdge{Source: src, Target: tgt, Type: t.Type}
		if t.Type == "Is_a_Person" {
			if !g.personSet[edgeKey(edge)] {
				g.personEdges = append(g.personEdges, edge)
				g.personSet[edgeKey(edge)] = true
				personChanged = true
				oc.EdgeAdded = true
			}
		} else if !g.edgeSet[edgeKey(edge)] {
			g.edges = append(g.edges, edge)
			g.edgeSet[edgeKey(edge)] = true
			edgesChanged = true
			oc.EdgeAdded = true
		}
		outcomes = append(outcomes, oc)
	}

	if !*dryRun && (edgesChanged || personChanged) {
		if err := g.write(*repo, edgesChanged, personChanged); err != nil {
			return err
		}
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		enc.Encode(outcomes)
	} else {
		for _, oc := range outcomes {
			status := "exists"
			if oc.EdgeAdded {
				status = "added"
			}
			created := []string{}
			if oc.SourceCreated {
				created = append(created, "created source")
			}
			if oc.TargetCreated {
				created = append(created, "created target")
			}
			line := fmt.Sprintf("%s %s -[%s]-> %s", status, oc.Triple.Source, oc.Triple.Type, oc.Triple.Target)
			if len(created) > 0 {
				line += " (" + strings.Join(created, ", ") + ")"
			}
			if oc.Error != "" {
				line = "error " + oc.Triple.Source + " -[" + oc.Triple.Type + "]-> " + oc.Triple.Target + ": " + oc.Error
			}
			fmt.Println(line)
		}
		if *dryRun {
			fmt.Println("dry-run: nothing written")
		} else if edgesChanged || personChanged {
			fmt.Println("graph files written; run the graph validator before committing:")
			fmt.Println("  python3 .agents/skills/maintain-favorites-graph/scripts/validate_graph.py .")
		}
	}
	if failed {
		return errReported
	}
	return nil
}

// write persists the graph byte-compatibly with the existing files: two-space
// indent, non-ASCII escaped as \uXXXX (Python ensure_ascii), no trailing
// newline.
func (g *graph) write(repo string, edgesChanged, personChanged bool) error {
	if err := writePyJSON(filepath.Join(repo, "graph", "entities.json"), entityFile{g.entities}); err != nil {
		return err
	}
	if edgesChanged {
		if err := writePyJSON(filepath.Join(repo, "graph", "edges.json"), edgeFile{g.edges}); err != nil {
			return err
		}
	}
	if personChanged {
		if err := writePyJSON(filepath.Join(repo, "graph", "is_a_person-edges.json"), edgeFile{g.personEdges}); err != nil {
			return err
		}
	}
	return nil
}

// writePyJSON encodes v like Python's json.dump(indent=2): two-space indent,
// non-ASCII as \uXXXX escapes, and no trailing newline.
func writePyJSON(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	out := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	return os.WriteFile(path, []byte(escapeNonASCII(string(out))), 0644)
}

// escapeNonASCII replaces non-ASCII runes with \uXXXX escapes (surrogate pairs
// above the BMP), matching Python's ensure_ascii. Non-ASCII bytes only occur
// inside JSON string values, so this transformation is structure-safe.
func escapeNonASCII(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case r <= 0xFFFF:
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			r1, r2 := utf16SurrogatePair(r)
			fmt.Fprintf(&b, "\\u%04x\\u%04x", r1, r2)
		}
	}
	return b.String()
}

func utf16SurrogatePair(r rune) (rune, rune) {
	r -= 0x10000
	return 0xD800 + (r >> 10), 0xDC00 + (r & 0x3FF)
}

// ---------------------------------------------------------------------------
// bio: Wikidata evidence candidates for Founder_of / employment edges
// ---------------------------------------------------------------------------

type wikidataSearchHit struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type bioEmployer struct {
	Name  string `json:"name"`
	QID   string `json:"qid"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
	Edge  string `json:"edge"` // Current_Employee_of | Previous_Employee_of candidate
}

type bioResult struct {
	Input      string              `json:"input"`
	Candidates []wikidataSearchHit `json:"candidates,omitempty"`
	Chosen     *wikidataSearchHit  `json:"chosen,omitempty"`
	Employers  []bioEmployer       `json:"employers,omitempty"`
	Founded    []string            `json:"founded,omitempty"`   // orgs this person founded (reverse P112)
	FoundedBy  []string            `json:"foundedBy,omitempty"` // founders, when input is an org (P112)
	Notes      []string            `json:"notes,omitempty"`
	Error      string              `json:"error,omitempty"`
}

func cmdGraphBio(args []string) error {
	fs := flag.NewFlagSet("graph bio", flag.ContinueOnError)
	qid := fs.String("qid", "", "use this Wikidata QID instead of the first search hit (single input only)")
	delay := fs.Duration("delay", 2*time.Second, "minimum delay between Wikidata API calls (be polite)")
	asJSON := fs.Bool("json", false, "emit a JSON array instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Names are whole arguments/lines ("Peter Adkison"), not "target rest".
	names, err := readRawLines(fs.Args())
	if err != nil {
		return err
	}
	if *qid != "" && len(names) != 1 {
		return fmt.Errorf("--qid requires exactly one input name")
	}
	client := newPoliteClient(
		"fav/"+version+" (favorites repo graph evidence; +https://github.com/johnpfeiffer/favorites)",
		*delay, 15*time.Second, 2, 30*time.Second)

	results := make([]bioResult, 0, len(names))
	for _, name := range names {
		results = append(results, bioLookup(client, name, *qid))
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(results)
	}
	for i, res := range results {
		if i > 0 {
			fmt.Println()
		}
		printBioResult(os.Stdout, res)
	}
	return nil
}

func printBioResult(w io.Writer, res bioResult) {
	fmt.Fprintf(w, "INPUT  %s\n", res.Input)
	if res.Error != "" {
		fmt.Fprintf(w, "ERROR  %s\n", res.Error)
		return
	}
	if res.Chosen != nil {
		fmt.Fprintf(w, "ENTITY %s — %s (%s)\n", res.Chosen.ID, res.Chosen.Label, res.Chosen.Description)
	}
	if len(res.Candidates) > 1 {
		var alts []string
		for _, c := range res.Candidates[1:] {
			alts = append(alts, fmt.Sprintf("%s %s (%s)", c.ID, c.Label, c.Description))
		}
		fmt.Fprintf(w, "ALTS   %s\n", strings.Join(alts, "; "))
	}
	for _, emp := range res.Employers {
		span := emp.Start
		if emp.End != "" {
			span += " – " + emp.End
		} else if emp.Start != "" {
			span += " – present"
		}
		fmt.Fprintf(w, "EMPLOYER %s (%s) %s -> %s candidate\n", emp.Name, emp.QID, span, emp.Edge)
	}
	for _, f := range res.FoundedBy {
		fmt.Fprintf(w, "FOUNDED-BY %s -> Founder_of candidate (founder -> %s)\n", f, res.Input)
	}
	for _, f := range res.Founded {
		fmt.Fprintf(w, "FOUNDED %s -> Founder_of candidate (%s -> org)\n", f, res.Input)
	}
	fmt.Fprintln(w, "NOTE   candidates only: verify Current_Employee_of against a first-party current source before writing")
}

// bioLookup gathers Wikidata evidence for one name.
func bioLookup(client *politeClient, name, qid string) bioResult {
	res := bioResult{Input: name}
	if qid == "" {
		hits, err := wikidataSearch(client, name)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Candidates = hits
		if len(hits) == 0 {
			res.Notes = append(res.Notes, "no Wikidata entry found; gather evidence manually")
			return res
		}
		qid = hits[0].ID
		res.Chosen = &hits[0]
	} else {
		res.Chosen = &wikidataSearchHit{ID: qid, Label: name}
	}

	claims, err := wikidataClaims(client, qid)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	var labelQIDs []string
	// P31 instance-of: Q5 = human.
	isHuman := false
	for _, c := range claims["P31"] {
		if c.Value == "Q5" {
			isHuman = true
		}
	}
	// P108 employer with P580/P582 qualifiers.
	for _, c := range claims["P108"] {
		emp := bioEmployer{QID: c.Value, Start: c.Start, End: c.End}
		if c.End != "" {
			emp.Edge = "Previous_Employee_of"
		} else {
			emp.Edge = "Current_Employee_of"
		}
		res.Employers = append(res.Employers, emp)
		labelQIDs = append(labelQIDs, c.Value)
	}
	// P112 founded by (when the entity is an organization).
	for _, c := range claims["P112"] {
		res.FoundedBy = append(res.FoundedBy, c.Value)
		labelQIDs = append(labelQIDs, c.Value)
	}
	// Reverse founder lookup for humans: orgs with P112 pointing at this QID.
	if isHuman {
		orgs, err := wikidataFoundedByPerson(client, qid)
		if err != nil {
			res.Notes = append(res.Notes, "reverse founder query failed: "+err.Error())
		} else {
			res.Founded = orgs
			labelQIDs = append(labelQIDs, orgs...)
		}
	}

	labels, err := wikidataLabels(client, dedupeStrings(labelQIDs))
	if err != nil {
		res.Notes = append(res.Notes, "label resolution failed: "+err.Error())
	} else {
		for i := range res.Employers {
			if l, ok := labels[res.Employers[i].QID]; ok {
				res.Employers[i].Name = l
			}
		}
		for i, q := range res.FoundedBy {
			if l, ok := labels[q]; ok {
				res.FoundedBy[i] = l + " (" + q + ")"
			}
		}
		for i, q := range res.Founded {
			if l, ok := labels[q]; ok {
				res.Founded[i] = l + " (" + q + ")"
			}
		}
	}
	return res
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func wikidataSearch(client *politeClient, name string) ([]wikidataSearchHit, error) {
	u := "https://www.wikidata.org/w/api.php?action=wbsearchentities&format=json&language=en&limit=3&search=" + url.QueryEscape(name)
	res, err := client.get(u, 1<<20)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Search []wikidataSearchHit `json:"search"`
	}
	if err := json.Unmarshal(res.Body, &parsed); err != nil {
		return nil, err
	}
	return parsed.Search, nil
}

type wikidataClaim struct {
	Value string // target QID
	Start string // P580 qualifier, YYYY-MM-DD or YYYY
	End   string // P582 qualifier
}

// wikidataClaims fetches an entity's claims, reduced to the fields we use.
func wikidataClaims(client *politeClient, qid string) (map[string][]wikidataClaim, error) {
	u := "https://www.wikidata.org/w/api.php?action=wbgetentities&format=json&props=claims&ids=" + url.QueryEscape(qid)
	res, err := client.get(u, 2<<20)
	if err != nil {
		return nil, err
	}
	if res.Status != http.StatusOK {
		return nil, fmt.Errorf("wbgetentities: HTTP %d", res.Status)
	}
	var parsed struct {
		Entities map[string]struct {
			Claims map[string][]struct {
				Mainsnak struct {
					Datavalue *struct {
						Value json.RawMessage `json:"value"`
					} `json:"datavalue"`
				} `json:"mainsnak"`
				// Qualifier values come in several shapes (time, quantity,
				// entity id, string), so keep the raw bytes and extract the
				// time only when it is one.
				Qualifiers map[string][]struct {
					Datavalue *struct {
						Value json.RawMessage `json:"value"`
					} `json:"datavalue"`
				} `json:"qualifiers"`
			} `json:"claims"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(res.Body, &parsed); err != nil {
		return nil, err
	}
	ent, ok := parsed.Entities[qid]
	if !ok {
		return nil, fmt.Errorf("entity %s not in response", qid)
	}
	out := map[string][]wikidataClaim{}
	for prop, claims := range ent.Claims {
		if prop != "P31" && prop != "P108" && prop != "P112" {
			continue
		}
		for _, c := range claims {
			if c.Mainsnak.Datavalue == nil {
				continue
			}
			var idHolder struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(c.Mainsnak.Datavalue.Value, &idHolder); err != nil || idHolder.ID == "" {
				continue
			}
			claim := wikidataClaim{Value: idHolder.ID}
			if qs, ok := c.Qualifiers["P580"]; ok && len(qs) > 0 && qs[0].Datavalue != nil {
				claim.Start = wikidataQualifierTime(qs[0].Datavalue.Value)
			}
			if qs, ok := c.Qualifiers["P582"]; ok && len(qs) > 0 && qs[0].Datavalue != nil {
				claim.End = wikidataQualifierTime(qs[0].Datavalue.Value)
			}
			out[prop] = append(out[prop], claim)
		}
	}
	return out, nil
}

// wikidataQualifierTime extracts a time value from a raw qualifier
// datavalue; non-time qualifiers (quantities, strings) yield "".
func wikidataQualifierTime(raw json.RawMessage) string {
	var holder struct {
		Time string `json:"time"`
	}
	if err := json.Unmarshal(raw, &holder); err != nil {
		return ""
	}
	return wikidataTime(holder.Time)
}

// wikidataTime renders "+2023-01-01T00:00:00Z" as 2023-01-01 and
// "+2023-00-00T00:00:00Z" (year precision) as 2023.
func wikidataTime(t string) string {
	t = strings.TrimPrefix(t, "+")
	parts := strings.SplitN(t, "T", 2)
	if len(parts) == 0 {
		return t
	}
	ymd := strings.Split(parts[0], "-")
	if len(ymd) >= 1 && (len(ymd) < 2 || ymd[1] == "00") {
		return ymd[0]
	}
	return parts[0]
}

// wikidataFoundedByPerson runs the reverse SPARQL query: organizations whose
// founded-by (P112) is this person.
func wikidataFoundedByPerson(client *politeClient, qid string) ([]string, error) {
	query := fmt.Sprintf("SELECT ?org WHERE { ?org wdt:P112 wd:%s } LIMIT 50", qid)
	u := "https://query.wikidata.org/sparql?format=json&query=" + url.QueryEscape(query)
	res, err := client.get(u, 1<<20)
	if err != nil {
		return nil, err
	}
	if res.Status != http.StatusOK {
		return nil, fmt.Errorf("SPARQL: HTTP %d", res.Status)
	}
	var parsed struct {
		Results struct {
			Bindings []struct {
				Org struct {
					Value string `json:"value"`
				} `json:"org"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(res.Body, &parsed); err != nil {
		return nil, err
	}
	var out []string
	for _, b := range parsed.Results.Bindings {
		q := b.Org.Value
		if i := strings.LastIndex(q, "/"); i >= 0 {
			q = q[i+1:]
		}
		out = append(out, q)
	}
	return out, nil
}

func wikidataLabels(client *politeClient, qids []string) (map[string]string, error) {
	if len(qids) == 0 {
		return nil, nil
	}
	sort.Strings(qids)
	u := "https://www.wikidata.org/w/api.php?action=wbgetentities&format=json&props=labels&languages=en&ids=" + strings.Join(qids, "|")
	res, err := client.get(u, 1<<20)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Entities map[string]struct {
			Labels map[string]struct {
				Value string `json:"value"`
			} `json:"labels"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(res.Body, &parsed); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for qid, ent := range parsed.Entities {
		if l, ok := ent.Labels["en"]; ok {
			out[qid] = l.Value
		}
	}
	return out, nil
}
