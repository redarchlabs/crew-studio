from pathlib import Path

import pytest

from studio_runner import workorder as wo
from studio_runner.workorder import WORD_JOINER, build_context, defuse_placeholders


def att(filename, content_type, path=None):
    return {"filename": filename, "content_type": content_type, "path": path or f"attachments/{filename}"}


@pytest.fixture
def ws(tmp_path: Path) -> Path:
    (tmp_path / "attachments").mkdir()
    return tmp_path


def order(*attachments, brief="", number=1, title="T"):
    return {"number": number, "title": title, "brief": brief, "attachments": list(attachments)}


class TestDefuse:
    @pytest.mark.parametrize("text", ["{", "{}", "{ x}", "{1}", '{"a":1}', "}{", "", "no braces", "{-x}"])
    def test_non_identifier_braces_untouched(self, text):
        assert defuse_placeholders(text) == text

    def test_multiple_and_underscore(self):
        assert defuse_placeholders("{a} {_b}") == f"{{{WORD_JOINER}a}} {{{WORD_JOINER}_b}}"

    def test_unicode_untouched(self):
        assert defuse_placeholders("héllo 😀") == "héllo 😀"

    def test_defused_text_no_longer_matches_crewai_placeholder(self):
        from studio_runner.build import PLACEHOLDER

        assert not PLACEHOLDER.search(defuse_placeholders("{topic}"))


class TestHeader:
    def test_none_and_empty_work_order(self, tmp_path):
        for value in (None, {}):
            ctx = build_context(value, tmp_path)
            assert (ctx.section, ctx.section_without_files, ctx.model_files) == ("", "", {})

    def test_brief_only(self, tmp_path):
        ctx = build_context(order(brief="  Do the thing  ", number=9, title=" Title "), tmp_path)
        assert "## Work order WO-9: Title" in ctx.section
        assert "Do the thing" in ctx.section
        assert "Files provided" not in ctx.section
        assert ctx.section == ctx.section_without_files
        assert ctx.model_files == {}

    def test_missing_fields_use_defaults(self, tmp_path):
        ctx = build_context({"attachments": None, "brief": None}, tmp_path)
        assert "WO-?:" in ctx.section
        assert "Files provided" not in ctx.section

    def test_files_only_no_brief(self, ws):
        (ws / "attachments" / "n.txt").write_text("hello")
        ctx = build_context(order(att("n.txt", "text/plain")), ws)
        assert "Files provided with this work order:" in ctx.section
        assert "```\nhello\n```" in ctx.section


class TestModelFiles:
    def test_images_and_pdfs_are_model_files_for_vision_but_paths_otherwise(self, ws):
        for name in ("a.png", "b.jpg", "c.gif", "d.webp", "e.pdf"):
            (ws / "attachments" / name).write_bytes(b"\x00")
        types = ["image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf"]
        names = ["a.png", "b.jpg", "c.gif", "d.webp", "e.pdf"]
        ctx = build_context(order(*[att(n, t) for n, t in zip(names, types)]), ws)
        assert list(ctx.model_files) == ["a", "b", "c", "d", "e"]
        assert ctx.section.count("is attached for you to look at") == 5
        assert "attached for you" not in ctx.section_without_files
        for n in names:
            assert f"`attachments/{n}`" in ctx.section_without_files
            assert f"`attachments/{n}`" not in ctx.section

    def test_input_names_are_sanitised_and_unique(self, ws):
        for n in ("My Shot!.png", "!!!.png", "My Shot!.png"):
            (ws / "attachments" / n).write_bytes(b"1")
        ctx = build_context(
            order(att("My Shot!.png", "image/png"), att("!!!.png", "image/png"), att("My Shot!.png", "image/png")), ws
        )
        assert list(ctx.model_files) == ["my_shot", "file", "my_shot_2"]

    def test_content_type_with_params_is_not_a_model_file(self, ws):
        (ws / "attachments" / "a.png").write_bytes(b"1")
        ctx = build_context(order(att("a.png", "image/png; charset=binary")), ws)
        assert ctx.model_files == {}


