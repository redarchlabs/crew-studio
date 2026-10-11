#!/usr/bin/env python3
"""Fake LLM provider for the Crew Studio E2E suite (stdlib only, never contacts a real provider).

Speaks three wire formats and answers every call with a CrewAI ReAct final answer plus token usage:
  POST /v1/chat/completions   OpenAI Chat Completions (also non-/v1 path)
  POST /v1/responses          OpenAI Responses API
  POST /v1/messages           Anthropic Messages
  POST /v1/embeddings         OpenAI embeddings: deterministic 768-number vectors (see fake_embedding); not logged
Every request is appended as one JSON line to the file given by --log, so tests can assert on what the
model was sent (for example that a pasted image arrived as a base64 image part).

Behaviour switches, found anywhere in the request text:
  E2E_SLOW_MARKER   the reply is delayed by --slow-seconds (default 90) so a run can be cancelled
  E2E_FAIL_MARKER   the server answers HTTP 500
  E2E_CHAT_PROPOSE  (chat completions with tools) the first call asks for the start_work_order tool
  E2E_CHAT_NOTE     (chat completions with tools) the first call asks for the propose_note_change tool
  E2E_KM_SAVE       (chat completions with tools) the first call asks for the Save to Knowledge (KM2) tool
  E2E_KM_ASK        (chat completions with tools) the first call asks for the Ask Knowledge (KM2) tool
  E2E_CHAT_TRACK    (chat completions with tools) the first call asks for the propose_tracking_item tool
  E2E_DELIVER_REPORT (chat completions with tools) the first call asks for the Deliver Report tool (project Atlas)
  E2E_DELIVER_DAILY (chat completions with tools) the first call asks for the Deliver Report tool (project ERP, daily)
  E2E_MEMORY_REMEMBER (chat completions with tools) the first call asks for the Remember tool (crew memory, explicit saves)
  E2E_KM2_TRACKER_COMMENT (chat completions with tools) the first call asks for Comment on Tracker Issue on ERP-2 (KM2 issues)
  E2E_AGENT_ASK     (chat completions with tools) the first call asks the coworker "E2E Specialist reviewer" a question (agent chat)
  E2E_AGENT_PROPOSE (chat completions with tools) the first call asks for Start Crew Work Order on "E2E Crew" (agent chat)
  E2E_AGENT_HANDOFF (chat completions with tools) the first call asks for Hand off work to "E2E Specialist reviewer" (agent chat)
  E2E_SLOW_HANDOFF  (chat completions with tools) the same, with E2E_SLOW_MARKER in the brief so the hand-off can be cancelled
  E2E_AGENT_QUESTION (chat completions with tools) Hand off work to "E2E Specialist reviewer" with E2E_HANDOFF_ASK_LEAD in the brief
  E2E_HANDOFF_ASK_LEAD (chat completions with tools) the specialist asks the lead with Ask the lead (E2E_LEAD_ASK_PERSON in the question)
  E2E_LEAD_ASK_PERSON (chat completions with tools) the lead desk asks the person with Ask a human
  E2E_PROPOSE_HANDOFF (chat completions with tools) Hand off work to "E2E Specialist reviewer" with E2E_SPECIALIST_PROPOSE in the brief
  E2E_SPECIALIST_PROPOSE (chat completions with tools) the specialist proposes a comment on PRJ-10 with Propose Tracker Comment
  E2E_AGENT_MCP     (chat completions with tools) the agent calls the MCP read tool mcp_lookup_note (fake_mcp.py) about Atlas
A scripted tool call is made once per run: once the request carries a tool result, the normal answer follows.
GET /__health answers ok; GET /__requests returns the recorded requests; GET /__embeddings counts embedding calls.
"""
import argparse
import functools
import hashlib
import json
import math
import random
import re
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

