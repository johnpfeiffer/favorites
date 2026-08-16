---
name: maintain-favorites-graph
description: Create, extend, clean up, review, and validate the lightweight ontology in a favorites repository. Use when extracting canonical entities and relationships from content/*.json, editing graph/entities.json or graph/edges.json, deduplicating graph data, deciding whether a content item deserves an entity, or checking graph integrity.
---

# Maintain Favorites Graph

Treat `content/*.json` records as evidence for the graph, not as graph nodes by default. Keep the graph small enough that every node and edge supports a useful traversal.

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
2. It is an endpoint of a supported, evidence-backed edge.
3. Traversing to or from it adds value beyond opening the original bookmark.

Do not add:

- A bookmark title, URL, summary, episode, article, or video merely because it appears in `content/`.
- A descriptive local title in place of a work's real title.
- Tags, topics, roles, or generic concepts under the current schema.
- Speculative or isolated entities with no supported edge.

Admit a work for `Author_of` only when it has a genuine canonical title and is useful as a shared or notable graph node. For example, keep `Attention Is All You Need`; do not create an entity named `Wired: Eight Google Employees Invented Modern AI - Transformers Paper`. The Wired record is evidence about people, Google, the paper, and their relationships.

Reuse an existing entity and UUID when the canonical name already exists. Generate a UUID4 only for a genuinely new entity. Never regenerate IDs during cleanup or routine maintenance.

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
5. Remove nodes left isolated by cleanup unless another supported edge still uses them.
6. Preserve valid unrelated graph data.
7. Run the adjacent validator from the repository root before handoff:

```bash
python3 .agents/skills/maintain-favorites-graph/scripts/validate_graph.py .
```

## Report the result

Summarize entity and edge counts, counts by edge type, canonicalizations or removals, evidence limitations, and validator status. Call out time-sensitive employment claims separately.

If the user requests a new edge type or richer provenance, first describe the schema migration needed. Do not overload an existing edge type or add undeclared fields ad hoc.

