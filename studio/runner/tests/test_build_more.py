from pathlib import Path

import pytest

from studio_runner import build as bd
from studio_runner.build import SpecError, build_crew, build_llm, check_spec, interpolate
from studio_runner.tools import ToolFactory
from studio_runner.workorder import WORD_JOINER

from .conftest import make_spec


@pytest.fixture(autouse=True)
def keys(monkeypatch):
    monkeypatch.setenv("OPENAI_API_KEY", "sk-test")
    monkeypatch.setenv("ANTHROPIC_API_KEY", "sk-ant-test")


def tf(tmp_path: Path) -> ToolFactory:
    return ToolFactory("gemini-3.8-flash", lambda: True, lambda *a: None, tmp_path)


class TestInterpolate:
    def test_known_unknown_hyphen_and_repeat(self):
        assert interpolate("{a}-{a} {b-c} {d}", {"a": "1", "b-c": "2"}) == "1-1 2 {d}"

    def test_no_recursive_substitution(self):
        assert interpolate("{a}", {"a": "{b}", "b": "x"}) == "{b}"

    def test_non_identifier_braces_untouched(self):
        assert interpolate('{"a": 1} { x } {1x}', {"x": "y"}) == '{"a": 1} { x } {1x}'

    def test_empty(self):
        assert interpolate("", {}) == ""


class TestCheckSpec:
    @pytest.mark.parametrize("bad", [None, "str", 3])
    def test_non_dict(self, bad):
        with pytest.raises(SpecError, match="JSON object"):
            check_spec(bad)

    def test_lists_every_missing_key(self):
        spec = make_spec()
        del spec["llms"], spec["pricing"]
        with pytest.raises(SpecError, match="llms, pricing"):
            check_spec(spec)

    def test_negative_budget(self):
        with pytest.raises(SpecError, match="more than"):
            check_spec(make_spec(budget_usd=-1))

    def test_non_numeric_budget_raises_valueerror(self):
        with pytest.raises(ValueError):
            check_spec(make_spec(budget_usd="lots"))

    def test_valid_spec(self):
        check_spec(make_spec())

    def test_spec_error_is_valueerror(self):
        assert issubclass(SpecError, ValueError)


class TestBuildLlm:
    def test_all_params(self):
        llm = build_llm({"model": "openai/gpt-6.1-sol", "temperature": 0.3, "max_completion_tokens": 77, "reasoning_effort": "low", "api": "responses"})
        assert llm.temperature == 0.3 and llm.max_completion_tokens == 77 and llm.reasoning_effort == "low" and llm.api == "responses"

    def test_minimal_params_do_not_set_options(self):
        llm = build_llm({"model": "openai/gpt-6-luna", "temperature": None, "max_completion_tokens": None, "reasoning_effort": "", "api": "completions"})
        assert llm.temperature is None and llm.max_completion_tokens is None
        assert llm.api != "responses"

    def test_zero_temperature_is_sent(self):
        assert build_llm({"model": "openai/gpt-6-luna", "temperature": 0}).temperature == 0

    def test_vision_override(self):
        base = build_llm({"model": "openai/gpt-6-luna"})
        vis = build_llm({"model": "openai/gpt-6-luna", "vision": True})
        assert base.supports_multimodal() is False
        assert vis.supports_multimodal() is True

    def test_anthropic_branch_routing(self):
        llm = build_llm({"model": "claude-opus-5-5", "max_completion_tokens": 100, "vision": True})
        assert type(llm).__name__ == "StudioAnthropicCompletion" and llm.supports_multimodal() is True