SLOW_MARKER = "E2E_SLOW_MARKER"
FAIL_MARKER = "E2E_FAIL_MARKER"
ANSWER_TAG = "FAKE-LLM-ANSWER"
# Scripted chat tool calls: marker -> (tool, arguments)
TOOL_SCRIPTS = {
    "E2E_CHAT_PROPOSE": ("start_work_order", {"title": "E2E chat sweep", "brief": "Sweep project Atlas now and list what slipped."}),
    "E2E_CHAT_NOTE": ("propose_note_change", {"name": "e2e-chat-note", "content": "Added from chat.", "base_version": 0}),
    "E2E_KM_SAVE": ("save_to_knowledge_km2", {"title": "E2E Atlas weekly", "external_ref": "e2e-atlas-weekly",
                                              "content": "# Atlas weekly\nOn track.", "metadata": {"project": "atlas"}}),
    "E2E_KM_ASK": ("ask_knowledge_km2", {"question": "Who owns Atlas?"}),
    "E2E_CHAT_TRACK": ("propose_tracking_item", {"item_ref": "PRJ-11", "project": "Atlas vendor", "include_children": "none"}),
    "E2E_DELIVER_REPORT": ("deliver_report", {"project": "Atlas", "markdown": "# Atlas weekly status\n\nOn track. PRJ-11 is blocked on vendor access."}),
    "E2E_DELIVER_DAILY": ("deliver_report", {"project": "ERP", "cadence": "daily",
                                             "markdown": "**At risk.** Vendor sandbox access is the critical path; Sam needs the credentials."}),
    "E2E_KM2_TRACKER_COMMENT": ("comment_on_tracker_issue", {"id": "ERP-2", "text": "Is the vendor sandbox unblocked yet?",
                                                             "mentions": ["9e000000-0000-4000-8000-000000000003"]}),
    "E2E_AGENT_ASK": ("ask_question_to_coworker", {"question": "What is the biggest risk for project Atlas?",
                                                   "context": "The person asked whether Atlas is on track.", "coworker": "E2E Specialist reviewer"}),
    "E2E_AGENT_READ_ISSUE": ("get_tracker_issue", {"id": "PRJ-11", "connection": ""}),
    "E2E_AGENT_PROPOSE": ("start_crew_work_order", {"crew": "E2E Crew", "title": "E2E agent run", "brief": "Research the Atlas risks.",
                                                    "inputs": [{"key": "topic", "value": "Atlas"}]}),
    "E2E_AGENT_HANDOFF": ("hand_off_work", {"worker": "E2E Specialist reviewer", "title": "E2E vendor comparison",
                                            "brief": "Compare the vendors."}),
    "E2E_SLOW_HANDOFF": ("hand_off_work", {"worker": "E2E Specialist reviewer", "title": "E2E slow comparison",
                                           "brief": "Compare the vendors slowly. E2E_SLOW_MARKER"}),
    "E2E_AGENT_QUESTION": ("hand_off_work", {"worker": "E2E Specialist reviewer", "title": "E2E price comparison",
                                             "brief": "Compare the vendors' prices. E2E_HANDOFF_ASK_LEAD"}),
    "E2E_HANDOFF_ASK_LEAD": ("ask_the_lead", {"question": "List or negotiated prices? E2E_LEAD_ASK_PERSON",
                                              "context": "The comparison needs one price model."}),
    "E2E_LEAD_ASK_PERSON": ("ask_a_human", {"question": "Which prices should the comparison use?",
                                            "context": "The specialist must pick list or negotiated prices."}),
    "E2E_PROPOSE_HANDOFF": ("hand_off_work", {"worker": "E2E Specialist reviewer", "title": "E2E summary post",
                                              "brief": "Post the summary on PRJ-10. E2E_SPECIALIST_PROPOSE"}),
    "E2E_SPECIALIST_PROPOSE": ("propose_tracker_comment", {"connection": "", "issue": "PRJ-10", "text": "Summary posted from E2E."}),
    "E2E_AGENT_MCP": ("mcp_lookup_note", {"query": "Atlas"}),
    "E2E_MEMORY_REMEMBER": ("remember", {"content": "The vendor status page lags real outages by about an hour", "scope": "/vendors/acme",
                                         "categories": ["vendor"], "importance": 0.7}),
}
LOCK = threading.Lock()
ARGS = None
EMBED_DIMENSIONS = 768
# The whole text's own direction, mixed into the bag of words so identical words in another order still differ
WHOLE_TEXT_WEIGHT = 0.2
EMBED_COUNTS = {"requests": 0, "texts": 0}


