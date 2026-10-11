"""The ``trk_*`` entity schema and the KM2 config-promotion bundle that installs it.

The schema is a contract: Crew Studio's ``km2_tracker`` connection reads these slugs, field names and picklist
values (exact case). The bundle follows KM2's migration bundle format v2 (``services/migration/bundle.py`` and the
exporter's entity shape): every entity, field and relationship carries an ``id`` and a ``lineage_id``. Ids are
uuid5 values derived from the slugs, so the same bundle is produced on every run and a re-promotion matches the
same rows by lineage.
"""

from __future__ import annotations

import json
import uuid
from dataclasses import dataclass, field
from typing import Any

BUNDLE_KIND = "km2-migration-bundle"
BUNDLE_FORMAT_VERSION = 2
# Every resource section KM2 knows, in its dependency order; only ``entities`` is filled.
RESOURCE_ORDER: tuple[str, ...] = (
    "tags",
    "entities",
    "connections",
    "folders",
    "workflows",
    "inbound_endpoints",
    "reports",
    "forms",
    "views",
    "mcp_servers",
    "agents",
    "records",
    "documents",
)
ID_NAMESPACE = uuid.uuid5(uuid.NAMESPACE_URL, "https://crew-studio.local/pmo_sim/trk")
# A fixed pseudo source org, so the bundle bytes are identical on every build.
SOURCE_ORG_ID = str(uuid.uuid5(ID_NAMESPACE, "source-org"))

ISSUE_TYPES = ("Epic", "Story", "Task", "Bug")
STATUSES = ("Backlog", "Todo", "In Progress", "In Review", "Done", "Canceled", "Duplicate")
STATUS_CATEGORIES = ("to_do", "in_progress", "done", "canceled")
PRIORITIES = ("Urgent", "High", "Medium", "Low", "None")
PROJECT_STATUSES = ("planned", "active", "paused", "completed", "canceled")
LINK_KINDS = ("blocks", "relates", "duplicates")

# Status -> status_category; the simulator keeps the two in step on every write.
STATUS_CATEGORY: dict[str, str] = {
    "Backlog": "to_do",
    "Todo": "to_do",
    "In Progress": "in_progress",
    "In Review": "in_progress",
    "Done": "done",
    "Canceled": "canceled",
    "Duplicate": "canceled",
}


@dataclass(frozen=True)
class FieldSpec:
    slug: str
    field_type: str
    name: str = ""
    required: bool = False
    unique: bool = False
    options: tuple[str, ...] = ()

    @property
    def label(self) -> str:
        return self.name or self.slug.replace("_", " ").capitalize()


@dataclass(frozen=True)
class RelSpec:
    slug: str
    target: str
    on_delete: str = "SET NULL"
    required: bool = False
    cardinality: str = "many_to_one"
    name: str = ""

    @property
    def label(self) -> str:
        return self.name or self.slug.replace("_", " ").capitalize()


@dataclass(frozen=True)
class EntitySpec:
    slug: str
    name: str
    description: str
    fields: tuple[FieldSpec, ...]
    relationships: tuple[RelSpec, ...] = field(default_factory=tuple)

    def field_slugs(self) -> set[str]:
        return {f.slug for f in self.fields}


def _f(slug: str, field_type: str = "text", **kw: Any) -> FieldSpec:
    return FieldSpec(slug=slug, field_type=field_type, **kw)