class TestBuildCrew:
    def test_defaults_and_agent_settings(self, tmp_path):
        built = build_crew(make_spec(), tf(tmp_path))
        a1 = built.crew.agents[0]
        assert a1.max_iter == 3 and a1.allow_delegation is False
        assert built.crew.name == "Test Crew"
        assert built.crew.process.value == "sequential"
        assert built.tasks[1].markdown is True and built.tasks[0].markdown is False

    def test_roles_in_task_agents_are_interpolated_but_agent_role_is_not(self, tmp_path):
        built = build_crew(make_spec(), tf(tmp_path))
        assert built.task_agents["Research"] == "Researcher of AI"
        assert "{topic}" in built.crew.agents[0].role  # CrewAI interpolates at kickoff

    def test_unknown_tool_raises(self, tmp_path):
        spec = make_spec()
        spec["agents"][0]["tools"] = ["nope"]
        with pytest.raises(ValueError, match="Unknown tool 'nope'"):
            build_crew(spec, tf(tmp_path))

    @pytest.mark.parametrize("tool", ["google_search", "scrape_website", "workspace_read", "workspace_write"])
    def test_tools_attached(self, tmp_path, tool):
        spec = make_spec()
        spec["agents"][0]["tools"] = [tool]
        assert len(build_crew(spec, tf(tmp_path)).crew.agents[0].tools) == 1

    def test_google_search_tool_attributed_to_interpolated_role(self, tmp_path):
        spec = make_spec()
        spec["agents"][0]["tools"] = ["google_search"]
        tool = build_crew(spec, tf(tmp_path)).crew.agents[0].tools[0]
        assert tool._agent_role == "Researcher of AI"

    def test_missing_agent_key_raises_keyerror(self, tmp_path):
        spec = make_spec()
        del spec["agents"][0]["max_iter"]
        with pytest.raises(KeyError):
            build_crew(spec, tf(tmp_path))

    def test_empty_tasks_error(self, tmp_path):
        with pytest.raises(SpecError):
            build_crew(make_spec(tasks=[]), tf(tmp_path))

    def test_agent_with_unknown_llm_gets_crewai_default(self, tmp_path, monkeypatch):
        monkeypatch.setenv("OPENAI_MODEL_NAME", "gpt-4o-mini")
        spec = make_spec()
        spec["agents"][1]["llm_id"] = "missing"
        built = build_crew(spec, tf(tmp_path))
        assert built.crew.agents[1].llm.model != "gpt-6-luna"

    def test_sequential_task_without_agent_is_rejected_by_crewai(self, tmp_path):
        spec = make_spec()
        spec["tasks"][1]["agent_id"] = None
        with pytest.raises(Exception, match="Agent is missing in the task"):
            build_crew(spec, tf(tmp_path))

    def test_hierarchical_task_without_agent_is_allowed_and_unattributed(self, tmp_path):
        spec = make_spec(crew={"id": "c", "name": "H", "process": "hierarchical", "manager_llm_id": "l1"})
        spec["tasks"][1]["agent_id"] = None
        built = build_crew(spec, tf(tmp_path))
        assert built.tasks[1].agent is None and "Write" not in built.task_agents

    def test_unknown_context_ids_and_forward_refs_ignored(self, tmp_path):
        spec = make_spec()
        spec["tasks"][0]["context_task_ids"] = ["t2", "ghost"]  # t2 is defined later
        spec["tasks"][1]["context_task_ids"] = ["ghost"]
        built = build_crew(spec, tf(tmp_path))
        from crewai.utilities.constants import NOT_SPECIFIED

        assert built.tasks[0].context is NOT_SPECIFIED and built.tasks[1].context is NOT_SPECIFIED

    def test_hierarchical_without_manager_llm(self, tmp_path, monkeypatch):
        spec = make_spec(crew={"id": "c", "name": "H", "process": "hierarchical", "manager_llm_id": None})
        with pytest.raises(Exception):
            # CrewAI requires a manager for hierarchical crews; the error must surface, not be swallowed
            build_crew(spec, tf(tmp_path))

    def test_sequential_ignores_manager_llm(self, tmp_path):
        spec = make_spec(crew={"id": "c", "name": "S", "process": "sequential", "manager_llm_id": "l1"})
        assert build_crew(spec, tf(tmp_path)).crew.manager_llm is None

    def test_none_inputs_are_ok(self, tmp_path):
        assert build_crew(make_spec(inputs=None), tf(tmp_path)).task_agents["Research"] == "Researcher of {topic}"

    def test_work_order_brief_only_adds_text_to_all_tasks(self, tmp_path):
        spec = make_spec()
        spec["work_order"] = {"number": 5, "title": "Hi", "brief": "Use {braces}", "attachments": []}
        built = build_crew(spec, tf(tmp_path), tmp_path)
        for t in built.tasks:
            assert "Work order WO-5: Hi" in t.description and "{" + WORD_JOINER + "braces}" in t.description
            assert not t.input_files

    def test_no_workspace_defaults_to_cwd(self, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        (tmp_path / "n.txt").write_text("inline me")
        spec = make_spec()
        spec["work_order"] = {"number": 1, "title": "T", "attachments": [{"filename": "n.txt", "content_type": "text/plain", "path": "n.txt"}]}
        assert "inline me" in build_crew(spec, tf(tmp_path)).tasks[0].description

    def test_image_for_vision_agent_becomes_input_file_and_only_for_it(self, tmp_path):
        (tmp_path / "a").mkdir()
        (tmp_path / "a" / "s.png").write_bytes(b"\x89PNG")
        spec = make_spec()
        spec["llms"].append({"id": "v", "name": "V", "model": "openai/gpt-6-luna", "vision": True})
        spec["agents"][0]["llm_id"] = "v"
        spec["work_order"] = {"number": 1, "title": "T", "attachments": [{"filename": "s.png", "content_type": "image/png", "path": "a/s.png"}]}
        spec["agents"][0]["tools"] = []
        built = build_crew(spec, tf(tmp_path), tmp_path)
        assert list(built.tasks[0].input_files) == ["s"]
        assert not built.tasks[1].input_files

    def test_model_files_empty_returns_empty_without_importing(self):
        from studio_runner.workorder import WorkOrderContext

        assert bd._model_files(WorkOrderContext()) == {}
