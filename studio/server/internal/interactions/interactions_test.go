package interactions

import (
	"testing"
	"time"

	"crewstudio/internal/model"
)

func ptr[T any](v T) *T { return &v }

func spec() model.RunSpec {
	return model.RunSpec{
		Crew: model.Crew{Name: "Research Crew", Process: model.ProcessSequential},
		Agents: []model.Agent{
			{ID: "a1", Role: "Researcher of {topic}"},
			{ID: "a2", Role: "Writer"},
		},
		Tasks: []model.Task{
			{ID: "t1", Name: "Research", AgentID: ptr("a1")},
			{ID: "t2", Name: "Write", AgentID: ptr("a2"), ContextTaskIDs: []string{"t1"}},
		},
		Inputs: map[string]string{"topic": "AI"},
	}
}

var t0 = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func ev(seq int, typ string, offset time.Duration, payload map[string]any) model.RunEvent {
	return model.RunEvent{Seq: seq, Type: typ, Payload: payload, CreatedAt: t0.Add(offset)}
}

func events() []model.RunEvent {
	return []model.RunEvent{
		ev(1, "task_started", 0, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI"}),
		ev(2, "agent_started", 0, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI"}),
		ev(3, "llm_call", time.Second, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI", "model": "openai/gpt-6-luna",
			"usage": map[string]any{"prompt_tokens": 100.0, "completion_tokens": 20.0}, "cost_usd": 0.01}),
		ev(4, "tool_started", 2*time.Second, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI", "tool_name": "Google Search"}),
		ev(5, "search", 3*time.Second, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI", "tool_name": "Google Search", "cost_usd": 0.002}),
		ev(6, "llm_call", 4*time.Second, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI", "model": "openai/gpt-6-luna",
			"usage": map[string]any{"prompt_tokens": 50.0, "completion_tokens": 10.0}, "cost_usd": 0.005}),
		ev(7, "task_completed", 5*time.Second, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI"}),
		ev(8, "task_started", 5*time.Second, map[string]any{"task_name": "Write", "agent_role": "Writer"}),
		ev(9, "delegation", 6*time.Second, map[string]any{"from_agent": "Writer", "to_agent": "Researcher of AI", "task_name": "Write"}),
		ev(10, "task_failed", 7*time.Second, map[string]any{"task_name": "Write", "agent_role": "Writer", "error": "boom"}),
	}
}

func findNode(t *testing.T, g Graph, id string) Node {
	t.Helper()
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not found in %+v", id, g.Nodes)
	return Node{}
}

func findEdge(t *testing.T, g Graph, source, target, kind string) Edge {
	t.Helper()
	for _, e := range g.Edges {
		if e.Source == source && e.Target == target && e.Kind == kind {
			return e
		}
	}
	t.Fatalf("edge %s -%s-> %s not found in %+v", source, kind, target, g.Edges)
	return Edge{}
}

func TestBuildStructureFromSpec(t *testing.T) {
	g := Build(spec(), nil)

	findNode(t, g, "crew")
	if n := findNode(t, g, "task:Research"); n.Status != StatusPending {
		t.Errorf("task status = %q, want pending", n.Status)
	}
	findNode(t, g, "agent:Researcher of AI") // role interpolated with run inputs
	findEdge(t, g, "crew", "task:Research", KindRuns)
	findEdge(t, g, "task:Research", "agent:Researcher of AI", KindAssigned)
	findEdge(t, g, "task:Research", "task:Write", KindContext)
}

func TestBuildAggregatesEvents(t *testing.T) {
	g := Build(spec(), events())

	research := findNode(t, g, "task:Research")
	if research.Status != StatusDone {
		t.Errorf("Research status = %q", research.Status)
	}
	if research.Stats.DurationMS != 5000 {
		t.Errorf("Research duration = %d", research.Stats.DurationMS)
	}
	if research.Stats.LLMCalls != 2 || research.Stats.PromptTokens != 150 || research.Stats.CompletionTokens != 30 {
		t.Errorf("Research stats = %+v", research.Stats)
	}

	if write := findNode(t, g, "task:Write"); write.Status != StatusFailed {
		t.Errorf("Write status = %q", write.Status)
	}

	agent := findNode(t, g, "agent:Researcher of AI")
	if agent.Stats.ToolCalls != 1 || agent.Stats.LLMCalls != 2 {
		t.Errorf("agent stats = %+v", agent.Stats)
	}
	if got, want := agent.Stats.CostUSD, 0.017; !closeTo(got, want) {
		t.Errorf("agent cost = %v, want %v", got, want)
	}

	llmEdge := findEdge(t, g, "agent:Researcher of AI", "model:openai/gpt-6-luna", KindLLMCall)
	if llmEdge.Count != 2 || !closeTo(llmEdge.CostUSD, 0.015) {
		t.Errorf("llm edge = %+v", llmEdge)
	}
	toolEdge := findEdge(t, g, "agent:Researcher of AI", "tool:Google Search", KindToolCall)
	if toolEdge.Count != 1 || !closeTo(toolEdge.CostUSD, 0.002) {
		t.Errorf("tool edge = %+v", toolEdge)
	}
	findEdge(t, g, "agent:Writer", "agent:Researcher of AI", KindDelegation)

	crew := findNode(t, g, "crew")
	if crew.Stats.LLMCalls != 2 || !closeTo(crew.Stats.CostUSD, 0.017) {
		t.Errorf("crew stats = %+v", crew.Stats)
	}
}

func TestBuildAddsUnknownAgentsAndTasks(t *testing.T) {
	g := Build(spec(), []model.RunEvent{
		ev(1, "agent_started", 0, map[string]any{"agent_role": "Crew Manager", "task_name": "Research"}),
		ev(2, "llm_call", 0, map[string]any{"agent_role": "Crew Manager", "task_name": "Ad hoc", "model": "m", "cost_usd": 0.0}),
	})
	findNode(t, g, "agent:Crew Manager")
	findEdge(t, g, "task:Research", "agent:Crew Manager", KindAssigned)
	findNode(t, g, "task:Ad hoc")
}

func TestBuildRunningTaskStatus(t *testing.T) {
	g := Build(spec(), events()[:3])
	if n := findNode(t, g, "task:Research"); n.Status != StatusRunning {
		t.Errorf("status = %q, want running", n.Status)
	}
	if n := findNode(t, g, "agent:Researcher of AI"); n.Status != StatusRunning {
		t.Errorf("agent status = %q, want running", n.Status)
	}
}

func closeTo(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}

func TestBuildSettlesOnTerminalRunStatus(t *testing.T) {
	g := Build(spec(), []model.RunEvent{
		ev(1, "task_started", 0, map[string]any{"task_name": "Research"}),
		ev(2, "agent_started", 0, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI"}),
		ev(3, "run_status", time.Second, map[string]any{"status": "cancelled"}),
	})
	if n := findNode(t, g, "task:Research"); n.Status != StatusFailed {
		t.Errorf("task still %q after cancel", n.Status)
	}
}

func TestBuildUsesStartingAgentWhenCompletionHasNoRole(t *testing.T) {
	g := Build(spec(), []model.RunEvent{
		ev(1, "task_started", 0, map[string]any{"task_name": "Research"}),
		ev(2, "agent_started", 0, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI"}),
		ev(3, "task_completed", time.Second, map[string]any{"task_name": "Research"}),
		ev(4, "task_started", time.Second, map[string]any{"task_name": "Write"}),
		ev(5, "agent_started", time.Second, map[string]any{"task_name": "Write", "agent_role": "Writer"}),
		ev(6, "crew_failed", 2*time.Second, map[string]any{"error": "x"}),
	})
	if n := findNode(t, g, "agent:Researcher of AI"); n.Status != StatusDone || n.Stats.DurationMS != 1000 {
		t.Errorf("researcher = %+v", n)
	}
	if n := findNode(t, g, "agent:Writer"); n.Status != StatusFailed {
		t.Errorf("writer still %q after crew failed", n.Status)
	}
	if n := findNode(t, g, "task:Write"); n.Status != StatusFailed {
		t.Errorf("write task still %q after crew failed", n.Status)
	}
}

func TestVideoCostLandsOnTheToolAgentAndTask(t *testing.T) {
	g := Build(spec(), []model.RunEvent{
		ev(1, "task_started", 0, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI"}),
		ev(2, "video", time.Second, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI",
			"tool_name": "Generate Video", "cost_usd": 0.81}),
	})
	tool := findNode(t, g, "tool:Generate Video")
	if tool.Stats.CostUSD != 0.81 {
		t.Errorf("tool cost = %v", tool.Stats.CostUSD)
	}
	for _, n := range g.Nodes {
		if (n.ID == "agent:Researcher of AI" || n.Kind == "crew") && n.Stats.CostUSD != 0.81 {
			t.Errorf("%s %s cost = %v, want 0.81", n.Kind, n.ID, n.Stats.CostUSD)
		}
	}
	findEdge(t, g, "agent:Researcher of AI", "tool:Generate Video", KindToolCall)
}

func TestToolNamesMergeAcrossSpellings(t *testing.T) {
	// CrewAI reports the sanitized tool name; the runner's cost event uses the display name
	g := Build(spec(), []model.RunEvent{
		ev(1, "tool_started", 0, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI", "tool_name": "google_search"}),
		ev(2, "search", time.Second, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI", "tool_name": "Google Search", "cost_usd": 0.01}),
		ev(3, "tool_started", 2*time.Second, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI", "tool_name": "google_search"}),
		ev(4, "search", 3*time.Second, map[string]any{"task_name": "Research", "agent_role": "Researcher of AI", "tool_name": "Google Search", "cost_usd": 0.02}),
	})
	var tools []Node
	for _, n := range g.Nodes {
		if n.Kind == "tool" {
			tools = append(tools, n)
		}
	}
	if len(tools) != 1 {
		t.Fatalf("got %d tool nodes, want 1: %+v", len(tools), tools)
	}
	tool := tools[0]
	if tool.Label != "Google Search" || tool.Stats.ToolCalls != 2 || tool.Stats.CostUSD < 0.0299 || tool.Stats.CostUSD > 0.0301 {
		t.Errorf("tool = %+v", tool)
	}
	e := findEdge(t, g, "agent:Researcher of AI", tool.ID, KindToolCall)
	if e.Count != 2 || e.CostUSD < 0.0299 {
		t.Errorf("edge = %+v", e)
	}
}

func TestToolKey(t *testing.T) {
	for in, want := range map[string]string{"Google Search": "google_search", "google_search": "google_search", " Read  Workspace-File ": "read_workspace_file"} {
		if got := toolKey(in); got != want {
			t.Errorf("toolKey(%q) = %q, want %q", in, got, want)
		}
	}
}
