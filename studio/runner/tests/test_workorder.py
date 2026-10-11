import io
import json
from pathlib import Path

from studio_runner.emitter import MAX_TOOL_OUTPUT_CHARS, Emitter, cap_strings
from studio_runner.workorder import WORD_JOINER, build_context, defuse_placeholders


def test_defuse_placeholders_leaves_json_alone():
    assert defuse_placeholders("Hi {name}") == "Hi {" + WORD_JOINER + "name}"
    assert defuse_placeholders('{"a": 1} and { spaced }') == '{"a": 1} and { spaced }'


def test_build_context_sorts_files(tmp_path: Path):
    (tmp_path / "attachments").mkdir()
    (tmp_path / "attachments" / "a.png").write_bytes(b"\x89PNG")
    (tmp_path / "attachments" / "b.txt").write_text("small {text}")
    (tmp_path / "attachments" / "c.docx").write_bytes(b"PK")
    (tmp_path / "attachments" / "big.txt").write_text("x" * 30_000)
    wo = {
        "number": 7, "title": "T", "brief": "Do {it}",
        "attachments": [
            {"filename": "a.png", "content_type": "image/png", "path": "attachments/a.png"},
            {"filename": "a.png", "content_type": "image/png", "path": "attachments/a.png"},
            {"filename": "b.txt", "content_type": "text/plain", "path": "attachments/b.txt"},
            {"filename": "c.docx", "content_type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
             "path": "attachments/c.docx"},
            {"filename": "big.txt", "content_type": "text/plain", "path": "attachments/big.txt"},
            {"filename": "escape", "content_type": "text/plain", "path": "../../etc/passwd"},
        ],
    }
    ctx = build_context(wo, tmp_path)
    assert list(ctx.model_files) == ["a", "a_2"]
    assert "Work order WO-7: T" in ctx.section
    assert "{" + WORD_JOINER + "it}" in ctx.section and "{it}" not in ctx.section
    assert "small {" + WORD_JOINER + "text}" in ctx.section
    assert "c.docx" in ctx.section and "`attachments/c.docx`" in ctx.section
    assert "`attachments/big.txt`" in ctx.section, "large text files are referenced, not quoted"
    assert "escape: could not be loaded" in ctx.section


def test_no_work_order_adds_nothing(tmp_path: Path):
    ctx = build_context(None, tmp_path)
    assert ctx.section == "" and ctx.model_files == {}


def test_emitter_keeps_full_detail_in_transcript():
    stream, transcript = io.StringIO(), io.StringIO()
    emitter = Emitter(stream, transcript)
    long = "y" * (MAX_TOOL_OUTPUT_CHARS + 100)
    emitter.emit("tool_finished", {"output": long})
    emitter.emit("crew_completed", {"output": long})
    live = [json.loads(line) for line in stream.getvalue().splitlines()]
    full = [json.loads(line) for line in transcript.getvalue().splitlines()]
    assert len(live[0]["payload"]["output"]) < len(long)
    assert full[0]["payload"]["output"] == long
    assert live[1]["payload"]["output"] == long, "final results are never cut"


def test_cap_strings_is_recursive():
    assert cap_strings({"a": ["xxxx", {"b": "yyyy"}], "n": 3}, 2)["a"][1]["b"].startswith("yy\n")


def test_agents_without_vision_get_paths_not_files(tmp_path: Path):
    from studio_runner.build import build_crew
    from studio_runner.tools import ToolFactory

    from .conftest import make_spec

    (tmp_path / "attachments").mkdir()
    (tmp_path / "attachments" / "shot.png").write_bytes(b"\x89PNG")
    spec = make_spec()
    spec["agents"][0]["tools"] = []
    spec["llms"].append({"id": "l2", "name": "Vision", "model": "openai/gpt-6-luna", "vision": True})
    spec["agents"][1]["llm_id"] = "l2"
    spec["work_order"] = {"number": 1, "title": "T", "brief": "",
                          "attachments": [{"filename": "shot.png", "content_type": "image/png", "path": "attachments/shot.png"}]}
    tools = ToolFactory(search_model="m", searches_allowed=lambda: True, record_search=lambda *a: None, workspace=tmp_path)
    built = build_crew(spec, tools, tmp_path)
    research, write = built.tasks
    assert not research.input_files and "`attachments/shot.png`" in research.description
    assert write.input_files and "attached for you to look at" in write.description
