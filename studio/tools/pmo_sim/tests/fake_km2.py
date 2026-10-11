"""In-process fake of the KM2 v1 API surface the simulator uses (http.server on a random local port).

Implements, closely enough to catch shape mistakes:
- ``POST /api/v1/config/promotions`` (multipart ``file``; ``dry_run``/``strategy``/``apply_deletes``): validates
  the bundle like KM2's importer (kind, format_version, slugs, field types, picklist options, relationship rules)
  and creates entity definitions on apply; returns a ``PromotionResult``-shaped body.
- ``GET /api/v1/entities`` and ``GET /api/v1/entities/{slug}``.
- Records CRUD with KM2's validation: unknown fields, picklist membership, required fields, unique fields (a
  violation is a 500, as in KM2 2.6.1), relationship targets, on_delete (CASCADE / SET NULL / RESTRICT), and
  ``filter=field:op:value``, ``order_by``/``order_dir``, keyset ``cursor`` pagination with ``limit`` <= 200.
- Bearer ``km2_`` key auth with per-key scopes, and injectable failures (e.g. one 429 with Retry-After).

Run standalone for a manual end-to-end check: ``python -m pmo_sim.tests.fake_km2 --port 8765``.
"""

from __future__ import annotations

import argparse
import base64
import contextlib
import email.parser
import email.policy
import json
import re
import threading
import uuid
from datetime import UTC, date, datetime
from decimal import Decimal
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any
from urllib.parse import parse_qs, unquote, urlparse

SLUG_RE = re.compile(r"^[a-z][a-z0-9_]*$")
FIELD_TYPES = {
    "text",
    "long_text",
    "integer",
    "bigint",
    "numeric",
    "boolean",
    "date",
    "timestamptz",
    "uuid",
    "json",
    "picklist",
}
CARDINALITIES = {"one_to_one", "one_to_many", "many_to_one", "many_to_many"}
ON_DELETE = {"CASCADE", "SET NULL", "RESTRICT"}
OPS = {"eq", "ne", "gt", "gte", "lt", "lte", "in", "contains", "isnull"}
ALL_SCOPES = {"config:read", "config:write", "entities:read", "records:read", "records:write"}
BASE_COLUMNS = ("id", "created_at", "updated_at")


class ApiError(Exception):
    def __init__(self, status: int, detail: Any, headers: dict[str, str] | None = None) -> None:
        super().__init__(str(detail))
        self.status = status
        self.detail = detail
        self.headers = headers or {}


def _now() -> str:
    return datetime.now(UTC).isoformat()


