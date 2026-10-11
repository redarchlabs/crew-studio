"""Runs run_crew.py as the Go service does, against a fake OpenAI-compatible server (no real spend)."""

import json
import os
import subprocess
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

from .conftest import make_spec

RUNNER = Path(__file__).resolve().parent.parent / "run_crew.py"


class FakeOpenAI(BaseHTTPRequestHandler):
    paths: list[str] = []
    prompts: list[str] = []

    def do_POST(self):  # noqa: N802 - http.server naming
        length = int(self.headers.get("content-length", 0))
        request = json.loads(self.rfile.read(length) or b"{}")
        FakeOpenAI.paths.append(self.path)
        prompt = json.dumps(request)
        FakeOpenAI.prompts.append(prompt)
        answer = "Thought: I now know the final answer\nFinal Answer: " + ("REPORT about AI" if "Write about AI" in prompt else "NOTES about AI")
        body = json.dumps({
            "id": "chatcmpl-test", "object": "chat.completion", "created": 0, "model": request.get("model", "x"),
            "choices": [{"index": 0, "finish_reason": "stop", "message": {"role": "assistant", "content": answer}}],
            "usage": {"prompt_tokens": 1000, "completion_tokens": 100, "total_tokens": 1100},
        }).encode()
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


@pytest.fixture
def fake_openai():
    server = ThreadingHTTPServer(("127.0.0.1", 0), FakeOpenAI)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    yield f"http://127.0.0.1:{server.server_address[1]}/v1"
    server.shutdown()


def run_runner(request: dict, env_extra: dict, tmp_path: Path) -> tuple[int, list[dict], str]:
    env = {
        "PATH": os.environ["PATH"],
        "HOME": str(tmp_path),
        "CREWAI_DISABLE_TELEMETRY": "true",
        "OTEL_SDK_DISABLED": "true",
        "CREWAI_TRACING_ENABLED": "false",
        "NO_COLOR": "1",
        **env_extra,
    }
    proc = subprocess.run(
        [sys.executable, str(RUNNER)], input=json.dumps(request), capture_output=True, text=True, env=env, timeout=240,
        cwd=tmp_path,
    )
    events = [json.loads(line) for line in proc.stdout.splitlines() if line.strip()]
    return proc.returncode, events, proc.stderr


def test_bad_request_reports_crew_failed(tmp_path: Path):
    code, events, _ = run_runner({"nope": True}, {}, tmp_path)
    assert code == 2
    assert events[-1]["type"] == "crew_failed"


def test_full_run_streams_events_and_costs(tmp_path: Path, fake_openai: str):
    spec = make_spec()
    spec["agents"][0]["tools"] = []
    request = {"spec": spec, "grounded_searches_in_period": 0, "workspace": str(tmp_path)}
    code, events, stderr = run_runner(request, {"OPENAI_API_KEY": "sk-test", "OPENAI_BASE_URL": fake_openai}, tmp_path)
    assert code == 0, stderr[-3000:]

    types = [e["type"] for e in events]
    assert types[-1] == "crew_completed"
    for expected in ("crew_started", "task_started", "agent_started", "llm_call", "task_completed"):
        assert expected in types, types

    final = events[-1]["payload"]
    assert "REPORT about AI" in final["output"]
    assert [o["task_name"] for o in final["task_outputs"]] == ["Research", "Write"]
    assert final["task_outputs"][0]["agent_role"] == "Researcher of AI"
    assert final["usage"]["prompt_tokens"] >= 2000
    assert final["cost_usd"] > 0

    llm_calls = [e["payload"] for e in events if e["type"] == "llm_call"]
    assert llm_calls[0]["model"].endswith("gpt-6-luna")
    assert llm_calls[0]["messages"], "prompt messages should be captured"
    assert any(c["agent_role"] == "Writer" for c in llm_calls)


# A 1x1 PNG
TINY_PNG = bytes.fromhex(
    "89504e470d0a1a0a0000000d4948445200000001000000010806000000"
    "1f15c4890000000d49444154789c6300010000050001"
    "0d0a2db40000000049454e44ae426082"
)


