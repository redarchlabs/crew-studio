#!/usr/bin/env python3
"""Fake Jira Cloud (and a little Linear) for the Crew Studio E2E suite (stdlib only, never contacts a real tracker).

Answers the documented Jira REST v3 shapes Crew Studio uses (docs/trackers.md) from a small project PRJ:
  PRJ-10  "Atlas rollout" (in progress, due far ahead)
  PRJ-11  child of PRJ-10, assigned to Sam, due three days ago, labelled blocked, with one comment (from eight days ago)
  PRJ-12  child of PRJ-10, done
  PRJ-13  child of PRJ-11 (a sub-task), unassigned, due after PRJ-10
Every request needs Basic auth for e2e-bot@e2e.example with the token "e2e-jira-token". Posted comments and created
issues are kept and returned by GET /__posted. Linear: POST /graphql answers viewer with the key "e2e-linear-key".
GET /__health answers ok.
"""
import argparse
import base64
import datetime
import json
import re
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, unquote, urlparse

EMAIL, TOKEN, LINEAR_KEY = "e2e-bot@e2e.example", "e2e-jira-token", "e2e-linear-key"
AUTH = "Basic " + base64.b64encode(f"{EMAIL}:{TOKEN}".encode()).decode()
LOCK = threading.Lock()
POSTED: list[dict] = []

ME = {"accountId": "e2e-bot", "displayName": "Studio Bot", "emailAddress": EMAIL, "accountType": "atlassian", "active": True}
SAM = {"accountId": "acc-sam", "displayName": "Sam Lee", "emailAddress": "sam@e2e.example", "accountType": "atlassian", "active": True}
DANA = {"accountId": "acc-dana", "displayName": "Dana Ruiz", "accountType": "atlassian", "active": True}


def day(offset: int) -> str:
    return (datetime.date.today() + datetime.timedelta(days=offset)).isoformat()


def stamp(offset: int) -> str:
    """A Jira timestamp at 09:00 UTC, offset days from today."""
    return day(offset) + "T09:00:00.000+0000"


# Every date is counted from today, so overdue, stale and "commented N days ago" read the same whenever the suite runs
UPDATED = stamp(-9)
CATEGORY = {"To Do": "new", "In Progress": "indeterminate", "Done": "done"}


def issue(key: str, title: str, status: str, due: str | None, parent: str | None = None, assignee=None, labels=()):
    return {"key": key, "title": title, "status": status, "due": due, "parent": parent, "assignee": assignee, "labels": list(labels)}


ISSUES = {i["key"]: i for i in [
    issue("PRJ-10", "Atlas rollout", "In Progress", day(60)),
    issue("PRJ-11", "Vendor access", "To Do", day(-3), "PRJ-10", SAM, ["blocked"]),
    issue("PRJ-12", "Docs", "Done", None, "PRJ-10"),
    issue("PRJ-13", "Sub-task", "To Do", day(90), "PRJ-11"),
]}
COMMENTS = {"PRJ-11": [{"id": "1", "author": SAM, "created": stamp(-8), "text": "Blocked on vendor access."}]}


def adf(text: str) -> dict:
    return {"type": "doc", "version": 1, "content": [{"type": "paragraph", "content": [{"type": "text", "text": text}]}]}


def jira_issue(i: dict) -> dict:
    fields = {
        "summary": i["title"], "status": {"name": i["status"], "statusCategory": {"key": CATEGORY[i["status"]]}},
        "assignee": i["assignee"], "duedate": i["due"], "priority": {"name": "Medium"}, "updated": UPDATED,
        "labels": i["labels"], "project": {"key": "PRJ", "name": "Project PRJ"}, "description": adf("About " + i["title"]),
    }
    if i["parent"]:
        fields["parent"] = {"key": i["parent"]}
    return {"id": str(10000 + int(i["key"].split("-")[1])), "key": i["key"], "fields": fields}


