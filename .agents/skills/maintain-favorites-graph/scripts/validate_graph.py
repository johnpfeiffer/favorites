#!/usr/bin/env python3
"""Validate the lightweight favorites graph using only the Python standard library."""

from __future__ import annotations

import json
import sys
import uuid
from collections import Counter
from pathlib import Path
from typing import Any


RELATIONSHIP_EDGE_TYPES = {
    "Founder_of",
    "Author_of",
    "Host_of",
    "Current_Employee_of",
    "Previous_Employee_of",
}

# Classification edge type -> (segregated file, class entity name).
CLASS_EDGE_TYPES = {
    "Is_a_Person": ("is_a_person-edges.json", "Person"),
    "Is_a_Tag": ("is_a_tag-edges.json", "Tag"),
    "Is_a_Company": ("is_a_company-edges.json", "Company"),
}

ALLOWED_EDGE_TYPES = RELATIONSHIP_EDGE_TYPES | set(CLASS_EDGE_TYPES)


def load_json(path: Path, errors: list[str]) -> Any:
    try:
        with path.open(encoding="utf-8") as handle:
            return json.load(handle)
    except FileNotFoundError:
        errors.append(f"missing file: {path}")
    except json.JSONDecodeError as exc:
        errors.append(f"invalid JSON in {path}: {exc}")
    return None


def is_uuid4(value: object) -> bool:
    if not isinstance(value, str):
        return False
    try:
        parsed = uuid.UUID(value)
    except (ValueError, AttributeError):
        return False
    return parsed.version == 4 and str(parsed).casefold() == value.casefold()


def load_content_tags(repo_root: Path, errors: list[str]) -> set[str]:
    """Every keyword in content/*.jsonld, spelled exactly as stored."""
    files = sorted((repo_root / "content").glob("*.jsonld"))
    if not files:
        errors.append("no content/*.jsonld files found for tag checks")
    tags: set[str] = set()
    for path in files:
        doc = load_json(path, errors)
        if not isinstance(doc, dict):
            continue
        for item in doc.get("itemListElement") or []:
            if isinstance(item, dict):
                tags.update(k for k in item.get("keywords") or [] if isinstance(k, str))
    return tags


