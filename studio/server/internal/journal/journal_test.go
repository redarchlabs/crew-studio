package journal

import (
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
)

func TestBuild(t *testing.T) {
	agentID := "a1"
	spec := model.RunSpec{
		Agents: []model.Agent{{ID: agentID, Role: "{company} Analyst"}},
		Tasks:  []model.Task{{Name: "Research", AgentID: &agentID}, {Name: "Write"}},
		Inputs: map[string]string{"company": "Acme"},
	}
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	ev := func(seq int, typ string, p map[string]any) model.RunEvent {
		return model.RunEvent{Seq: seq, Type: typ, Payload: p, CreatedAt: at.Add(time.Duration(seq) * time.Second)}
	}
	events := []model.RunEvent{
		ev(1, "run_status", map[string]any{"status": "running"}),
		ev(2, "task_started", map[string]any{"task_name": "Research", "agent_role": "Acme Analyst"}),
		ev(3, "llm_call", map[string]any{"task_name": "Research", "agent_role": "Acme Analyst", "model": "openai/gpt-6-luna",
			"cost_usd": 0.01, "usage": map[string]any{"prompt_tokens": 100.0, "completion_tokens": 20.0}}),
		ev(4, "tool_started", map[string]any{"task_name": "Research", "agent_role": "Acme Analyst", "tool_name": "Google Search",
			"tool_args": map[string]any{"query": "acme"}}),
		ev(5, "search", map[string]any{"task_name": "Research", "agent_role": "Acme Analyst", "query": "acme  revenue\n2026", "cost_usd": 0.014}),
		ev(6, "log", map[string]any{"line": "noise"}),
		ev(7, "delegation", map[string]any{"from_agent": "Acme Analyst", "to_agent": "Writer", "question": "draft"}),
		ev(8, "task_completed", map[string]any{"task_name": "Research", "agent_role": "Acme Analyst", "output": strings.Repeat("x", 5000)}),
		ev(9, "budget", map[string]any{"state": "searches_stopped", "spent_usd": 1.0, "budget_usd": 1.0}),
		ev(10, "artifacts", map[string]any{"files": 2.0}),
		ev(11, "video", map[string]any{"task_name": "Research", "agent_role": "Acme Analyst", "model": "gemini-omni-1.1-flash",
			"file": "videos/intro.mp4", "prompt": "a logo reveal", "made": true, "input_tokens": 20.0, "output_tokens": 46336.0, "cost_usd": 0.81}),
	}
	j := Build(model.Run{ID: "r", Status: model.RunRunning}, spec, events, map[int]string{3: "Here is what I found"})
	if len(j.Entries) != 10 || j.Truncated {
		t.Fatalf("entries = %d", len(j.Entries))
	}
	byKind := map[string]Entry{}
	for _, e := range j.Entries {
		byKind[e.Kind] = e
	}
	if e := byKind["llm_call"]; e.Body != "Here is what I found" || e.Tokens != 120 || e.Title != "Asked openai/gpt-6-luna" {
		t.Errorf("llm entry = %+v", e)
	}
	if e := byKind["search"]; e.Title != "Searched: acme revenue 2026" {
		t.Errorf("search title = %q", e.Title)
	}
	if e := byKind["video"]; e.Title != "Made video videos/intro.mp4" || e.Body != "a logo reveal" || e.Tokens != 46356 || e.CostUSD != 0.81 {
		t.Errorf("video entry = %+v", e)
	}
	if e := byKind["task_completed"]; !strings.Contains(e.Body, "[cut;") || len(e.Body) > maxBodyLen+100 {
		t.Errorf("long output not cut: %d", len(e.Body))
	}
	if !strings.Contains(byKind["budget"].Title, "searches stopped") || byKind["artifacts"].Title != "Saved 2 files" {
		t.Errorf("budget/artifacts = %q / %q", byKind["budget"].Title, byKind["artifacts"].Title)
	}
	a := j.Agents[0]
	if a.Role != "Acme Analyst" || a.LLMCalls != 1 || a.ToolCalls != 1 || a.PromptTokens != 100 || a.CostUSD < 0.8339 || a.Entries != 7 {
		t.Errorf("agent = %+v", a)
	}
	if len(j.Agents) != 1 {
		t.Errorf("agents = %+v", j.Agents)
	}
	if j.Tasks[0].Status != TaskDone || j.Tasks[0].Output == nil || j.Tasks[1].Status != TaskPending || j.Tasks[1].Name != "Write" {
		t.Errorf("tasks = %+v", j.Tasks)
	}
}

func TestBlockedVideoIsTitledSo(t *testing.T) {
	events := []model.RunEvent{{Seq: 1, Type: "video", Payload: map[string]any{"agent_role": "A", "file": "v.mp4", "made": false}}}
	if e := Build(model.Run{}, model.RunSpec{}, events, nil).Entries[0]; e.Title != "Video blocked: v.mp4" || e.Kind != "video" {
		t.Errorf("entry = %+v", e)
	}
}

func TestBuildCapsEntries(t *testing.T) {
	events := make([]model.RunEvent, MaxEntries+10)
	for i := range events {
		events[i] = model.RunEvent{Seq: i + 1, Type: "tool_started", Payload: map[string]any{"agent_role": "A", "tool_name": "t"}}
	}
	j := Build(model.Run{}, model.RunSpec{}, events, nil)
	if len(j.Entries) != MaxEntries || !j.Truncated || j.Agents[0].ToolCalls != MaxEntries+10 {
		t.Errorf("entries %d truncated %v calls %d", len(j.Entries), j.Truncated, j.Agents[0].ToolCalls)
	}
}

// A specialist's question to the lead shows as a line under the specialist, and its outcome as a line under the lead.
func TestConsultationLines(t *testing.T) {
	cases := []struct {
		outcome, role, title, body string
	}{
		{"asked", "Market analyst", "Market analyst asked the lead: List or negotiated prices?", "List or negotiated prices?"},
		{"answered", "Research lead", "Research lead answered Market analyst", "Use list prices."},
		{"needs_person", "Research lead", "Research lead passed Market analyst's question to the person", "Use list prices."},
		{"declined", "Research lead", "Research lead could not be asked again", "List or negotiated prices?"},
		{"stopped", "Research lead", "Research lead stopped: nobody answered in time", "Use list prices."},
		{"failed", "Research lead", "Research lead could not answer Market analyst", "Use list prices."},
	}
	for _, tc := range cases {
		events := []model.RunEvent{{Seq: 1, Type: "consultation", Payload: map[string]any{"from_agent": "Market analyst", "to_agent": "Research lead",
			"question": "List or negotiated prices?", "answer": "Use list prices.", "outcome": tc.outcome}}}
		e := Build(model.Run{}, model.RunSpec{}, events, nil).Entries[0]
		if e.Kind != "consultation" || e.AgentRole != tc.role || e.Title != tc.title || e.Body != tc.body {
			t.Errorf("%s: entry = %+v", tc.outcome, e)
		}
	}
}