@functools.lru_cache(maxsize=4096)
def _hashed_vector(seed_text, dimensions):
    """A unit vector seeded from a SHA-256 of the text: the same text always gives the same vector."""
    rng = random.Random(int.from_bytes(hashlib.sha256(seed_text.encode("utf-8")).digest()[:8], "big"))
    v = [rng.gauss(0.0, 1.0) for _ in range(dimensions)]
    norm = math.sqrt(sum(x * x for x in v)) or 1.0
    return tuple(x / norm for x in v)


def fake_embedding(text, dimensions=EMBED_DIMENSIONS):
    """Identical text gives an identical vector and different text a different one. Texts that share words point
    the same way (a bag of hashed words), so a search by meaning finds memories that use the query's words."""
    words = re.findall(r"[a-z0-9]+", text.lower())
    total = [0.0] * dimensions
    for w in words:
        for i, x in enumerate(_hashed_vector("w:" + w, dimensions)):
            total[i] += x
    norm = math.sqrt(sum(x * x for x in total))
    whole = _hashed_vector("t:" + text, dimensions)
    if norm == 0:
        return list(whole)
    mixed = [(1 - WHOLE_TEXT_WEIGHT) * x / norm + WHOLE_TEXT_WEIGHT * y for x, y in zip(total, whole)]
    norm = math.sqrt(sum(x * x for x in mixed)) or 1.0
    return [round(x / norm, 6) for x in mixed]


