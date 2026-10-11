"""A fake KM2 organisation with the tracker entities, for km2_tracker connections (stdlib only, used by fake_km2.py).

Answers KM2's v1 catalog and records shapes (KM2 routers/v1/entities.py and records.py):
  GET  /api/v1/entities/{slug}                   the entity with its fields (404 "entity not found" when unknown)
  GET  /api/v1/entities/{slug}/records           filter=<field>:<op>[:<value>] (split at most twice; eq ne gt gte lt lte
                                                 in contains isnull), order_by/order_dir, limit <= 200, opaque cursor
  GET  /api/v1/entities/{slug}/records/{id}      one record (404 "record not found")
  POST /api/v1/entities/{slug}/records           creates trk_comment (and trk_issue) records; unknown fields are a 400
Relationship values are bare record ids. The key is "Authorization: Bearer km2_e2e-tracker-key".

Project ERP "ERP <-> CRM Integration" (lead Priya Patel):
  ERP-1  "ERP <-> CRM integration" umbrella, in progress, due far ahead
  ERP-2  child of ERP-1, Sam Lee's, due three days ago, labelled blocked, one comment by Sam
  ERP-3  child of ERP-1, done
  ERP-4  child of ERP-1, unassigned, to do
Test controls: POST /__tracker/comment {"issue": "ERP-2", "author": "sam", "body": "..."} adds a comment as a person,
POST /__tracker/issue {"issue": "ERP-4", "status": "In Progress", "status_category": "in_progress"} changes an issue,
GET /__tracker/comments lists every comment record, GET /__tracker/requests the records calls received.
"""
import base64
import datetime
import json
import threading
from urllib.parse import parse_qs, unquote, urlparse

KEY = "km2_e2e-tracker-key"
LOCK = threading.Lock()
REQUESTS: list[str] = []
PREFIX = "trk_"


def now() -> str:
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def day(offset: int) -> str:
    return (datetime.date.today() + datetime.timedelta(days=offset)).isoformat()


def uid(n: int) -> str:
    return f"9e000000-0000-4000-8000-{n:012d}"


CREATED = "2026-09-01T08:00:00+00:00"
UPDATED = "2026-10-01T09:00:00+00:00"
BOT, PRIYA, SAM, PROJECT = uid(1), uid(2), uid(3), uid(10)

FIELDS = {
    "person": ["name", "email", "role", "active", "is_bot"],
    "project": ["key", "name", "description", "status"],
    "issue": ["identifier", "number", "project_key", "title", "description", "issue_type", "status", "status_category", "priority",
              "labels", "estimate", "due_date", "parent_key", "activity_at"],
    "comment": ["body", "issue_key", "project_key", "author_name", "mentions", "sim_posted_at"],
}
RELS = {"person": [], "project": ["lead"], "issue": ["project", "parent", "assignee", "creator"], "comment": ["issue", "author"]}
PICKLISTS = {"status_category": ["to_do", "in_progress", "done", "canceled"],
             "status": ["Backlog", "Todo", "In Progress", "In Review", "Done", "Canceled", "Duplicate", "active", "planned", "paused",
                        "completed", "canceled"],
             "priority": ["Urgent", "High", "Medium", "Low", "None"], "issue_type": ["Epic", "Story", "Task", "Bug"]}
TYPES = {"number": "integer", "estimate": "integer", "active": "boolean", "is_bot": "boolean", "due_date": "date",
         "activity_at": "timestamptz", "sim_posted_at": "timestamptz", "description": "long_text", "body": "long_text"}


def base(rid: str, **fields) -> dict:
    """A record made at its own minute of the first of September, in the order the records are listed here."""
    minute = int(rid[-4:]) % 60
    return {"id": rid, "created_at": CREATED.replace("08:00", f"08:{minute:02d}"), "updated_at": UPDATED, **fields}


def issue(n: int, title: str, status: str, category: str, due, parent=None, assignee=None, labels=None) -> dict:
    return base(uid(100 + n), identifier=f"ERP-{n}", number=n, project_key="ERP", title=title, description=f"About {title}",
                issue_type="Task", status=status, status_category=category, priority="Medium", labels=labels, estimate=None,
                due_date=due, parent_key=f"ERP-{parent}" if parent else None, activity_at=UPDATED, project=PROJECT,
                parent=uid(100 + parent) if parent else None, assignee=assignee, creator=None)