def search(jql: str) -> list[dict]:
    project = re.search(r'project = "([A-Z0-9_]+)"', jql)
    parent = re.search(r'parent = "([A-Z0-9_]+-\d+)"', jql)
    open_only = "statusCategory != Done" in jql
    out = []
    for i in sorted(ISSUES.values(), key=lambda x: int(x["key"].split("-")[1])):
        if open_only and i["status"] == "Done":
            continue
        if parent and i["parent"] != parent.group(1):
            continue
        if project and not parent and project.group(1) != "PRJ":
            continue
        out.append(jira_issue(i))
    return out


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):  # quiet
        pass

    def send(self, status: int, body) -> None:
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def body(self) -> dict:
        n = int(self.headers.get("Content-Length") or 0)
        try:
            return json.loads(self.rfile.read(n) or b"{}")
        except ValueError:
            return {}

    def do_GET(self):
        url = urlparse(self.path)
        if url.path == "/__health":
            return self.send(200, {"ok": True})
        if url.path == "/__posted":
            with LOCK:
                return self.send(200, list(POSTED))
        if self.headers.get("Authorization") != AUTH:
            return self.send(401, {"errorMessages": ["Client must be authenticated to access this resource."]})
        q = parse_qs(url.query)
        path = unquote(url.path)
        if path == "/rest/api/3/myself":
            return self.send(200, ME)
        if path == "/rest/api/3/project/search":
            return self.send(200, {"isLast": True, "values": [{"id": "1", "key": "PRJ", "name": "Project PRJ", "lead": DANA}]})
        if path == "/rest/api/3/project/PRJ":
            return self.send(200, {"id": "1", "key": "PRJ", "name": "Project PRJ", "lead": DANA})
        if path == "/rest/api/3/users/search":
            return self.send(200, [SAM, DANA, ME] if q.get("startAt", ["0"])[0] == "0" else [])
        if path == "/rest/api/3/search/jql":
            return self.send(200, {"issues": search(q.get("jql", [""])[0]), "isLast": True})
        m = re.fullmatch(r"/rest/api/3/issue/([A-Z0-9_]+-\d+)(/comment)?", path)
        if m and m.group(1) in ISSUES:
            if m.group(2):
                comments = [{"id": c["id"], "author": c["author"], "created": c["created"], "body": adf(c["text"])} for c in COMMENTS.get(m.group(1), [])]
                return self.send(200, {"startAt": 0, "total": len(comments), "comments": list(reversed(comments))})
            return self.send(200, jira_issue(ISSUES[m.group(1)]))
        return self.send(404, {"errorMessages": ["Issue does not exist or you do not have permission to see it."]})

    def do_POST(self):
        url = urlparse(self.path)
        if url.path == "/graphql":
            if self.headers.get("Authorization") != LINEAR_KEY:
                return self.send(401, {"errors": [{"message": "Authentication required", "extensions": {"code": "AUTHENTICATION_ERROR"}}]})
            return self.send(200, {"data": {"viewer": {"id": "lin-bot", "name": "Linear Bot", "displayName": "bot", "email": "bot@e2e.example", "url": ""}}})
        if self.headers.get("Authorization") != AUTH:
            return self.send(401, {"errorMessages": ["unauthorized"]})
        m = re.fullmatch(r"/rest/api/3/issue/([A-Z0-9_]+-\d+)/comment", unquote(url.path))
        if m and m.group(1) in ISSUES:
            with LOCK:
                # The author is the account the request authenticated as: always the connection's bot
                POSTED.append({"issue": m.group(1), "author": ME["accountId"], "body": self.body().get("body")})
                return self.send(201, {"id": str(100 + len(POSTED))})
        return self.send(404, {"errorMessages": ["not found"]})


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--port", type=int, default=8094)
    args = p.parse_args()
    srv = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    srv.serve_forever()


if __name__ == "__main__":
    main()