ENTITIES: tuple[EntitySpec, ...] = (
    EntitySpec(
        "trk_person",
        "Tracker person",
        "People who own, create and comment on tracker issues (one of them is the Crew Studio bot).",
        (
            _f("name", required=True),
            _f("email", required=True, unique=True),
            _f("role"),
            _f("active", "boolean"),
            _f("is_bot", "boolean", name="Is bot"),
        ),
    ),
    EntitySpec(
        "trk_project",
        "Tracker project",
        "A project; issues are numbered per project key (ERP-1, ERP-2, ...).",
        (
            _f("key", required=True, unique=True),
            _f("name"),
            _f("description", "long_text"),
            _f("status", "picklist", options=PROJECT_STATUSES),
            _f("start_date", "date"),
            _f("target_date", "date"),
        ),
        (RelSpec("lead", "trk_person"),),
    ),
    EntitySpec(
        "trk_cycle",
        "Tracker cycle",
        "A time-boxed cycle (sprint) of a project.",
        (
            _f("name"),
            _f("number", "integer"),
            _f("starts_on", "date"),
            _f("ends_on", "date"),
            _f("project_key"),
        ),
        (RelSpec("project", "trk_project"),),
    ),
    EntitySpec(
        "trk_issue",
        "Tracker issue",
        "An issue (epic, story, task or bug). status_category is kept in step with status.",
        (
            _f("identifier", required=True, unique=True),
            _f("number", "integer"),
            _f("project_key", required=True),
            _f("title"),
            _f("description", "long_text"),
            _f("issue_type", "picklist", options=ISSUE_TYPES),
            _f("status", "picklist", options=STATUSES),
            _f("status_category", "picklist", required=True, options=STATUS_CATEGORIES),
            _f("priority", "picklist", options=PRIORITIES),
            _f("labels"),
            _f("estimate", "integer"),
            _f("start_date", "date"),
            _f("due_date", "date"),
            _f("parent_key"),
            _f("activity_at", "timestamptz"),
            _f("completed_on", "date"),
        ),
        (
            RelSpec("project", "trk_project", on_delete="RESTRICT"),
            RelSpec("parent", "trk_issue"),
            RelSpec("assignee", "trk_person"),
            RelSpec("creator", "trk_person"),
            RelSpec("cycle", "trk_cycle"),
        ),
    ),
    EntitySpec(
        "trk_comment",
        "Tracker comment",
        "A comment on an issue. sim_posted_at is the simulated time; created_at is the real write time.",
        (
            _f("body", "long_text", required=True),
            _f("issue_key"),
            _f("project_key"),
            _f("author_name"),
            _f("mentions"),
            _f("sim_posted_at", "timestamptz"),
        ),
        (
            RelSpec("issue", "trk_issue", on_delete="CASCADE", required=True),
            RelSpec("author", "trk_person"),
        ),
    ),
    EntitySpec(
        "trk_issue_link",
        "Tracker issue link",
        "A typed link between two issues (from_key blocks to_key, relates, duplicates).",
        (
            _f("kind", "picklist", options=LINK_KINDS),
            _f("from_key"),
            _f("to_key"),
            _f("active", "boolean"),
        ),
        (
            RelSpec("from_issue", "trk_issue", on_delete="CASCADE"),
            RelSpec("to_issue", "trk_issue", on_delete="CASCADE"),
        ),
    ),
    EntitySpec(
        "trk_event",
        "Tracker event",
        "Issue history: one row per change, with a deterministic event_id.",
        (
            _f("event_id", required=True, unique=True),
            _f("issue_key"),
            _f("kind"),
            _f("from_value"),
            _f("to_value"),
            _f("actor_name"),
            _f("sim_day", "integer"),
            _f("sim_at", "timestamptz"),
        ),
        (RelSpec("issue", "trk_issue", on_delete="CASCADE"),),
    ),
    EntitySpec(
        "trk_sim_state",
        "Tracker simulator state",
        "Single record holding the project simulator's clock.",
        (
            _f("sim_day", "integer"),
            _f("sim_date", "date"),
            _f("seed", "integer"),
            _f("advanced_at", "timestamptz"),
        ),
    ),
)

ENTITY_BY_SLUG: dict[str, EntitySpec] = {e.slug: e for e in ENTITIES}
# Children before parents: the order records must be deleted in.
DELETE_ORDER: tuple[str, ...] = (
    "trk_comment",
    "trk_event",
    "trk_issue_link",
    "trk_sim_state",
    "trk_issue",
    "trk_cycle",
    "trk_project",
    "trk_person",
)


def stable_id(*parts: str) -> str:
    return str(uuid.uuid5(ID_NAMESPACE, ":".join(parts)))


