from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import textwrap
import uuid
from pathlib import Path

import pytest

from pmo_sim import bundle
from pmo_sim.bundle import ENTITY_BY_SLUG, build_bundle, describe_plan, missing_fields, plan_errors

KM2_REPO = Path(os.environ.get("KM2_REPO", "/home/jblair/github/redarchlabs/red-arch-km-2"))
KM2_FILES = (
    "services/api/src/api/schemas/custom_entity.py",
    "services/api/src/api/services/migration/bundle.py",
    "services/api/src/api/services/migration/diff.py",
)

# The contract Crew Studio's km2_tracker reads (plan section 2): slugs, field types, flags, picklists.
CONTRACT = {
    "trk_person": {"name": "text!", "email": "text!u", "role": "text", "active": "boolean", "is_bot": "boolean"},
    "trk_project": {
        "key": "text!u",
        "name": "text",
        "description": "long_text",
        "status": "picklist",
        "start_date": "date",
        "target_date": "date",
    },
    "trk_cycle": {"name": "text", "number": "integer", "starts_on": "date", "ends_on": "date", "project_key": "text"},
    "trk_issue": {
        "identifier": "text!u",
        "number": "integer",
        "project_key": "text!",
        "title": "text",
        "description": "long_text",
        "issue_type": "picklist",
        "status": "picklist",
        "status_category": "picklist!",
        "priority": "picklist",
        "labels": "text",
        "estimate": "integer",
        "start_date": "date",
        "due_date": "date",
        "parent_key": "text",
        "activity_at": "timestamptz",
        "completed_on": "date",
    },
    "trk_comment": {
        "body": "long_text!",
        "issue_key": "text",
        "project_key": "text",
        "author_name": "text",
        "mentions": "text",
        "sim_posted_at": "timestamptz",
    },
    "trk_issue_link": {"kind": "picklist", "from_key": "text", "to_key": "text", "active": "boolean"},
    "trk_event": {
        "event_id": "text!u",
        "issue_key": "text",
        "kind": "text",
        "from_value": "text",
        "to_value": "text",
        "actor_name": "text",
        "sim_day": "integer",
        "sim_at": "timestamptz",
    },
    "trk_sim_state": {"sim_day": "integer", "sim_date": "date", "seed": "integer", "advanced_at": "timestamptz"},
}
PICKLISTS = {
    ("trk_project", "status"): ["planned", "active", "paused", "completed", "canceled"],
    ("trk_issue", "issue_type"): ["Epic", "Story", "Task", "Bug"],
    ("trk_issue", "status"): ["Backlog", "Todo", "In Progress", "In Review", "Done", "Canceled", "Duplicate"],
    ("trk_issue", "status_category"): ["to_do", "in_progress", "done", "canceled"],
    ("trk_issue", "priority"): ["Urgent", "High", "Medium", "Low", "None"],
    ("trk_issue_link", "kind"): ["blocks", "relates", "duplicates"],
}
RELATIONSHIPS = {
    "trk_project": {"lead": ("trk_person", "SET NULL", False)},
    "trk_cycle": {"project": ("trk_project", "SET NULL", False)},
    "trk_issue": {
        "project": ("trk_project", "RESTRICT", False),
        "parent": ("trk_issue", "SET NULL", False),
        "assignee": ("trk_person", "SET NULL", False),
        "creator": ("trk_person", "SET NULL", False),
        "cycle": ("trk_cycle", "SET NULL", False),
    },
    "trk_comment": {"issue": ("trk_issue", "CASCADE", True), "author": ("trk_person", "SET NULL", False)},
    "trk_issue_link": {"from_issue": ("trk_issue", "CASCADE", False), "to_issue": ("trk_issue", "CASCADE", False)},
    "trk_event": {"issue": ("trk_issue", "CASCADE", False)},
    "trk_sim_state": {},
}


def _entities() -> dict[str, dict]:
    return {e["slug"]: e for e in build_bundle()["resources"]["entities"]}