class Store:
    """Entity catalog + records, all in memory, guarded by one lock."""

    def __init__(self) -> None:
        self.lock = threading.RLock()
        self.entities: dict[str, dict[str, Any]] = {}  # slug -> definition (with lineage + relationships)
        self.records: dict[str, dict[str, dict[str, Any]]] = {}
        self.keys: dict[str, set[str]] = {}
        self.failures: list[tuple[str | None, int, dict[str, str]]] = []  # (method filter, status, headers)
        self.fail_after_writes: int | None = None  # raise 503 on every write once this many writes happened
        self.request_log: list[tuple[str, str]] = []
        self.writes = 0  # successful record writes (POST/PATCH/DELETE on /records)
        self.promotions: list[dict[str, Any]] = []
        self._seq = 0

    # -------------------------------------------------------------- catalog
    def entity(self, slug: str) -> dict[str, Any]:
        ent = self.entities.get(slug)
        if ent is None:
            raise ApiError(404, "entity not found")
        return ent

    def field_map(self, ent: dict[str, Any]) -> dict[str, dict[str, Any]]:
        return {f["slug"]: f for f in ent["fields"]}

    def rel_map(self, ent: dict[str, Any]) -> dict[str, dict[str, Any]]:
        return {r["slug"]: r for r in ent["relationships"] if r["cardinality"] != "many_to_many"}

    def install_bundle(self, bundle: dict[str, Any], dry_run: bool) -> dict[str, Any]:
        if bundle.get("kind") != "km2-migration-bundle":
            raise ApiError(400, "file is not a KM2 migration bundle")
        if bundle.get("format_version") not in (1, 2):
            raise ApiError(400, f"unsupported bundle format_version {bundle.get('format_version')!r}")
        entities = (bundle.get("resources") or {}).get("entities") or []
        errors: list[str] = []
        warnings: list[str] = []
        outcome = {"created": 0, "overwritten": 0, "renamed": 0, "skipped": 0, "failed": 0}
        objects: list[dict[str, Any]] = []
        added = unchanged = 0
        id_map: dict[str, str] = {}
        staged: dict[str, dict[str, Any]] = {}
        for ent in entities:
            slug = ent["slug"]
            existing = self.entities.get(slug)
            if existing is not None:
                outcome["skipped"] += 1
                unchanged += 1
                id_map[ent["id"]] = slug
                continue
            problem = self._validate_entity(ent)
            if problem:
                outcome["failed"] += 1
                errors.append(f"entity {slug!r}: {problem}")
                continue
            added += 1
            objects.append(
                {
                    "resource_type": "entities",
                    "status": "added",
                    "lineage_id": ent.get("lineage_id"),
                    "natural_key": slug,
                    "name": ent["name"],
                }
            )
            outcome["created"] += 1
            id_map[ent["id"]] = slug
            staged[slug] = {
                "id": str(uuid.uuid4()),
                "lineage_id": ent.get("lineage_id"),
                "name": ent["name"],
                "slug": slug,
                "description": ent.get("description"),
                "is_active": True,
                "write_access": "member",
                "fields": [dict(f, id=str(uuid.uuid4()), read_access="member") for f in ent["fields"]],
                "relationships": [],
            }
        for ent in entities:
            target_ent = staged.get(ent["slug"])
            if target_ent is None:
                continue
            for rel in ent.get("relationships") or []:
                target_slug = id_map.get(rel["target_definition_id"])
                if target_slug is None:
                    warnings.append(
                        f"relationship {rel['slug']!r} on {ent['slug']!r}: target entity not in bundle; skipped"
                    )
                    continue
                problem = self._validate_rel(rel)
                if problem:
                    warnings.append(f"relationship {rel['slug']!r} on {ent['slug']!r}: {problem}")
                    continue
                target_ent["relationships"].append(dict(rel, id=str(uuid.uuid4()), target_slug=target_slug))
        if not dry_run:
            self.entities.update(staged)
            for slug in staged:
                self.records.setdefault(slug, {})
        diff = {
            "resources": [
                {
                    "resource_type": "entities",
                    "added": added,
                    "changed": 0,
                    "unchanged": unchanged,
                    "deleted": 0,
                    "count_only": False,
                    "objects": objects,
                }
            ],
            "delete_order": [],
            "has_deletes": False,
            "totals": {"added": added, "changed": 0, "unchanged": unchanged, "deleted": 0},
        }
        summary = {
            "strategy": "skip",
            "dry_run": dry_run,
            "resources": {"entities": outcome},
            "warnings": warnings,
            "errors": errors,
            "generated_secrets": [],
        }
        result = {
            "diff": diff,
            "import_summary": summary,
            "deleted": {},
            "reverse_snapshot": None if dry_run else {},
            "blockers": [],
            "dry_run": dry_run,
        }
        self.promotions.append(result)
        return result

    @staticmethod
    def _validate_entity(ent: dict[str, Any]) -> str | None:
        if not SLUG_RE.match(ent.get("slug") or "") or len(ent["slug"]) > 63:
            return "invalid slug"
        if not 1 <= len(ent.get("name") or "") <= 100:
            return "invalid name"
        slugs = [f["slug"] for f in ent.get("fields") or []]
        if len(slugs) != len(set(slugs)):
            return "duplicate field slugs within an entity"
        for f in ent.get("fields") or []:
            if not SLUG_RE.match(f["slug"]) or f["field_type"] not in FIELD_TYPES:
                return f"invalid field {f['slug']!r}"
            if f["field_type"] == "picklist" and not f.get("picklist_options"):
                return "picklist fields must define at least one option"
            if f["field_type"] != "picklist" and f.get("picklist_options"):
                return "picklist_options is only valid for picklist fields"
        return None

    @staticmethod
    def _validate_rel(rel: dict[str, Any]) -> str | None:
        if not SLUG_RE.match(rel.get("slug") or ""):
            return "invalid slug"
        if rel.get("cardinality") not in CARDINALITIES or rel.get("on_delete", "SET NULL") not in ON_DELETE:
            return "invalid cardinality/on_delete"
        if rel.get("is_required") and rel.get("on_delete", "SET NULL") == "SET NULL":
            return "a required relationship cannot use on_delete=SET NULL"
        return None

    # -------------------------------------------------------------- records
    def _coerce(self, f: dict[str, Any], value: Any) -> Any:
        if value is None:
            return None
        ftype = f["field_type"]
        try:
            if ftype in ("integer", "bigint"):
                if isinstance(value, bool):
                    raise ValueError
                return int(value) if isinstance(value, int) else int(str(value).strip())
            if ftype == "numeric":
                return float(Decimal(str(value)))
            if ftype == "boolean":
                if isinstance(value, bool):
                    return value
                return str(value).strip().lower() in ("true", "t", "1", "yes", "y", "on")
            if ftype == "date":
                text = str(value)
                return (datetime.fromisoformat(text).date() if "T" in text else date.fromisoformat(text)).isoformat()
            if ftype == "timestamptz":
                dt = datetime.fromisoformat(str(value))
                return (dt if dt.tzinfo else dt.replace(tzinfo=UTC)).isoformat()
        except (ValueError, ArithmeticError) as exc:
            raise ApiError(400, f"{f['slug']!r} must be a valid {ftype}") from exc
        return value

    def _to_row(self, ent: dict[str, Any], payload: dict[str, Any], *, create: bool) -> dict[str, Any]:
        fields, rels = self.field_map(ent), self.rel_map(ent)
        unknown = set(payload) - set(fields) - set(rels)
        if unknown:
            raise ApiError(400, f"unknown fields: {sorted(unknown)}")
        row: dict[str, Any] = {}
        for slug, value in payload.items():
            f, r = fields.get(slug), rels.get(slug)
            if value is None and ((f and f["is_required"]) or (r and r["is_required"])):
                raise ApiError(400, f"{slug!r} is required and cannot be null")
            if f is not None:
                if f["field_type"] == "picklist" and value is not None and value not in f["picklist_options"]:
                    raise ApiError(400, f"{slug!r} must be one of {f['picklist_options']}")
                row[slug] = self._coerce(f, value)
            else:
                assert r is not None
                if value is not None:
                    try:
                        uuid.UUID(str(value))
                    except ValueError as exc:
                        raise ApiError(400, f"{slug!r} must be a record id (uuid)") from exc
                    if str(value) not in self.records.get(r["target_slug"], {}):
                        raise ApiError(400, f"{slug!r} references a record not in this org")
                row[slug] = None if value is None else str(value)
        if create:
            for f in ent["fields"]:
                if f["is_required"] and row.get(f["slug"]) is None:
                    raise ApiError(400, f"{f['slug']!r} is required")
            for r in rels.values():
                if r["is_required"] and row.get(r["slug"]) is None:
                    raise ApiError(400, f"{r['slug']!r} is required")
        return row

    def _check_unique(self, ent: dict[str, Any], row: dict[str, Any], record_id: str | None) -> None:
        for f in ent["fields"]:
            if not f["is_unique"] or row.get(f["slug"]) is None:
                continue
            for rid, rec in self.records[ent["slug"]].items():
                if rid != record_id and rec.get(f["slug"]) == row[f["slug"]]:
                    # KM2 2.6.1 does not catch the IntegrityError: the client sees a bare 500.
                    raise ApiError(500, "Internal Server Error")

    def create(self, slug: str, payload: dict[str, Any]) -> dict[str, Any]:
        ent = self.entity(slug)
        row = self._to_row(ent, payload, create=True)
        self._check_unique(ent, row, None)
        rid = str(uuid.uuid4())
        now = _now()
        rec = {"id": rid, "created_at": now, "updated_at": now}
        rec.update({f["slug"]: None for f in ent["fields"]})
        rec.update({r: None for r in self.rel_map(ent)})
        rec.update(row)
        self._seq += 1
        rec["_seq"] = self._seq
        self.records[slug][rid] = rec
        return self.public(rec)

    def update(self, slug: str, rid: str, payload: dict[str, Any]) -> dict[str, Any]:
        ent = self.entity(slug)
        rec = self.records[slug].get(rid)
        if rec is None:
            raise ApiError(404, "record not found")
        row = self._to_row(ent, payload, create=False)
        self._check_unique(ent, row, rid)
        rec.update(row)
        rec["updated_at"] = _now()
        return self.public(rec)

    def delete(self, slug: str, rid: str) -> None:
        self.entity(slug)
        if rid not in self.records[slug]:
            raise ApiError(404, "record not found")
        for other_slug, other in self.entities.items():
            for rel in other["relationships"]:
                if rel.get("target_slug") != slug or rel["cardinality"] == "many_to_many":
                    continue
                refs = [r for r in self.records.get(other_slug, {}).values() if r.get(rel["slug"]) == rid]
                if refs and rel["on_delete"] == "RESTRICT":
                    raise ApiError(500, "Internal Server Error")  # FK violation surfaces as an unhandled 500
        for other_slug, other in self.entities.items():
            for rel in other["relationships"]:
                if rel.get("target_slug") != slug or rel["cardinality"] == "many_to_many":
                    continue
                for ref in list(self.records.get(other_slug, {}).values()):
                    if ref.get(rel["slug"]) != rid:
                        continue
                    if rel["on_delete"] == "CASCADE":
                        if ref["id"] in self.records[other_slug]:
                            self.delete(other_slug, ref["id"])
                    else:
                        ref[rel["slug"]] = None
        self.records[slug].pop(rid, None)

    @staticmethod
    def public(rec: dict[str, Any]) -> dict[str, Any]:
        return {k: v for k, v in rec.items() if not k.startswith("_")}

    def _typed(self, ent: dict[str, Any], slug: str, value: Any) -> Any:
        if value is None:
            return None
        if slug in ("created_at", "updated_at"):
            return datetime.fromisoformat(str(value))
        f = self.field_map(ent).get(slug)
        if f is None:
            return str(value)
        if f["field_type"] in ("integer", "bigint", "numeric"):
            return float(value)
        if f["field_type"] == "timestamptz":
            dt = datetime.fromisoformat(str(value))
            return dt if dt.tzinfo else dt.replace(tzinfo=UTC)
        if f["field_type"] == "boolean":
            return value if isinstance(value, bool) else str(value).lower() in ("true", "1", "yes", "t")
        return str(value)

    def _match(self, ent: dict[str, Any], rec: dict[str, Any], slug: str, op: str, raw: Any) -> bool:
        value = self._typed(ent, slug, rec.get(slug))
        if op == "isnull":
            want = raw is None or str(raw).strip().lower() in ("true", "1", "yes", "y")
            return (value is None) == want
        if op == "in":
            values = [self._typed(ent, slug, v) for v in (raw.split(",") if raw else [])]
            if len(values) > 200:
                raise ApiError(400, "'in' filter accepts at most 200 values")
            return value in values
        if op == "contains":
            return value is not None and str(raw).lower() in str(value).lower()
        target = self._typed(ent, slug, raw)
        if value is None:
            return op == "ne" and target is not None
        return {
            "eq": value == target,
            "ne": value != target,
            "gt": value > target,
            "gte": value >= target,
            "lt": value < target,
            "lte": value <= target,
        }[op]

    def list(self, slug: str, query: dict[str, list[str]]) -> dict[str, Any]:
        ent = self.entity(slug)
        valid = set(self.field_map(ent)) | set(self.rel_map(ent)) | set(BASE_COLUMNS)
        clauses = []
        for raw in query.get("filter", []):
            parts = raw.split(":", 2)
            if len(parts) < 2 or not parts[0]:
                raise ApiError(400, f"invalid filter {raw!r}; expected '<field>:<op>[:<value>]'")
            if parts[1] not in OPS:
                raise ApiError(400, f"unknown filter operator {parts[1]!r}")
            if parts[0] not in valid:
                raise ApiError(400, f"unknown field: {parts[0]!r}")
            clauses.append((parts[0], parts[1], parts[2] if len(parts) == 3 else None))
        limit = int((query.get("limit") or ["50"])[0])
        if not 1 <= limit <= 200:
            raise ApiError(422, "limit must be between 1 and 200")
        order_by = (query.get("order_by") or ["created_at"])[0]
        if order_by not in valid:
            raise ApiError(400, f"unknown field: {order_by!r}")
        order_dir = (query.get("order_dir") or ["desc"])[0]
        rows = [r for r in self.records[slug].values() if all(self._match(ent, r, *c) for c in clauses)]

        def key(rec: dict[str, Any]) -> tuple[Any, ...]:
            value = self._typed(ent, order_by, rec.get(order_by))
            return (value is None, value if value is not None else 0, rec["_seq"])

        rows.sort(key=key, reverse=order_dir == "desc")
        start = 0
        if query.get("cursor"):
            try:
                cur = json.loads(base64.urlsafe_b64decode(query["cursor"][0].encode()))
            except ValueError as exc:
                raise ApiError(400, "invalid cursor") from exc
            if cur["s"] != order_by or cur["d"] != order_dir:
                raise ApiError(400, "cursor does not match the requested sort order")
            ids = [r["id"] for r in rows]
            start = ids.index(cur["id"]) + 1 if cur["id"] in ids else len(rows)
        page = rows[start : start + limit]
        has_more = start + limit < len(rows)
        next_cursor = None
        if has_more and page:
            last = page[-1]
            token = {"s": order_by, "d": order_dir, "v": last.get(order_by), "id": last["id"]}
            next_cursor = base64.urlsafe_b64encode(json.dumps(token).encode()).decode()
        return {"items": [self.public(r) for r in page], "next_cursor": next_cursor, "limit": limit}

    def count(self, slug: str) -> int:
        return len(self.records.get(slug, {}))

    def all(self, slug: str) -> list[dict[str, Any]]:
        return sorted((self.public(r) for r in self.records.get(slug, {}).values()), key=lambda r: r["created_at"])