RECORDS = {
    "person": [base(BOT, name="Crew Studio", email="crew-studio@e2e.example", role="bot", active=True, is_bot=True),
               base(PRIYA, name="Priya Patel", email="priya@e2e.example", role="PM", active=True, is_bot=False),
               base(SAM, name="Sam Lee", email="sam@e2e.example", role="Integration engineer", active=True, is_bot=False)],
    "project": [base(PROJECT, key="ERP", name="ERP <-> CRM Integration", description=None, status="active", lead=PRIYA)],
    "issue": [issue(1, "ERP <-> CRM integration", "In Progress", "in_progress", day(60)),
              issue(2, "Vendor sandbox credentials", "Todo", "to_do", day(-3), 1, SAM, "blocked"),
              issue(3, "Data mapping workshop", "Done", "done", None, 1),
              issue(4, "Customer sync design", "Todo", "to_do", day(30), 1)],
    "comment": [{**base(uid(500), body="Still waiting on the vendor's sandbox.", issue_key="ERP-2", project_key="ERP", author_name="Sam Lee",
                        mentions=None, sim_posted_at=None, issue=uid(102), author=SAM), "created_at": "2026-10-02T09:00:00+00:00"}],
}
NEXT = [600]


def entity(slug: str):
    name = slug[len(PREFIX):] if slug.startswith(PREFIX) else ""
    return name if name in FIELDS else None


def columns(name: str) -> list[str]:
    return ["id", "created_at", "updated_at", *FIELDS[name], *RELS[name]]


def as_time(v: str):
    try:
        return datetime.datetime.fromisoformat(v.replace("Z", "+00:00"))
    except ValueError:
        return None


def key_of(field: str, v):
    """A comparable form of a value: numbers as numbers, times as datetimes, the rest as text."""
    if v is None:
        return None
    if TYPES.get(field) == "integer":
        return float(v)
    if field in ("created_at", "updated_at") or TYPES.get(field) == "timestamptz":
        return as_time(str(v))
    return str(v)


def matches(rec: dict, field: str, op: str, value):
    have = rec.get(field)
    if op == "isnull":
        want = value is None or str(value).lower() in ("true", "1", "yes", "y")
        return (have is None) == want
    if have is None:
        return False
    if op == "in":
        return str(have) in (value or "").split(",")
    if op == "contains":
        return str(value).lower() in str(have).lower()
    a, b = key_of(field, have), key_of(field, value)
    return {"eq": a == b, "ne": a != b, "gt": a > b, "gte": a >= b, "lt": a < b, "lte": a <= b}[op]


def list_records(name: str, query: dict) -> tuple[int, dict]:
    recs = list(RECORDS[name])
    for raw in query.get("filter", []):
        parts = raw.split(":", 2)
        if len(parts) < 2 or not parts[0]:
            return 400, {"detail": f"invalid filter {raw!r}; expected '<field>:<op>[:<value>]'"}
        field, op = parts[0], parts[1]
        if field not in columns(name):
            return 400, {"detail": f"unknown field: {field!r}"}
        if op not in ("eq", "ne", "gt", "gte", "lt", "lte", "in", "contains", "isnull"):
            return 400, {"detail": f"unknown filter operator {op!r}"}
        value = parts[2] if len(parts) == 3 else None
        try:
            if op not in ("isnull", "in", "contains") and key_of(field, value) is None:
                raise ValueError(field)
            recs = [r for r in recs if matches(r, field, op, value)]
        except ValueError:
            return 400, {"detail": f"{field!r} must be a valid value"}
    order = (query.get("order_by") or ["created_at"])[0]
    if order not in columns(name):
        return 400, {"detail": f"unknown field: {order!r}"}
    desc = (query.get("order_dir") or ["desc"])[0] != "asc"
    recs.sort(key=lambda r: r["id"], reverse=True)  # id descending breaks ties
    present = [r for r in recs if r.get(order) is not None]
    present.sort(key=lambda r: key_of(order, r[order]), reverse=desc)
    nulls = [r for r in recs if r.get(order) is None]
    recs = nulls + present if desc else present + nulls  # Postgres: nulls first descending, last ascending
    limit = int((query.get("limit") or ["50"])[0])
    if not 1 <= limit <= 200:
        return 422, {"detail": [{"loc": ["query", "limit"], "msg": "Input should be less than or equal to 200"}]}
    start = 0
    if query.get("cursor"):
        try:
            cur = json.loads(base64.urlsafe_b64decode(query["cursor"][0]))
            start = cur["n"]
            if cur["s"] != order or cur["d"] != ("desc" if desc else "asc"):
                return 400, {"detail": "cursor does not match the requested sort order"}
        except (ValueError, KeyError):
            return 400, {"detail": "invalid cursor"}
    page = recs[start:start + limit]
    nxt = None
    if start + limit < len(recs):
        nxt = base64.urlsafe_b64encode(json.dumps({"s": order, "d": "desc" if desc else "asc", "n": start + limit}).encode()).decode()
    return 200, {"items": page, "next_cursor": nxt, "limit": limit}