def embeddings_response(req):
    """(status, body) for an OpenAI embeddings request: input is a string or a list of strings."""
    inp = req.get("input")
    texts = [inp] if isinstance(inp, str) else inp
    if not isinstance(texts, list) or not texts or not all(isinstance(t, str) for t in texts):
        return 400, {"error": {"message": "input must be a string or a list of strings", "type": "invalid_request_error"}}
    dims = req.get("dimensions") or EMBED_DIMENSIONS
    if not isinstance(dims, int) or not 1 <= dims <= 3072:
        return 400, {"error": {"message": "dimensions must be 1 to 3072", "type": "invalid_request_error"}}
    with LOCK:
        EMBED_COUNTS["requests"] += 1
        EMBED_COUNTS["texts"] += len(texts)
    tokens = sum(max(1, len(t) // 4) for t in texts)
    return 200, {
        "object": "list",
        "data": [{"object": "embedding", "index": i, "embedding": fake_embedding(t, dims)} for i, t in enumerate(texts)],
        "model": req.get("model") or "text-embedding-3-small",
        "usage": {"prompt_tokens": tokens, "total_tokens": tokens},
    }


def text_parts(value):
    """All text found in a message content value (string, or list of typed parts)."""
    if isinstance(value, str):
        return [value]
    out = []
    if isinstance(value, list):
        for part in value:
            if isinstance(part, str):
                out.append(part)
            elif isinstance(part, dict):
                for key in ("text", "content", "input_text", "output_text"):
                    v = part.get(key)
                    if isinstance(v, str):
                        out.append(v)
                    elif isinstance(v, list):
                        out.extend(text_parts(v))
    return out


def collect(req):
    """(joined text, number of image parts, image mime types) across every wire format."""
    texts, images, mimes = [], 0, []
    system = req.get("system")
    if system:
        texts.extend(text_parts(system))
    if req.get("instructions"):
        texts.append(str(req["instructions"]))
    items = list(req.get("messages") or [])
    inp = req.get("input")
    if isinstance(inp, str):
        texts.append(inp)
    elif isinstance(inp, list):
        items.extend(inp)
    for msg in items:
        content = msg.get("content") if isinstance(msg, dict) else None
        texts.extend(text_parts(content))
        if isinstance(content, list):
            for part in content:
                if not isinstance(part, dict):
                    continue
                kind = part.get("type", "")
                url = ""
                if kind == "image_url":
                    iu = part.get("image_url")
                    url = iu.get("url", "") if isinstance(iu, dict) else str(iu or "")
                elif kind == "input_image":
                    url = str(part.get("image_url", ""))
                elif kind == "image":  # Anthropic
                    src = part.get("source") or {}
                    images += 1
                    mimes.append(src.get("media_type", ""))
                    continue
                if kind in ("image_url", "input_image"):
                    images += 1
                    m = re.match(r"data:([^;,]+)", url)
                    mimes.append(m.group(1) if m else ("url" if url else ""))
    return "\n".join(texts), images, mimes


def tool_names(req):
    return [t.get("function", {}).get("name", "") for t in req.get("tools") or [] if isinstance(t, dict)]


def scripted_tool_call(req, text):
    """The tool call a marker asks for, unless this run already called a tool (its result is in the messages)."""
    if any(isinstance(m, dict) and m.get("role") == "tool" for m in req.get("messages") or []):
        return None
    for marker, (tool, args) in TOOL_SCRIPTS.items():
        if marker in text and tool in tool_names(req):
            return tool, args
    return None


def final_answer(text):
    """A different, deterministic answer per prompt so journals/outputs are distinguishable."""
    wo = re.search(r"Work order WO-(\d+)", text)
    subject = f"WO-{wo.group(1)}" if wo else "run"
    m = re.search(r"Current Task:\s*(.{0,60})", text)
    task = (m.group(1).splitlines() or [""])[0].strip() if m else ""
    return (
        "Thought: I now know the final answer\n"
        f"Final Answer: # {ANSWER_TAG}\n\n- Subject: {subject}\n- Task: {task}\n- Result: all checks passed"
    )


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def _send(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/__health"):
            return self._send(200, {"status": "ok"})
        if self.path.startswith("/__requests"):
            try:
                with open(ARGS.log, encoding="utf-8") as f:
                    return self._send(200, [json.loads(line) for line in f if line.strip()])
            except OSError:
                return self._send(200, [])
        if self.path.startswith("/__embeddings"):
            with LOCK:
                return self._send(200, dict(EMBED_COUNTS))
        if self.path.rstrip("/").endswith("/models"):
            return self._send(200, {"object": "list", "data": [{"id": "fake", "object": "model"}]})
        self._send(404, {"error": "not found"})

    def do_POST(self):
        length = int(self.headers.get("content-length") or 0)
        try:
            req = json.loads(self.rfile.read(length) or b"{}")
        except ValueError:
            return self._send(400, {"error": "bad json"})
        path = self.path.split("?")[0].rstrip("/")
        if path.endswith("/embeddings"):
            # Kept out of the request log, which specs read as the list of model calls
            return self._send(*embeddings_response(req))
        text, images, mimes = collect(req)
        record = {
            "ts": time.time(), "path": path, "model": req.get("model"), "stream": bool(req.get("stream")),
            "image_count": images, "image_mimes": mimes, "text": text,
            "has_tools": bool(req.get("tools")), "tool_names": tool_names(req),
        }
        with LOCK:
            with open(ARGS.log, "a", encoding="utf-8") as f:
                f.write(json.dumps(record) + "\n")
        if FAIL_MARKER in text:
            return self._send(500, {"error": {"message": "forced failure", "type": "server_error"}})
        if SLOW_MARKER in text:
            deadline = time.time() + ARGS.slow_seconds
            while time.time() < deadline:
                time.sleep(0.25)
        else:
            time.sleep(ARGS.delay)
        answer = final_answer(text)
        usage_in, usage_out = 1200 + images * 300, 150
        model = req.get("model") or "fake"
        try:
            if path.endswith("/chat/completions"):
                call = scripted_tool_call(req, text)
                if call and not req.get("stream"):
                    return self._send(200, {
                        "id": "chatcmpl-fake", "object": "chat.completion", "created": int(time.time()), "model": model,
                        "choices": [{"index": 0, "finish_reason": "tool_calls", "message": {
                            "role": "assistant", "content": None,
                            "tool_calls": [{"id": "call_fake", "type": "function",
                                            "function": {"name": call[0], "arguments": json.dumps(call[1])}}]}}],
                        "usage": {"prompt_tokens": usage_in, "completion_tokens": 40, "total_tokens": usage_in + 40},
                    })
                if req.get("stream"):
                    return self._stream_chat(model, answer, usage_in, usage_out)
                return self._send(200, {
                    "id": "chatcmpl-fake", "object": "chat.completion", "created": int(time.time()), "model": model,
                    "choices": [{"index": 0, "finish_reason": "stop", "message": {"role": "assistant", "content": answer}}],
                    "usage": {"prompt_tokens": usage_in, "completion_tokens": usage_out, "total_tokens": usage_in + usage_out},
                })
            if path.endswith("/responses"):
                return self._send(200, {
                    "id": "resp_fake", "object": "response", "created_at": int(time.time()), "status": "completed", "model": model,
                    "output": [{"type": "message", "id": "msg_fake", "role": "assistant", "status": "completed",
                                "content": [{"type": "output_text", "text": answer, "annotations": []}]}],
                    "usage": {"input_tokens": usage_in, "output_tokens": usage_out, "total_tokens": usage_in + usage_out,
                              "input_tokens_details": {"cached_tokens": 0}, "output_tokens_details": {"reasoning_tokens": 0}},
                })
            if path.endswith("/messages"):
                return self._send(200, {
                    "id": "msg_fake", "type": "message", "role": "assistant", "model": model,
                    "content": [{"type": "text", "text": answer}], "stop_reason": "end_turn", "stop_sequence": None,
                    "usage": {"input_tokens": usage_in, "output_tokens": usage_out},
                })
        except (BrokenPipeError, ConnectionResetError):
            return
        self._send(404, {"error": "unknown path " + path})

    def _stream_chat(self, model, answer, tin, tout):
        self.send_response(200)
        self.send_header("content-type", "text/event-stream")
        self.send_header("connection", "close")
        self.end_headers()
        base = {"id": "chatcmpl-fake", "object": "chat.completion.chunk", "created": int(time.time()), "model": model}
        chunks = [
            {**base, "choices": [{"index": 0, "delta": {"role": "assistant", "content": answer}, "finish_reason": None}]},
            {**base, "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}]},
            {**base, "choices": [], "usage": {"prompt_tokens": tin, "completion_tokens": tout, "total_tokens": tin + tout}},
        ]
        for c in chunks:
            self.wfile.write(f"data: {json.dumps(c)}\n\n".encode())
        self.wfile.write(b"data: [DONE]\n\n")
        self.close_connection = True


def main():
    global ARGS
    p = argparse.ArgumentParser()
    p.add_argument("--port", type=int, required=True)
    p.add_argument("--log", required=True)
    p.add_argument("--delay", type=float, default=0.3)
    p.add_argument("--slow-seconds", type=float, default=90)
    ARGS = p.parse_args()
    open(ARGS.log, "w").close()
    srv = ThreadingHTTPServer(("127.0.0.1", ARGS.port), Handler)
    srv.daemon_threads = True
    srv.serve_forever()


if __name__ == "__main__":
    main()