def _parse_multipart(content_type: str, body: bytes) -> dict[str, bytes]:
    msg = email.parser.BytesParser(policy=email.policy.HTTP).parsebytes(
        f"Content-Type: {content_type}\r\n\r\n".encode() + body
    )
    parts: dict[str, bytes] = {}
    for part in msg.iter_parts():
        name = part.get_param("name", header="content-disposition")
        if name:
            parts[str(name)] = part.get_payload(decode=True) or b""
    return parts


SCOPE_FOR = {
    ("POST", "promotions"): "config:write",
    ("GET", "entities"): "entities:read",
    ("GET", "records"): "records:read",
    ("POST", "records"): "records:write",
    ("PATCH", "records"): "records:write",
    ("DELETE", "records"): "records:write",
}


def make_handler(store: Store) -> type[BaseHTTPRequestHandler]:
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, fmt: str, *args: Any) -> None:  # silence
            return

        def _send(self, status: int, body: Any = None, headers: dict[str, str] | None = None) -> None:
            data = b"" if body is None else json.dumps(body).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            for k, v in (headers or {}).items():
                self.send_header(k, v)
            self.end_headers()
            self.wfile.write(data)

        def _body(self) -> bytes:
            length = int(self.headers.get("Content-Length") or 0)
            return self.rfile.read(length) if length else b""

        def _auth(self, scope: str) -> None:
            auth = self.headers.get("Authorization") or ""
            key = auth.removeprefix("Bearer ").strip() if auth.startswith("Bearer ") else self.headers.get("X-API-Key")
            if not key or not key.startswith("km2_") or key not in store.keys:
                raise ApiError(401, "Invalid or missing API key")
            if scope not in store.keys[key]:
                raise ApiError(403, f"API key lacks the {scope!r} scope")

        def _dispatch(self, method: str) -> None:
            url = urlparse(self.path)
            parts = [unquote(p) for p in url.path.split("/") if p]
            query = parse_qs(url.query, keep_blank_values=True)
            body = self._body()
            try:
                with store.lock:
                    store.request_log.append((method, self.path))
                    for i, (only, status, headers) in enumerate(store.failures):
                        if only is None or only == method:
                            store.failures.pop(i)
                            raise ApiError(status, "injected failure", headers)
                    if (
                        method != "GET"
                        and store.fail_after_writes is not None
                        and store.writes >= store.fail_after_writes
                    ):
                        raise ApiError(503, "injected outage")
                    status, result = self._route(method, parts, query, body)
                    if method != "GET" and "records" in parts:
                        store.writes += 1
                self._send(status, result)
            except ApiError as exc:
                self._send(exc.status, {"detail": exc.detail}, exc.headers)

        def _route(self, method: str, parts: list[str], query: dict[str, list[str]], body: bytes) -> tuple[int, Any]:
            if parts[:2] != ["api", "v1"]:
                raise ApiError(404, "Not Found")
            rest = parts[2:]
            if rest == ["config", "promotions"] and method == "POST":
                self._auth(SCOPE_FOR[("POST", "promotions")])
                files = _parse_multipart(self.headers.get("Content-Type") or "", body)
                if "file" not in files:
                    raise ApiError(422, "file is required")
                try:
                    bundle = json.loads(files["file"])
                except json.JSONDecodeError as exc:
                    raise ApiError(400, f"not a valid JSON bundle: {exc}") from exc
                dry = (query.get("dry_run") or ["false"])[0] == "true"
                if (query.get("strategy") or ["skip"])[0] != "skip":
                    raise ApiError(400, "fake only supports strategy=skip")
                return 200, store.install_bundle(bundle, dry)
            if rest[:1] == ["entities"]:
                if len(rest) == 1 and method == "GET":
                    self._auth("entities:read")
                    items = [self._entity_read(e) for e in store.entities.values()]
                    return 200, {"items": items, "total": len(items), "page": 1, "page_size": 20, "pages": 1}
                if len(rest) == 2 and method == "GET":
                    self._auth("entities:read")
                    return 200, self._entity_read(store.entity(rest[1]))
                if len(rest) >= 3 and rest[2] == "records":
                    self._auth(SCOPE_FOR.get((method, "records"), "records:write"))
                    slug = rest[1]
                    if len(rest) == 3 and method == "GET":
                        return 200, store.list(slug, query)
                    if len(rest) == 3 and method == "POST":
                        return 201, store.create(slug, json.loads(body or b"{}"))
                    if len(rest) == 4:
                        try:
                            uuid.UUID(rest[3])
                        except ValueError as exc:
                            raise ApiError(422, "invalid record id") from exc
                        if method == "GET":
                            rec = store.records[store.entity(slug)["slug"]].get(rest[3])
                            if rec is None:
                                raise ApiError(404, "record not found")
                            return 200, store.public(rec)
                        if method == "PATCH":
                            return 200, store.update(slug, rest[3], json.loads(body or b"{}"))
                        if method == "DELETE":
                            store.delete(slug, rest[3])
                            return 204, None
            raise ApiError(404, "Not Found")

        @staticmethod
        def _entity_read(ent: dict[str, Any]) -> dict[str, Any]:
            fields = [
                {
                    k: f[k]
                    for k in (
                        "id",
                        "name",
                        "slug",
                        "field_type",
                        "picklist_options",
                        "is_required",
                        "is_unique",
                        "default_value",
                        "order",
                        "read_access",
                    )
                }
                for f in ent["fields"]
            ]
            return {
                "id": ent["id"],
                "name": ent["name"],
                "slug": ent["slug"],
                "description": ent["description"],
                "is_active": True,
                "write_access": "member",
                "fields": fields,
            }

        def do_GET(self) -> None:
            self._dispatch("GET")

        def do_POST(self) -> None:
            self._dispatch("POST")

        def do_PATCH(self) -> None:
            self._dispatch("PATCH")

        def do_DELETE(self) -> None:
            self._dispatch("DELETE")

    return Handler