def test_bundle_envelope_matches_km2_v2() -> None:
    b = build_bundle()
    assert b["kind"] == "km2-migration-bundle"
    assert b["format_version"] == 2
    assert list(b["resources"]) == list(bundle.RESOURCE_ORDER)
    assert b["counts"]["entities"] == 8
    assert all(v == [] for k, v in b["resources"].items() if k != "entities")
    uuid.UUID(b["source_org_id"])


def test_bundle_is_deterministic() -> None:
    assert bundle.bundle_bytes() == bundle.bundle_bytes()
    first = _entities()["trk_issue"]
    assert first["id"] == str(uuid.uuid5(bundle.ID_NAMESPACE, "entity:trk_issue"))
    assert first["lineage_id"] == first["id"]


def test_fields_follow_the_contract() -> None:
    ents = _entities()
    assert set(ents) == set(CONTRACT)
    for slug, expected in CONTRACT.items():
        fields = {f["slug"]: f for f in ents[slug]["fields"]}
        assert list(fields) == list(expected), slug
        for name, spec in expected.items():
            f = fields[name]
            assert f["field_type"] == spec.rstrip("!u"), (slug, name)
            assert f["is_required"] == ("!" in spec), (slug, name)
            assert f["is_unique"] == spec.endswith("u"), (slug, name)
            assert f["lineage_id"] == f["id"]
            assert re.match(r"^[a-z][a-z0-9_]*$", f["slug"])
            assert set(f) == {
                "id",
                "lineage_id",
                "name",
                "slug",
                "field_type",
                "picklist_options",
                "is_required",
                "is_unique",
                "default_value",
                "order",
            }
            if f["field_type"] == "picklist":
                assert f["picklist_options"] == PICKLISTS[(slug, name)]
            else:
                assert f["picklist_options"] == []
        assert [f["order"] for f in ents[slug]["fields"]] == list(range(len(expected)))


def test_relationships_follow_the_contract() -> None:
    ents = _entities()
    ids = {e["id"]: s for s, e in ents.items()}
    for slug, expected in RELATIONSHIPS.items():
        rels = {r["slug"]: r for r in ents[slug]["relationships"]}
        assert set(rels) == set(expected), slug
        for name, (target, on_delete, required) in expected.items():
            r = rels[name]
            assert r["cardinality"] == "many_to_one"
            assert ids[r["target_definition_id"]] == target
            assert (r["on_delete"], r["is_required"]) == (on_delete, required)
            assert not (r["is_required"] and r["on_delete"] == "SET NULL")


def test_missing_fields_and_plan_rendering() -> None:
    definition = {"fields": [{"slug": "name"}, {"slug": "email"}]}
    assert missing_fields("trk_person", definition) == ["active", "is_bot", "role"]
    result = {
        "diff": {
            "resources": [
                {"resource_type": "tags", "added": 0},
                {
                    "resource_type": "entities",
                    "added": 1,
                    "changed": 0,
                    "unchanged": 7,
                    "deleted": 0,
                    "objects": [{"status": "added", "natural_key": "trk_event"}],
                },
            ]
        },
        "import_summary": {
            "dry_run": True,
            "resources": {"entities": {"created": 1, "skipped": 7, "failed": 0}},
            "warnings": ["relationship 'issue' on 'trk_event': boom"],
            "errors": ["entity 'x': bad"],
        },
    }
    lines = describe_plan(result)
    assert lines[0] == "entities: 1 to add, 0 changed, 7 unchanged, 0 to delete"
    assert "  added     trk_event" in lines
    assert "import (dry run): created 1, skipped 7" in lines
    assert plan_errors(result) == ["entity 'x': bad", "relationship 'issue' on 'trk_event': boom"]
    failed = {"import_summary": {"resources": {"entities": {"failed": 2}}}}
    assert plan_errors(failed) == ["2 entities failed to import"]


def test_entity_spec_labels() -> None:
    assert ENTITY_BY_SLUG["trk_person"].fields[4].label == "Is bot"
    assert ENTITY_BY_SLUG["trk_issue"].relationships[0].label == "Project"


