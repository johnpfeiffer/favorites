---
name: maintain-favorites-graph
description: Create, extend, clean up, review, and validate the lightweight ontology in a favorites repository. Use when extracting canonical entities and relationships from content/*.json, discovering subject companies from content titles, editing graph/entities.json or graph/edges.json, deduplicating graph data, deciding whether a content item deserves an entity, or checking graph integrity.
---

# Maintain Favorites Graph

Treat `content/*.json` records as evidence for the graph, not as graph nodes by default. Admit canonical subject companies for discovery even before they have edges, while keeping other nodes focused on useful traversals.

## Follow the schema

Store entities in `graph/entities.json`:

```json
{
  "entities": [
    {
      "id": "UUID4",
      "name": "Canonical name"
    }
  ]
}
```

Store directed relationship edges in `graph/edges.json`:

```json
{
  "edges": [
    {
      "source": "SOURCE_ENTITY_UUID",
      "target": "TARGET_ENTITY_UUID",
      "type": "Founder_of"
    }
  ]
}
```

Store `Is_a_Person` classification edges separately in `graph/is_a_person-edges.json`, using the same top-level `edges` array and edge object shape. Never place `Is_a_Person` in `edges.json`, and never place another edge type in `is_a_person-edges.json`.

Allow only these edge types unless the user explicitly expands the ontology:

- `Founder_of`: person → organization
- `Author_of`: person → canonical named work
- `Host_of`: person → show or podcast
- `Current_Employee_of`: person → organization
- `Previous_Employee_of`: person → organization
- `Is_a_Person`: named human person → the single `Person` class entity

## Admit entities conservatively

Add an entity only when all of these are true:

1. It is a canonical, durable named thing: a person, organization, show, podcast, notable named work, or an explicitly declared ontology class.
2. It is an endpoint of a supported, evidence-backed edge, or it is a company explicitly named as the subject of a curated content title.
3. Traversing to or from it adds value beyond opening the original bookmark.

Do not add:

- A bookmark title, URL, summary, episode, article, or video merely because it appears in `content/`.
- A descriptive local title in place of a work's real title.
- Tags, topics, roles, or generic concepts that have not been explicitly declared as ontology classes.
- Speculative or isolated entities, except subject companies admitted through the title-extraction workflow.

Admit a work for `Author_of` only when it has a genuine canonical title and is useful as a shared or notable graph node. The work must be a durable standalone publication: a book (e.g. `Genentech: The Beginnings of Biotech`), a paper (keep `Attention Is All You Need`), or a recorded lecture. Articles, blog posts, essays, and rants never qualify, however famous (do not create `Choose Boring Technology` or `Stevey's Google Platforms Rant`); those records stay content leaves, so keep their authors connected through employment edges instead. Do not create an entity named `Wired: Eight Google Employees Invented Modern AI - Transformers Paper`. The Wired record is evidence about people, Google, the paper, and their relationships. A recorded lecture or talk with a genuine canonical title (e.g. Grace Hopper's `Future Possibilities: Data, Hardware, Software, and People`) can qualify as a work, with the speaker standing in as author; flag that stretch in the report so the user can veto it. An interviewee or talk speaker is never an `Author_of` the interview article or the talk's write-up.

Reuse an existing entity and UUID when the canonical name already exists. Generate a UUID4 only for a genuinely new entity. Never regenerate IDs during cleanup or routine maintenance.

## Classify people

Maintain exactly one entity named `Person`. Add one `Is_a_Person` edge in `graph/is_a_person-edges.json` from each confidently identified named human to that class entity. Never classify the `Person` class itself, an organization, a show, or a work as a person.

When a name is ambiguous, do not add the classification edge. Ask the user to confirm it and list the relevant evidence. When adding a person as the source of any other supported edge, also add the corresponding `Is_a_Person` edge.

## Extract companies from titles

1. Scan the requested content titles and tags for organizations that are subjects of the saved item.
2. Normalize spelling and branding to a canonical company name, then reuse an existing entity when present.
3. Admit a directly named subject company without an edge when the current edge vocabulary cannot yet express its relationship.
4. Exclude publishers, podcast names, show prefixes, URL hosts, products, technologies, locations, and generic business terms unless they are themselves the subject being curated.
5. Use a URL only to disambiguate a title; do not turn its hostname into an entity.
6. Avoid duplicate entities for former names or rebrands unless the historical identity is useful independently under the user's requested scope.

## Extract evidence carefully

1. Read the selected content record and its existing graph neighborhood.
2. Extract only relationships stated explicitly or supported by the linked source.
3. Resolve the linked page when needed to identify canonical names, authors, or named works. For people and organizations, `tools/fav/fav graph bio <name>` gathers Wikidata candidates (employers with start/end qualifiers → `Current_`/`Previous_Employee_of` candidates, founded-by in both directions). It prints evidence only; every candidate still goes through the verification rules here, and `Current_Employee_of` always needs a first-party current source.
4. Treat vague associations such as `with Company`, a tag, or a company mentioned in a topic as insufficient evidence of employment.
5. Verify `Current_Employee_of` against a reliable current source because it is time-sensitive. Remove or change stale current-employment edges when evidence shows they are no longer current.
6. Prefer omission over guessing. Report useful candidate relationships that need confirmation instead of silently adding them.

Keep content records in `content/`; do not copy their URLs, tags, publication dates, or descriptive titles into entity objects under the current two-field entity schema.

## Maintain the graph

1. Inspect `entities.json`, `edges.json`, and `is_a_person-edges.json` before editing.
2. Match candidates against existing names case-insensitively and check common punctuation or branding variants.
3. Preserve existing UUIDs and edge direction.
4. Write with `tools/fav/fav graph add-edge` (batch triples via args or stdin; `--dry-run` first): it reuses entities case-insensitively, mints uppercase UUID4s only for genuinely new entities, checks edge types against the closed ontology, routes `Is_a_Person` to its segregated file, and writes byte-compatibly with the Python format so diffs stay append-only. Manual JSON edits remain the fallback for what the writer doesn't cover (e.g. removals during cleanup). Add or update the smallest evidence-backed set of entities and edges.
5. Remove nodes left isolated by cleanup unless they are explicitly admitted subject companies.
6. Preserve valid unrelated graph data.
7. Run the adjacent validator from the repository root before handoff:

```bash
python3 .agents/skills/maintain-favorites-graph/scripts/validate_graph.py .
```

8. Spot-check the tooling at random intervals (about one batch in ten): after a `fav graph add-edge` write, diff the graph files by eye — the change must be append-only, byte-compatible, and exactly the intended triples — and verify one `fav graph bio` candidate directly against Wikidata. Deterministic tools fail consistently, so silent drift looks authoritative until audited; report disagreements.

## Report the result

Summarize entity and edge counts, standalone company counts, counts by edge type, canonicalizations or removals, evidence limitations, and validator status. Call out time-sensitive employment claims separately.

If the user requests a new edge type or richer provenance, first describe the schema migration needed. Do not overload an existing edge type or add undeclared fields ad hoc.