def _field_dict(entity: str, order: int, spec: FieldSpec) -> dict[str, Any]:
    fid = stable_id("field", entity, spec.slug)
    return {
        "id": fid,
        "lineage_id": fid,
        "name": spec.label,
        "slug": spec.slug,
        "field_type": spec.field_type,
        "picklist_options": list(spec.options),
        "is_required": spec.required,
        "is_unique": spec.unique,
        "default_value": None,
        "order": order,
    }


def _rel_dict(entity: str, spec: RelSpec) -> dict[str, Any]:
    rid = stable_id("relationship", entity, spec.slug)
    return {
        "id": rid,
        "lineage_id": rid,
        "name": spec.label,
        "slug": spec.slug,
        "cardinality": spec.cardinality,
        "on_delete": spec.on_delete,
        "is_required": spec.required,
        "target_definition_id": stable_id("entity", spec.target),
    }


def entity_dict(spec: EntitySpec) -> dict[str, Any]:
    eid = stable_id("entity", spec.slug)
    return {
        "id": eid,
        "lineage_id": eid,
        "name": spec.name,
        "slug": spec.slug,
        "description": spec.description,
        "is_active": True,
        "fields": [_field_dict(spec.slug, i, f) for i, f in enumerate(spec.fields)],
        "relationships": [_rel_dict(spec.slug, r) for r in spec.relationships],
    }


def build_bundle() -> dict[str, Any]:
    """The full v2 bundle: the ``trk_*`` entities and every other section empty."""
    resources: dict[str, Any] = {name: [] for name in RESOURCE_ORDER}
    resources["entities"] = [entity_dict(e) for e in ENTITIES]
    counts = {name: len(items) for name, items in resources.items()}
    return {
        "kind": BUNDLE_KIND,
        "format_version": BUNDLE_FORMAT_VERSION,
        "source_org_id": SOURCE_ORG_ID,
        "counts": counts,
        "warnings": [],
        "resources": resources,
    }


def bundle_bytes() -> bytes:
    return (json.dumps(build_bundle(), indent=2, ensure_ascii=False) + "\n").encode("utf-8")


def missing_fields(slug: str, definition: dict[str, Any]) -> list[str]:
    """Field slugs the contract expects that a KM2 definition (``GET /api/v1/entities/{slug}``) lacks."""
    have = {f.get("slug") for f in definition.get("fields") or []}
    return sorted(ENTITY_BY_SLUG[slug].field_slugs() - have)


def describe_plan(result: dict[str, Any]) -> list[str]:
    """Human-readable lines for a ``PromotionResult`` (diff + import summary)."""
    lines: list[str] = []
    diff = result.get("diff") or {}
    for rd in diff.get("resources") or []:
        if rd.get("resource_type") != "entities":
            continue
        lines.append(
            "entities: {added} to add, {changed} changed, {unchanged} unchanged, {deleted} to delete".format(
                added=rd.get("added", 0),
                changed=rd.get("changed", 0),
                unchanged=rd.get("unchanged", 0),
                deleted=rd.get("deleted", 0),
            )
        )
        for obj in rd.get("objects") or []:
            lines.append(f"  {obj.get('status', '?'):<9} {obj.get('natural_key') or obj.get('name')}")
    summary = result.get("import_summary") or {}
    outcome = (summary.get("resources") or {}).get("entities")
    if outcome:
        parts = ", ".join(f"{k} {v}" for k, v in outcome.items() if v)
        lines.append(f"import ({'dry run' if summary.get('dry_run') else 'applied'}): {parts or 'nothing'}")
    for warning in summary.get("warnings") or []:
        lines.append(f"warning: {warning}")
    for error in summary.get("errors") or []:
        lines.append(f"error: {error}")
    return lines


def plan_errors(result: dict[str, Any]) -> list[str]:
    summary = result.get("import_summary") or {}
    errors = list(summary.get("errors") or [])
    outcome = (summary.get("resources") or {}).get("entities") or {}
    if outcome.get("failed"):
        errors.append(f"{outcome['failed']} entities failed to import")
    # A relationship that cannot be created only shows up as a warning in KM2's summary.
    errors.extend(w for w in summary.get("warnings") or [] if w.startswith("relationship "))
    return errors