def create(name: str, body: dict) -> tuple[int, dict]:
    unknown = sorted(k for k in body if k not in FIELDS[name] + RELS[name])
    if unknown:
        return 400, {"detail": f"unknown fields: {unknown}"}
    if name == "comment":
        target = next((i for i in RECORDS["issue"] if i["id"] == body.get("issue")), None)
        if not body.get("body") or target is None:
            return 400, {"detail": "'body' and 'issue' are required"}
    elif name == "issue":
        if any(i["identifier"] == body.get("identifier") for i in RECORDS["issue"]):
            return 409, {"detail": "a record with this identifier already exists"}
    else:
        return 403, {"detail": "the fake only creates comments and issues"}
    NEXT[0] += 1
    stamp = now()
    rec = {c: None for c in columns(name)} | body | {"id": uid(NEXT[0]), "created_at": stamp, "updated_at": stamp}
    RECORDS[name].append(rec)
    return 201, rec


def control(method: str, path: str, body: dict | None) -> tuple[int, object]:
    if method == "GET" and path == "/__tracker/comments":
        return 200, RECORDS["comment"]
    if method == "GET" and path == "/__tracker/requests":
        return 200, REQUESTS
    body = body or {}
    target = next((i for i in RECORDS["issue"] if i["identifier"] == body.get("issue")), None)
    if target is None:
        return 404, {"detail": "no such issue"}
    if path == "/__tracker/comment":
        author = {"sam": SAM, "priya": PRIYA}.get(body.get("author", "sam"), SAM)
        name = next(p["name"] for p in RECORDS["person"] if p["id"] == author)
        NEXT[0] += 1
        stamp = now()
        RECORDS["comment"].append({"id": uid(NEXT[0]), "created_at": stamp, "updated_at": stamp, "body": body.get("body", ""),
                                   "issue_key": target["identifier"], "project_key": "ERP", "author_name": name, "mentions": None,
                                   "sim_posted_at": None, "issue": target["id"], "author": author})
        return 201, RECORDS["comment"][-1]
    if path == "/__tracker/issue":
        for k in ("status", "status_category", "assignee", "due_date", "labels"):
            if k in body:
                target[k] = body[k]
        target["activity_at"] = target["updated_at"] = now()
        return 200, target
    return 404, {"detail": "Not Found"}


def handle(method: str, raw_path: str, auth: str, body: dict | None) -> tuple[int, object]:
    """(status, payload) for a tracker path, or (0, None) when the path is not the tracker's."""
    url = urlparse(raw_path)
    path = unquote(url.path)
    with LOCK:
        if path.startswith("/__tracker/"):
            return control(method, path, body)
        if not path.startswith("/api/v1/entities/"):
            return 0, None
        REQUESTS.append(f"{method} {raw_path}")
        if auth != f"Bearer {KEY}":
            return 401, {"detail": "Invalid or missing API key"}
        parts = path[len("/api/v1/entities/"):].split("/")
        name = entity(parts[0])
        if name is None:
            return 404, {"detail": "entity not found"}
        if len(parts) == 1 and method == "GET":
            fields = [{"id": f"f-{name}-{n}", "name": f, "slug": f, "field_type": "picklist" if f in PICKLISTS else TYPES.get(f, "text"), "picklist_options": PICKLISTS.get(f),
                       "is_required": False, "is_unique": f in ("identifier", "email", "key"), "default_value": None, "order": n,
                       "read_access": "member"} for n, f in enumerate(FIELDS[name])]
            return 200, {"id": f"e-{name}", "name": name, "slug": parts[0], "description": None, "is_active": True,
                         "write_access": "member", "fields": fields}
        if len(parts) == 2 and parts[1] == "records":
            if method == "GET":
                return list_records(name, parse_qs(url.query))
            if method == "POST":
                return create(name, body or {})
        if len(parts) == 3 and parts[1] == "records" and method == "GET":
            rec = next((r for r in RECORDS[name] if r["id"] == parts[2]), None)
            return (200, rec) if rec else (404, {"detail": "record not found"})
        return 405, {"detail": "Method Not Allowed"}