def test_work_order_brief_files_and_transcript(tmp_path: Path, fake_openai: str):
    workspace = tmp_path / "ws"
    (workspace / "attachments").mkdir(parents=True)
    (workspace / "attachments" / "shot.png").write_bytes(TINY_PNG)
    (workspace / "attachments" / "notes.md").write_text("Customer said {urgent} twice.")
    transcript = tmp_path / "private" / "transcript.jsonl"
    transcript.parent.mkdir()

    spec = make_spec()
    spec["agents"][0]["tools"] = []
    spec["llms"][0]["vision"] = True
    spec["work_order"] = {
        "id": "w1", "number": 42, "title": "Look at the {pasted} screenshot",
        "brief": "Compare the screenshot with the notes. Use {braces} freely.",
        "attachments": [
            {"id": "f1", "filename": "shot.png", "content_type": "image/png", "size_bytes": len(TINY_PNG), "path": "attachments/shot.png"},
            {"id": "f2", "filename": "notes.md", "content_type": "text/markdown; charset=utf-8", "size_bytes": 29, "path": "attachments/notes.md"},
        ],
    }
    request = {"spec": spec, "grounded_searches_in_period": 0, "workspace": str(workspace), "transcript_path": str(transcript)}
    FakeOpenAI.prompts.clear()
    code, events, stderr = run_runner(request, {"OPENAI_API_KEY": "sk-test", "OPENAI_BASE_URL": fake_openai}, tmp_path)
    assert code == 0, stderr[-3000:]
    assert events[-1]["type"] == "crew_completed"

    sent = "\n".join(FakeOpenAI.prompts)
    assert "Work order WO-42" in sent
    assert "Customer said" in sent, "small text files are quoted in the task"
    assert "image" in sent and "base64" in sent, "images are sent to the model"

    lines = [json.loads(line) for line in transcript.read_text().splitlines()]
    assert lines[-1]["type"] == "crew_completed"
    assert any(e["type"] == "llm_call" and e["payload"]["messages"] for e in lines), "the transcript keeps full prompts"


def _work_order_request(tmp_path: Path, attachments: list[dict], vision: bool) -> dict:
    spec = make_spec()
    spec["agents"][0]["tools"] = []
    spec["llms"][0]["vision"] = vision
    spec["work_order"] = {"id": "w1", "number": 7, "title": "Review", "brief": "Please review the attachment.", "attachments": attachments}
    return {"spec": spec, "grounded_searches_in_period": 0, "workspace": str(tmp_path / "ws")}


def test_work_order_text_attachment_is_quoted_without_files(tmp_path: Path, fake_openai: str):
    ws = tmp_path / "ws"
    (ws / "attachments").mkdir(parents=True)
    (ws / "attachments" / "notes.txt").write_text("UNIQUE-NOTE-TEXT with {curly} braces")
    request = _work_order_request(
        tmp_path, [{"id": "f", "filename": "notes.txt", "content_type": "text/plain", "path": "attachments/notes.txt"}], vision=True
    )
    FakeOpenAI.prompts.clear()
    code, events, stderr = run_runner(request, {"OPENAI_API_KEY": "sk-test", "OPENAI_BASE_URL": fake_openai}, tmp_path)
    assert code == 0, stderr[-3000:]
    assert events[-1]["type"] == "crew_completed"
    sent = "\n".join(FakeOpenAI.prompts)
    assert "UNIQUE-NOTE-TEXT" in sent and "Work order WO-7" in sent
    assert "base64" not in sent, "a text file is quoted, never sent as a model file"


def test_work_order_image_for_non_vision_agent_sends_name_only(tmp_path: Path, fake_openai: str):
    ws = tmp_path / "ws"
    (ws / "attachments").mkdir(parents=True)
    (ws / "attachments" / "shot.png").write_bytes(TINY_PNG)
    request = _work_order_request(
        tmp_path, [{"id": "f", "filename": "shot.png", "content_type": "image/png", "path": "attachments/shot.png"}], vision=False
    )
    FakeOpenAI.prompts.clear()
    code, events, stderr = run_runner(request, {"OPENAI_API_KEY": "sk-test", "OPENAI_BASE_URL": fake_openai}, tmp_path)
    assert code == 0, stderr[-3000:]
    assert events[-1]["type"] == "crew_completed"
    sent = "\n".join(FakeOpenAI.prompts)
    assert "shot.png" in sent and "`attachments/shot.png`" in sent.replace("\\u0060", "`")
    assert "base64" not in sent and "image_url" not in sent, "no image data for an agent without vision"
    assert "attached for you to look at" not in sent