class TestTextFiles:
    def test_small_text_inlined_in_both_sections(self, ws):
        (ws / "attachments" / "a.json").write_text('{"k": "v"} {name}')
        ctx = build_context(order(att("a.json", "application/json")), ws)
        for section in (ctx.section, ctx.section_without_files):
            assert '{"k": "v"}' in section
            assert "{" + WORD_JOINER + "name}" in section

    @pytest.mark.parametrize("ctype", ["text/csv", "application/xml", "application/x-yaml", "text/markdown; charset=utf-8"])
    def test_text_types(self, ws, ctype):
        (ws / "attachments" / "f").write_text("body")
        ctx = build_context(order(att("f", ctype)), ws)
        assert "```\nbody\n```" in ctx.section

    def test_exact_inline_limit_is_inlined_one_over_is_referenced(self, ws):
        (ws / "attachments" / "ok.txt").write_text("x" * wo.MAX_INLINE_FILE_CHARS)
        (ws / "attachments" / "big.txt").write_text("x" * (wo.MAX_INLINE_FILE_CHARS + 1))
        ctx = build_context(order(att("ok.txt", "text/plain"), att("big.txt", "text/plain")), ws)
        assert "- ok.txt:\n```" in ctx.section
        assert "- big.txt (text/plain) is in the workspace at `attachments/big.txt`" in ctx.section

    def test_total_inline_budget_is_enforced(self, ws, monkeypatch):
        monkeypatch.setattr(wo, "MAX_INLINE_TOTAL_CHARS", 25)
        (ws / "attachments" / "a.txt").write_text("a" * 20)
        (ws / "attachments" / "b.txt").write_text("b" * 10)
        (ws / "attachments" / "c.txt").write_text("c" * 3)
        ctx = build_context(order(att("a.txt", "text/plain"), att("b.txt", "text/plain"), att("c.txt", "text/plain")), ws)
        assert "- a.txt:\n```" in ctx.section
        assert "- b.txt (text/plain) is in the workspace" in ctx.section  # 10 > 5 left
        assert "- c.txt:\n```" in ctx.section

    def test_budget_exhausted_references_remaining(self, ws, monkeypatch):
        monkeypatch.setattr(wo, "MAX_INLINE_TOTAL_CHARS", 4)
        (ws / "attachments" / "a.txt").write_text("aaaa")
        (ws / "attachments" / "b.txt").write_text("b")
        ctx = build_context(order(att("a.txt", "text/plain"), att("b.txt", "text/plain")), ws)
        assert "- b.txt (text/plain) is in the workspace" in ctx.section

    def test_empty_text_file_inlined(self, ws):
        (ws / "attachments" / "e.txt").write_text("")
        ctx = build_context(order(att("e.txt", "text/plain")), ws)
        assert "- e.txt:\n```\n\n```" in ctx.section

    def test_unicode_text(self, ws):
        (ws / "attachments" / "u.txt").write_text("héllo 😀 日本語", encoding="utf-8")
        ctx = build_context(order(att("u.txt", "text/plain")), ws)
        assert "héllo 😀 日本語" in ctx.section

    def test_binary_looking_text_is_decoded_with_replacement(self, ws):
        (ws / "attachments" / "b.txt").write_bytes(b"ok\xff\xfe\x00bad")
        ctx = build_context(order(att("b.txt", "text/plain")), ws)
        assert "ok��\x00bad" in ctx.section

    def test_unreadable_text_falls_back_to_reference(self, ws, monkeypatch):
        (ws / "attachments" / "a.txt").write_text("hi")
        monkeypatch.setattr(wo, "_read_text", lambda path, limit: None)
        ctx = build_context(order(att("a.txt", "text/plain")), ws)
        assert "is in the workspace at `attachments/a.txt`" in ctx.section

    def test_read_text_oserror_returns_none(self, tmp_path):
        assert wo._read_text(tmp_path / "missing", 10) is None

    def test_unknown_type_is_referenced(self, ws):
        (ws / "attachments" / "x.bin").write_bytes(b"\x00")
        ctx = build_context(order(att("x.bin", "application/octet-stream")), ws)
        assert "is in the workspace at `attachments/x.bin`" in ctx.section
        assert ctx.model_files == {}


class TestMissing:
    def test_missing_file(self, ws):
        ctx = build_context(order(att("gone.txt", "text/plain")), ws)
        assert "- gone.txt: could not be loaded." in ctx.section
        assert "- gone.txt: could not be loaded." in ctx.section_without_files

    def test_directory_is_not_a_file(self, ws):
        (ws / "attachments" / "d").mkdir()
        ctx = build_context(order(att("d", "text/plain")), ws)
        assert "could not be loaded" in ctx.section

    def test_path_escaping_workspace(self, ws):
        outside = ws.parent / "outside.txt"
        outside.write_text("secret")
        ctx = build_context(order(att("o.txt", "text/plain", "../outside.txt")), ws)
        assert "could not be loaded" in ctx.section and "secret" not in ctx.section

    def test_symlink_out_of_workspace(self, ws):
        outside = ws.parent / "outside2.txt"
        outside.write_text("secret")
        (ws / "attachments" / "l.txt").symlink_to(outside)
        ctx = build_context(order(att("l.txt", "text/plain")), ws)
        assert "could not be loaded" in ctx.section and "secret" not in ctx.section

    def test_defaults_when_attachment_fields_absent(self, ws):
        ctx = build_context(order({}), ws)
        assert "- file: could not be loaded." in ctx.section