def validate(repo_root: Path) -> tuple[list[str], dict[str, object]]:
    errors: list[str] = []
    entities_doc = load_json(repo_root / "graph" / "entities.json", errors)
    edges_doc = load_json(repo_root / "graph" / "edges.json", errors)
    class_edge_docs = {
        edge_type: load_json(repo_root / "graph" / filename, errors)
        for edge_type, (filename, _) in CLASS_EDGE_TYPES.items()
    }
    content_tags = load_content_tags(repo_root, errors)
    if errors:
        return errors, {}

    if not isinstance(entities_doc, dict) or set(entities_doc) != {"entities"}:
        errors.append("entities.json must be an object containing only 'entities'")
        entities: list[Any] = []
    elif not isinstance(entities_doc["entities"], list):
        errors.append("entities must be an array")
        entities = []
    else:
        entities = entities_doc["entities"]

    if not isinstance(edges_doc, dict) or set(edges_doc) != {"edges"}:
        errors.append("edges.json must be an object containing only 'edges'")
        edges: list[Any] = []
    elif not isinstance(edges_doc["edges"], list):
        errors.append("edges must be an array")
        edges = []
    else:
        edges = edges_doc["edges"]

    class_edges: dict[str, list[Any]] = {}
    for edge_type, doc in class_edge_docs.items():
        filename = CLASS_EDGE_TYPES[edge_type][0]
        if not isinstance(doc, dict) or set(doc) != {"edges"}:
            errors.append(f"{filename} must be an object containing only 'edges'")
            class_edges[edge_type] = []
        elif not isinstance(doc["edges"], list):
            errors.append(f"{filename} edges must be an array")
            class_edges[edge_type] = []
        else:
            class_edges[edge_type] = doc["edges"]

    ids: list[str] = []
    normalized_names: list[str] = []
    for index, entity in enumerate(entities):
        label = f"entities[{index}]"
        if not isinstance(entity, dict) or set(entity) != {"id", "name"}:
            errors.append(f"{label} must contain exactly id and name")
            continue
        entity_id = entity["id"]
        name = entity["name"]
        if not is_uuid4(entity_id):
            errors.append(f"{label}.id is not a canonical UUID4: {entity_id!r}")
        else:
            ids.append(entity_id.casefold())
        if not isinstance(name, str) or not name.strip() or name != name.strip():
            errors.append(f"{label}.name must be a non-empty trimmed string")
        else:
            normalized_names.append(" ".join(name.casefold().split()))

    duplicate_ids = sorted(value for value, count in Counter(ids).items() if count > 1)
    duplicate_names = sorted(
        value for value, count in Counter(normalized_names).items() if count > 1
    )
    if duplicate_ids:
        errors.append(f"duplicate entity IDs: {', '.join(duplicate_ids)}")
    if duplicate_names:
        errors.append(f"duplicate entity names: {', '.join(duplicate_names)}")

    id_set = set(ids)
    names_by_id = {
        entity["id"].casefold(): entity["name"]
        for entity in entities
        if isinstance(entity, dict)
        and isinstance(entity.get("id"), str)
        and isinstance(entity.get("name"), str)
    }
    class_ids = {
        edge_type: {
            entity_id
            for entity_id, name in names_by_id.items()
            if name.casefold() == class_name.casefold()
        }
        for edge_type, (_, class_name) in CLASS_EDGE_TYPES.items()
    }
    all_class_ids = set().union(*class_ids.values())
    used_ids: set[str] = set()
    edge_keys: list[tuple[str, str, str]] = []
    edge_type_counts: Counter[str] = Counter()
    classified_ids: dict[str, set[str]] = {t: set() for t in CLASS_EDGE_TYPES}
    relationship_source_ids: set[str] = set()
    all_edges = [
        *(('edges.json', index, edge) for index, edge in enumerate(edges)),
        *(
            (CLASS_EDGE_TYPES[edge_type][0], index, edge)
            for edge_type, typed_edges in class_edges.items()
            for index, edge in enumerate(typed_edges)
        ),
    ]
    for filename, index, edge in all_edges:
        label = f"{filename} edges[{index}]"
        if not isinstance(edge, dict) or set(edge) != {"source", "target", "type"}:
            errors.append(f"{label} must contain exactly source, target, and type")
            continue
        source = edge["source"]
        target = edge["target"]
        edge_type = edge["type"]
        if not isinstance(source, str) or source.casefold() not in id_set:
            errors.append(f"{label}.source references a missing entity: {source!r}")
        else:
            used_ids.add(source.casefold())
        if not isinstance(target, str) or target.casefold() not in id_set:
            errors.append(f"{label}.target references a missing entity: {target!r}")
        else:
            used_ids.add(target.casefold())
        if source == target:
            errors.append(f"{label} is a self-edge")
        if edge_type not in ALLOWED_EDGE_TYPES:
            errors.append(f"{label}.type is unsupported: {edge_type!r}")
        elif isinstance(source, str) and isinstance(target, str):
            expected_file = CLASS_EDGE_TYPES.get(edge_type, ("edges.json",))[0]
            if filename != expected_file:
                errors.append(f"{label} belongs in {expected_file}")
            edge_keys.append((source.casefold(), target.casefold(), edge_type))
            edge_type_counts[edge_type] += 1
            if edge_type in CLASS_EDGE_TYPES:
                class_name = CLASS_EDGE_TYPES[edge_type][1]
                if target.casefold() not in class_ids[edge_type]:
                    errors.append(f"{label} must target the {class_name} class entity")
                elif source.casefold() in all_class_ids:
                    errors.append(f"{label} cannot classify an ontology class entity")
                else:
                    classified_ids[edge_type].add(source.casefold())
            else:
                relationship_source_ids.add(source.casefold())

    duplicate_edges = [key for key, count in Counter(edge_keys).items() if count > 1]
    if duplicate_edges:
        errors.append(f"duplicate edges: {duplicate_edges!r}")

    missing_person_classification = sorted(
        relationship_source_ids - classified_ids["Is_a_Person"]
    )
    if missing_person_classification:
        errors.append(
            "relationship sources missing Is_a_Person edges: "
            + ", ".join(missing_person_classification)
        )

    person_and_company = sorted(
        names_by_id[i]
        for i in classified_ids["Is_a_Person"] & classified_ids["Is_a_Company"]
    )
    if person_and_company:
        errors.append(
            "entities classified as both Person and Company: "
            + ", ".join(person_and_company)
        )

    # Is_a_Tag is exact: the entity name is the tag string the content uses,
    # and every entity whose name is a content tag carries the edge.
    tagged_names = {names_by_id[i] for i in classified_ids["Is_a_Tag"]}
    not_in_content = sorted(tagged_names - content_tags)
    if not_in_content:
        errors.append(
            "Is_a_Tag entities with no exactly matching content tag: "
            + ", ".join(not_in_content)
        )
    missing_tag_classification = sorted(
        name
        for entity_id, name in names_by_id.items()
        if name in content_tags
        and entity_id not in all_class_ids
        and name not in tagged_names
    )
    if missing_tag_classification:
        errors.append(
            "entities named exactly like a content tag but missing Is_a_Tag: "
            + ", ".join(missing_tag_classification)
        )

    isolated_ids = sorted(id_set - used_ids)
    class_edge_count = sum(len(typed) for typed in class_edges.values())

    summary: dict[str, object] = {
        "entities": len(entities),
        "edges": len(edges) + class_edge_count,
        "relationship_edges": len(edges),
        "person_edges": len(class_edges["Is_a_Person"]),
        "tag_edges": len(class_edges["Is_a_Tag"]),
        "company_edges": len(class_edges["Is_a_Company"]),
        "edge_types": dict(sorted(edge_type_counts.items())),
        "isolated_entities": len(isolated_ids),
        "people": len(classified_ids["Is_a_Person"]),
        "tags": len(classified_ids["Is_a_Tag"]),
        "companies": len(classified_ids["Is_a_Company"]),
        "content_tags": len(content_tags),
    }
    return errors, summary


def main() -> int:
    repo_root = Path(sys.argv[1] if len(sys.argv) > 1 else ".").resolve()
    errors, summary = validate(repo_root)
    if errors:
        print("Graph validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    print(json.dumps(summary, indent=2, sort_keys=True))
    print("Graph validation passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