class FakeKM2:
    """Context manager running the fake on 127.0.0.1:<random port>."""

    API_KEY = "km2_test_simulator"
    BOOTSTRAP_KEY = "km2_test_bootstrap"

    def __init__(self, port: int = 0) -> None:
        self.store = Store()
        self.store.keys[self.API_KEY] = {"entities:read", "records:read", "records:write"}
        self.store.keys[self.BOOTSTRAP_KEY] = {"config:read", "config:write"}
        self.server = ThreadingHTTPServer(("127.0.0.1", port), make_handler(self.store))
        self.thread = threading.Thread(target=self.server.serve_forever, kwargs={"poll_interval": 0.02}, daemon=True)

    @property
    def url(self) -> str:
        host, port = self.server.server_address[:2]
        return f"http://{host}:{port}"

    def __enter__(self) -> FakeKM2:
        self.thread.start()
        return self

    def __exit__(self, *exc: object) -> None:
        self.server.shutdown()
        self.server.server_close()


def main() -> None:  # pragma: no cover - manual use
    parser = argparse.ArgumentParser(description="Run the fake KM2 for a manual pmo_sim check")
    parser.add_argument("--port", type=int, default=8765)
    args = parser.parse_args()
    fake = FakeKM2(args.port)
    print(
        f"fake KM2 on {fake.url}\nKM2_BASE_URL={fake.url}\nKM2_API_KEY={fake.API_KEY}\n"
        f"KM2_BOOTSTRAP_KEY={fake.BOOTSTRAP_KEY}"
    )
    with fake, contextlib.suppress(KeyboardInterrupt):
        fake.thread.join()


if __name__ == "__main__":  # pragma: no cover
    main()
