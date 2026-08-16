#!/usr/bin/env python3
"""Validate the lightweight favorites graph using only the Python standard library."""

from __future__ import annotations

import json
import sys
import uuid
from collections import Counter
from pathlib import Path
from typing import Any


ALLOWED_EDGE_TYPES = {
    "Founder_of",
    "Author_of",
    "Host_of",
    "Current_Employee_of",
    "Previous_Employee_of",
    "Is_a_Person",
}


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


def validate(repo_root: Path) -> tuple[list[str], dict[str, object]]:
    errors: list[str] = []
    entities_doc = load_json(repo_root / "graph" / "entities.json", errors)
    edges_doc = load_json(repo_root / "graph" / "edges.json", errors)
    person_edges_doc = load_json(
        repo_root / "graph" / "is_a_person-edges.json", errors
    )
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

    if not isinstance(person_edges_doc, dict) or set(person_edges_doc) != {"edges"}:
        errors.append(
            "is_a_person-edges.json must be an object containing only 'edges'"
        )
        person_edges: list[Any] = []
    elif not isinstance(person_edges_doc["edges"], list):
        errors.append("is_a_person-edges.json edges must be an array")
        person_edges = []
    else:
        person_edges = person_edges_doc["edges"]

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
    person_class_ids = {
        entity["id"].casefold()
        for entity in entities
        if isinstance(entity, dict)
        and isinstance(entity.get("id"), str)
        and isinstance(entity.get("name"), str)
        and entity["name"].casefold() == "person"
    }
    used_ids: set[str] = set()
    edge_keys: list[tuple[str, str, str]] = []
    edge_type_counts: Counter[str] = Counter()
    classified_person_ids: set[str] = set()
    relationship_source_ids: set[str] = set()
    all_edges = [
        *(('edges.json', index, edge) for index, edge in enumerate(edges)),
        *(
            ('is_a_person-edges.json', index, edge)
            for index, edge in enumerate(person_edges)
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
            if filename == "edges.json" and edge_type == "Is_a_Person":
                errors.append(f"{label} belongs in is_a_person-edges.json")
            if filename == "is_a_person-edges.json" and edge_type != "Is_a_Person":
                errors.append(f"{label} belongs in edges.json")
            edge_keys.append((source.casefold(), target.casefold(), edge_type))
            edge_type_counts[edge_type] += 1
            if edge_type == "Is_a_Person":
                if target.casefold() not in person_class_ids:
                    errors.append(f"{label} must target the Person class entity")
                elif source.casefold() in person_class_ids:
                    errors.append(f"{label} cannot classify the Person class itself")
                else:
                    classified_person_ids.add(source.casefold())
            else:
                relationship_source_ids.add(source.casefold())

    duplicate_edges = [key for key, count in Counter(edge_keys).items() if count > 1]
    if duplicate_edges:
        errors.append(f"duplicate edges: {duplicate_edges!r}")

    missing_person_classification = sorted(
        relationship_source_ids - classified_person_ids
    )
    if missing_person_classification:
        errors.append(
            "relationship sources missing Is_a_Person edges: "
            + ", ".join(missing_person_classification)
        )

    isolated_ids = sorted(id_set - used_ids)

    summary: dict[str, object] = {
        "entities": len(entities),
        "edges": len(edges) + len(person_edges),
        "relationship_edges": len(edges),
        "person_edges": len(person_edges),
        "edge_types": dict(sorted(edge_type_counts.items())),
        "isolated_entities": len(isolated_ids),
        "people": len(classified_person_ids),
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
