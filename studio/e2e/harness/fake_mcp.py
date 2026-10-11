#!/usr/bin/env python3
"""Fake outside MCP server for the Crew Studio E2E suite (stdlib only): the server an MCP connection points at.

Speaks just enough MCP over Streamable HTTP (JSON responses, no sessions) for Crew Studio's client:
  POST /mcp   initialize, notifications (202), tools/list, tools/call
Every /mcp request needs "Authorization: Bearer e2e-mcp-secret" (401 otherwise). Tools:
  lookup_note   returns "E2E-MCP-NOTE: notes for <query>"; no readOnlyHint, so it is discovered as Changes
  delete_note   "deletes" a note; no readOnlyHint
  server_time   says it only reads (readOnlyHint), so it is discovered as Reads
  bad name      a name Studio refuses, so discovery skips it
GET /__calls lists the tools/call requests received (name, arguments, whether the auth header was right), GET /__health
answers ok. Not to be confused with Crew Studio's own /mcp endpoint.
"""
import argparse
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

SECRET = "Bearer e2e-mcp-secret"
NOTE_TAG = "E2E-MCP-NOTE"
QUERY_SCHEMA = {"type": "object", "properties": {"query": {"type": "string", "description": "What to look for"}}, "required": ["query"]}
TOOLS = [
    {"name": "lookup_note", "description": "Looks up the team's notes about a topic.", "inputSchema": QUERY_SCHEMA},
    {"name": "delete_note", "description": "Deletes a note.", "inputSchema": QUERY_SCHEMA},
    {"name": "server_time", "description": "Says what time it is on the server.", "inputSchema": {"type": "object"},
     "annotations": {"readOnlyHint": True}},
    {"name": "bad name", "description": "A tool whose name has a space.", "inputSchema": {"type": "object"}},
]
LOCK = threading.Lock()
CALLS: list[dict] = []


def result(msg_id, value):
    return {"jsonrpc": "2.0", "id": msg_id, "result": value}


def error(msg_id, code, message):
    return {"jsonrpc": "2.0", "id": msg_id, "error": {"code": code, "message": message}}


def call_tool(msg_id, params):
    name = params.get("name")
    args = params.get("arguments") or {}
    with LOCK:
        CALLS.append({"name": name, "arguments": args})
    if name == "lookup_note":
        return result(msg_id, {"content": [{"type": "text", "text": f"{NOTE_TAG}: notes for {args.get('query', '')}"}]})
    if name == "delete_note":
        return result(msg_id, {"content": [{"type": "text", "text": "deleted"}]})
    if name == "server_time":
        return result(msg_id, {"content": [{"type": "text", "text": "2026-10-10T12:00:00Z"}]})
    return error(msg_id, -32602, f"unknown tool {name}")


def answer(msg):
    method, msg_id, params = msg.get("method"), msg.get("id"), msg.get("params") or {}
    if method == "initialize":
        return result(msg_id, {"protocolVersion": params.get("protocolVersion") or "2025-06-18", "capabilities": {"tools": {}},
                               "serverInfo": {"name": "fake-mcp", "version": "1.0"}})
    if method == "tools/list":
        return result(msg_id, {"tools": TOOLS})
    if method == "tools/call":
        return call_tool(msg_id, params)
    if method == "ping":
        return result(msg_id, {})
    return error(msg_id, -32601, f"method not found: {method}")


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def _send(self, code, payload=None):
        body = b"" if payload is None else json.dumps(payload).encode()
        self.send_response(code)
        if payload is not None:
            self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/__health":
            return self._send(200, {"ok": True})
        if self.path == "/__calls":
            with LOCK:
                return self._send(200, list(CALLS))
        # No standalone event stream
        return self._send(405, {"error": "method not allowed"})

    def do_DELETE(self):
        return self._send(405, {"error": "method not allowed"})

    def do_POST(self):
        length = int(self.headers.get("content-length") or 0)
        raw = self.rfile.read(length) if length else b""
        if self.path != "/mcp":
            return self._send(404, {"error": "not found"})
        if self.headers.get("authorization") != SECRET:
            return self._send(401, {"error": "unauthorized"})
        try:
            msg = json.loads(raw or b"{}")
        except ValueError:
            return self._send(400, error(None, -32700, "parse error"))
        if not isinstance(msg, dict):
            return self._send(400, error(None, -32600, "batches are not supported"))
        if "id" not in msg:
            # A notification (such as notifications/initialized): accepted, no answer
            return self._send(202)
        return self._send(200, answer(msg))


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--port", type=int, required=True)
    args = p.parse_args()
    ThreadingHTTPServer(("127.0.0.1", args.port), Handler).serve_forever()


if __name__ == "__main__":
    main()
