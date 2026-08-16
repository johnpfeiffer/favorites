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

Store directed edges in `graph/edges.json`:

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

Allow only these edge types unless the user explicitly expands the ontology:

- `Founder_of`: person → organization
- `Author_of`: person → canonical named work
- `Host_of`: person → show or podcast
- `Current_Employee_of`: person → organization
- `Previous_Employee_of`: person → organization

## Admit entities conservatively

Add an entity only when all of these are true:

1. It is a canonical, durable named thing: a person, organization, show, podcast, or notable named work.
2. It is an endpoint of a supported, evidence-backed edge, or it is a company explicitly named as the subject of a curated content title.
3. Traversing to or from it adds value beyond opening the original bookmark.

Do not add:

- A bookmark title, URL, summary, episode, article, or video merely because it appears in `content/`.
- A descriptive local title in place of a work's real title.
- Tags, topics, roles, or generic concepts under the current schema.
- Speculative or isolated entities, except subject companies admitted through the title-extraction workflow.

Admit a work for `Author_of` only when it has a genuine canonical title and is useful as a shared or notable graph node. For example, keep `Attention Is All You Need`; do not create an entity named `Wired: Eight Google Employees Invented Modern AI - Transformers Paper`. The Wired record is evidence about people, Google, the paper, and their relationships. A recorded lecture or talk with a genuine canonical title (e.g. Grace Hopper's `Future Possibilities: Data, Hardware, Software, and People`) can qualify as a work, with the speaker standing in as author; flag that stretch in the report so the user can veto it. An interviewee or talk speaker is never an `Author_of` the interview article or the talk's write-up.

Reuse an existing entity and UUID when the canonical name already exists. Generate a UUID4 only for a genuinely new entity. Never regenerate IDs during cleanup or routine maintenance.

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
3. Resolve the linked page when needed to identify canonical names, authors, or named works.
4. Treat vague associations such as `with Company`, a tag, or a company mentioned in a topic as insufficient evidence of employment.
5. Verify `Current_Employee_of` against a reliable current source because it is time-sensitive. Remove or change stale current-employment edges when evidence shows they are no longer current.
6. Prefer omission over guessing. Report useful candidate relationships that need confirmation instead of silently adding them.

Keep content records in `content/`; do not copy their URLs, tags, publication dates, or descriptive titles into entity objects under the current two-field entity schema.

## Maintain the graph

1. Inspect both graph files before editing.
2. Match candidates against existing names case-insensitively and check common punctuation or branding variants.
3. Preserve existing UUIDs and edge direction.
4. Add or update the smallest evidence-backed set of entities and edges.
5. Remove nodes left isolated by cleanup unless they are explicitly admitted subject companies.
6. Preserve valid unrelated graph data.
7. Run the adjacent validator from the repository root before handoff:

```bash
python3 .agents/skills/maintain-favorites-graph/scripts/validate_graph.py .
```

## Report the result

Summarize entity and edge counts, standalone company counts, counts by edge type, canonicalizations or removals, evidence limitations, and validator status. Call out time-sensitive employment claims separately.

If the user requests a new edge type or richer provenance, first describe the schema migration needed. Do not overload an existing edge type or add undeclared fields ad hoc.
