#!/usr/bin/env python3
"""Fake KM2 v1 API for the Crew Studio E2E suite (stdlib only, never contacts a real KM2).

Answers the KM2 public v1 shapes Crew Studio uses (KM2 docs/API.md, "Enterprise API"):
  GET  /api/v1/knowledge/folders      three folders: Projects, Projects.Atlas, HR
  POST /api/v1/search                 one canned passage plus every saved document
  POST /api/v1/search/chat            a canned answer with one source
  POST /api/v1/knowledge/documents    JSON writes, versioned by external_ref per folder (201 created, 200 otherwise)
  GET  /api/v1/knowledge/documents/ID the saved document's status
Every /api/v1 request needs "Authorization: Bearer km2_e2e-key". GET /__requests lists the calls received (method,
path, body; never the key), GET /__saved the saved documents, GET /__health answers ok.

It is also a KM2 organisation with the tracker entities of a km2_tracker connection (fake_km2_tracker.py): the v1
catalog and records endpoints under /api/v1/entities, with the org-wide key "km2_e2e-tracker-key".
"""
import argparse
import hashlib
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import fake_km2_tracker as tracker

KEY = "km2_e2e-key"
PROJECTS, ATLAS, HR = ("6a1f0000-0000-4000-8000-000000000001", "6a1f0000-0000-4000-8000-000000000002",
                       "6a1f0000-0000-4000-8000-000000000003")
FOLDERS = [
    {"id": PROJECTS, "name": "Projects", "dot_path": "Projects", "parent_id": None, "order": 0},
    {"id": ATLAS, "name": "Atlas", "dot_path": "Projects.Atlas", "parent_id": PROJECTS, "order": 1},
    {"id": HR, "name": "HR", "dot_path": "HR", "parent_id": None, "order": 2},
]
RESERVED = {"access_keys", "tenant_id", "tags", "document_key", "document_id", "document_title", "type", "text",
            "summary", "summary_tree", "section", "chunk_order"}
LOCK = threading.Lock()
REQUESTS: list[dict] = []
DOCS: list[dict] = []


def upsert(folder: str, ref: str, title: str, content: str, meta: dict) -> tuple[int, str, dict]:
    digest = hashlib.sha256(content.encode()).hexdigest()
    for doc in DOCS:
        if ref and doc["folder_id"] == folder and doc["external_ref"] == ref:
            if doc["content_hash"] != digest:
                doc.update(content=content, content_hash=digest, title=title, metadata=meta)
                return 200, "updated", doc
            if doc["title"] != title:
                doc.update(title=title, metadata=meta)
                return 200, "metadata_updated", doc
            return 200, "unchanged", doc
    doc = {"id": f"7b2e0000-0000-4000-8000-{len(DOCS) + 1:012d}", "folder_id": folder, "external_ref": ref or None, "title": title,
           "content": content, "content_hash": digest, "metadata": meta}
    DOCS.append(doc)
    return 201, "created", doc


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def _send(self, code: int, payload) -> None:
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _body(self) -> dict:
        length = int(self.headers.get("content-length") or 0)
        try:
            data = json.loads(self.rfile.read(length) or b"{}")
        except ValueError:
            return {}
        return data if isinstance(data, dict) else {}

    def _authorised(self, body: dict) -> bool:
        with LOCK:
            REQUESTS.append({"method": self.command, "path": self.path, "body": body})
        if self.headers.get("Authorization") != f"Bearer {KEY}":
            self._send(401, {"detail": "Invalid or missing API key"})
            return False
        return True

    def _tracker(self, body: dict | None = None) -> bool:
        """Answers the tracker paths (records API and its test controls); False for every other path."""
        status, payload = tracker.handle(self.command, self.path, self.headers.get("Authorization") or "", body)
        if status == 0:
            return False
        self._send(status, payload)
        return True

    def do_GET(self):
        if self._tracker():
            return None
        if self.path.startswith("/__health"):
            return self._send(200, {"status": "ok"})
        if self.path.startswith("/__requests"):
            with LOCK:
                return self._send(200, list(REQUESTS))
        if self.path.startswith("/__saved"):
            with LOCK:
                return self._send(200, list(DOCS))
        if not self._authorised({}):
            return None
        if self.path == "/api/v1/knowledge/folders":
            return self._send(200, FOLDERS)
        if self.path.startswith("/api/v1/knowledge/documents/"):
            doc_id = self.path.rsplit("/", 1)[-1]
            with LOCK:
                doc = next((d for d in DOCS if d["id"] == doc_id), None)
            if doc:
                return self._send(200, {"id": doc["id"], "title": doc["title"], "processing_status": "SUCCESS",
                                        "folder_id": doc["folder_id"], "external_ref": doc["external_ref"]})
            return self._send(404, {"detail": "document not found"})
        return self._send(404, {"detail": "Not Found"})

    def do_POST(self):
        body = self._body()
        if self._tracker(body):
            return None
        if not self._authorised(body):
            return None
        if self.path == "/api/v1/search":
            hits = [{"id": "c1", "score": 0.91, "text": "Atlas charter: Dana owns delivery.", "document_id": "d1",
                     "document_key": "k1", "document_title": "Atlas charter", "chunk_order": 0}]
            with LOCK:
                hits += [{"id": "c-" + d["id"], "score": 0.5, "text": d["content"], "document_id": d["id"], "document_key": "k",
                          "document_title": d["title"], "chunk_order": 0} for d in DOCS]
            return self._send(200, {"hits": hits[: int(body.get("limit") or 5)], "total": len(hits)})
        if self.path == "/api/v1/search/chat":
            return self._send(200, {"answer": "KM2-FAKE-ANSWER: Dana owns Atlas.", "graph_context": [],
                                    "sources": [{"document_title": "Atlas charter", "document_id": "d1", "score": 0.9}]})
        if self.path == "/api/v1/knowledge/documents":
            return self._write(body)
        return self._send(404, {"detail": "Not Found"})

    def _write(self, body: dict):
        meta = body.get("metadata") or {}
        bad = sorted(set(meta) & RESERVED)
        if bad:
            return self._send(422, {"detail": [{"loc": ["body", "metadata"], "msg": "metadata may not set reserved field(s): " + ", ".join(bad)}]})
        folder, title, content = body.get("folder_id"), body.get("title"), body.get("content")
        if not title or not content:
            return self._send(422, {"detail": "title and content are required"})
        if folder not in (PROJECTS, ATLAS, HR):
            return self._send(404, {"detail": "folder not found"})
        if folder == HR:
            return self._send(403, {"detail": "This key's assignments may not add documents to this folder"})
        with LOCK:
            status, outcome, doc = upsert(folder, body.get("external_ref") or "", title, content, meta)
            answer = {"id": doc["id"], "document_key": "k-" + doc["id"], "external_ref": doc["external_ref"], "folder_id": folder,
                      "title": doc["title"], "processing_status": "PENDING", "content_hash": doc["content_hash"], "outcome": outcome}
        return self._send(status, answer)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=8095)
    args = parser.parse_args()
    ThreadingHTTPServer(("127.0.0.1", args.port), Handler).serve_forever()


if __name__ == "__main__":
    main()