VALIDATOR = textwrap.dedent(
    """
    import json, sys, uuid
    from api.schemas.custom_entity import EntityDefinitionCreate, EntityFieldCreate, EntityRelationshipCreate
    from api.services.migration.bundle import BUNDLE_KIND, SUPPORTED_BUNDLE_FORMAT_VERSIONS, RESOURCE_ORDER
    from api.services.migration.diff import compute_diff

    bundle = json.load(open(sys.argv[1]))
    assert bundle["kind"] == BUNDLE_KIND and bundle["format_version"] in SUPPORTED_BUNDLE_FORMAT_VERSIONS
    assert set(bundle["resources"]) == set(RESOURCE_ORDER)
    ents = bundle["resources"]["entities"]
    ids = {e["id"] for e in ents}
    for e in ents:
        # exactly what MigrationImporter._import_entities / _field_create build from the bundle
        EntityDefinitionCreate(name=e["name"], slug=e["slug"], description=e.get("description"), fields=[
            EntityFieldCreate(name=f["name"], slug=f["slug"], field_type=f["field_type"],
                              picklist_options=f.get("picklist_options") or [], is_required=f.get("is_required", False),
                              is_unique=f.get("is_unique", False), default_value=f.get("default_value"),
                              order=f.get("order", 0)) for f in e["fields"]])
        for r in e["relationships"]:
            assert r["target_definition_id"] in ids
            EntityRelationshipCreate(name=r["name"], slug=r["slug"], cardinality=r["cardinality"],
                                     target_definition_id=uuid.UUID(r["target_definition_id"]),
                                     on_delete=r.get("on_delete", "SET NULL"), is_required=r.get("is_required", False))
    empty = compute_diff(bundle["resources"], {})
    assert empty.totals["added"] == len(ents), empty.totals
    # A target that already holds the promoted entities (new ids, lineage stamped) reads as unchanged.
    remapped = json.loads(json.dumps(bundle["resources"]))
    for e in remapped["entities"]:
        for item in [e, *e["fields"], *e["relationships"]]:
            item["id"] = str(uuid.uuid4())
    by_lineage = {e["lineage_id"]: e["id"] for e in remapped["entities"]}
    for e in remapped["entities"]:
        for r in e["relationships"]:
            r["target_definition_id"] = by_lineage[r["target_definition_id"]]
    again = compute_diff(bundle["resources"], remapped)
    assert again.totals["unchanged"] == len(ents) and again.totals["added"] == 0, again.totals
    print("ok", len(ents), "entities", sum(len(e["fields"]) for e in ents), "fields",
          sum(len(e["relationships"]) for e in ents), "relationships")
    """
)


def _km2_ref() -> str:
    for ref in ("origin/main", "main", "HEAD"):
        probe = subprocess.run(["git", "-C", str(KM2_REPO), "rev-parse", "--verify", ref], capture_output=True)
        if probe.returncode == 0:
            return ref
    raise RuntimeError("no usable ref")


@pytest.mark.skipif(not (KM2_REPO / ".git").exists(), reason="KM2 repository not available")
def test_bundle_validates_against_km2_models(tmp_path: Path) -> None:
    pytest.importorskip("pydantic")
    ref = _km2_ref()
    for rel in KM2_FILES:
        source = subprocess.run(["git", "-C", str(KM2_REPO), "show", f"{ref}:{rel}"], capture_output=True, check=True)
        dest = tmp_path / rel.removeprefix("services/api/src/")
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_bytes(source.stdout)
    for pkg in ("api", "api/schemas", "api/services", "api/services/migration"):
        (tmp_path / pkg / "__init__.py").touch()
    bundle_file = tmp_path / "bundle.json"
    bundle_file.write_bytes(bundle.bundle_bytes())
    script = tmp_path / "validate.py"
    script.write_text(VALIDATOR)
    proc = subprocess.run(
        [sys.executable, str(script), str(bundle_file)],
        capture_output=True,
        text=True,
        env={**os.environ, "PYTHONPATH": str(tmp_path)},
    )
    assert proc.returncode == 0, proc.stderr
    assert proc.stdout.startswith("ok 8 entities")
    json.loads(bundle_file.read_text())
