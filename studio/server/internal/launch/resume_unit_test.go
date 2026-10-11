package launch

import (
	"strings"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
)

func TestFrozenProblems(t *testing.T) {
	llm, manager := "l1", "l2"
	lib := runspec.Library{
		LLMs:  map[string]model.LLM{llm: {ID: llm}},
		Tools: map[string]model.Tool{"google_search": {Key: "google_search", Available: true}, "video": {Key: "video", Available: false}},
	}
	ok := model.RunSpec{Crew: model.Crew{Process: model.ProcessSequential},
		Agents: []model.Agent{{Name: "A", LLMID: &llm, Tools: []string{"google_search"}}}}
	if got := frozenProblems(ok, lib); len(got) != 0 {
		t.Fatalf("problems = %v", got)
	}
	gone := "deleted-llm"
	broken := model.RunSpec{Crew: model.Crew{Process: model.ProcessHierarchical, ManagerLLMID: &manager},
		Agents: []model.Agent{{Name: "A", LLMID: &gone, Tools: []string{"video", "removed"}}, {Name: "B"}}}
	got := strings.Join(frozenProblems(broken, lib), "\n")
	for _, want := range []string{"The manager's LLM was deleted", `Agent "A"'s LLM was deleted`, `tool "video"`, `tool "removed"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"B"`) {
		t.Error("an agent without an LLM or tools has nothing taken away")
	}
}

func TestSamePrefix(t *testing.T) {
	agent, other, llm, llm2 := "a1", "a2", "l1", "l2"
	base := model.RunSpec{
		Tasks: []model.Task{
			{ID: "t1", Name: "Research", Description: "d", ExpectedOutput: "e", AgentID: &agent, ContextTaskIDs: []string{}},
			{ID: "t2", Name: "Write"},
		},
		Agents: []model.Agent{{ID: agent, Role: "R", Goal: "G", Backstory: "B", LLMID: &llm, Tools: []string{"google_search"}, MaxIter: 5}},
	}
	clone := func() model.RunSpec {
		now := base
		now.Tasks = append([]model.Task(nil), base.Tasks...)
		now.Agents = append([]model.Agent(nil), base.Agents...)
		return now
	}
	task := func(f func(*model.Task)) model.RunSpec { now := clone(); f(&now.Tasks[0]); return now }
	agentEdit := func(f func(*model.Agent)) model.RunSpec { now := clone(); f(&now.Agents[0]); return now }
	cases := []struct {
		name string
		now  model.RunSpec
		want bool
	}{
		{"unchanged", base, true},
		{"the agent's LLM may change", agentEdit(func(a *model.Agent) { a.LLMID = &llm2 }), true},
		{"description", task(func(t *model.Task) { t.Description = "new" }), false},
		{"expected output", task(func(t *model.Task) { t.ExpectedOutput = "new" }), false},
		{"name", task(func(t *model.Task) { t.Name = "new" }), false},
		{"agent", task(func(t *model.Task) { t.AgentID = &other }), false},
		{"agent removed", task(func(t *model.Task) { t.AgentID = nil }), false},
		{"context", task(func(t *model.Task) { t.ContextTaskIDs = []string{"t0"} }), false},
		{"another task", task(func(t *model.Task) { t.ID = "t9" }), false},
		{"agent goal", agentEdit(func(a *model.Agent) { a.Goal = "new" }), false},
		{"agent backstory", agentEdit(func(a *model.Agent) { a.Backstory = "new" }), false},
		{"agent role", agentEdit(func(a *model.Agent) { a.Role = "new" }), false},
		{"agent tools", agentEdit(func(a *model.Agent) { a.Tools = nil }), false},
		{"agent max iterations", agentEdit(func(a *model.Agent) { a.MaxIter = 9 }), false},
		{"agent delegation", agentEdit(func(a *model.Agent) { a.AllowDelegation = true }), false},
		{"agent missing now", func() model.RunSpec { now := clone(); now.Agents = nil; return now }(), false},
		{"nothing left to run", func() model.RunSpec { now := clone(); now.Tasks = now.Tasks[:1]; return now }(), false},
	}
	for _, c := range cases {
		if got := samePrefix(base, c.now, 1); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
	short := clone()
	short.Tasks = short.Tasks[:1]
	if samePrefix(short, base, 2) {
		t.Error("the earlier run had fewer tasks than the prefix")
	}
	if !sameID(nil, nil) || sameID(&agent, nil) || sameID(&agent, &other) {
		t.Error("sameID")
	}
	if !sameAgent(nil, nil) {
		t.Error("two missing agents are the same")
	}
}

func TestFinishedChangedWording(t *testing.T) {
	if got := finishedChanged(1); got != "The crew's first task (or its agent) was changed or moved" {
		t.Error(got)
	}
	if got := finishedChanged(3); got != "The crew's first 3 tasks (or their agents) were changed or reordered" {
		t.Error(got)
	}
}

func TestRequireAny(t *testing.T) {
	p := &auth.Principal{Permissions: map[string]bool{auth.WorkOrdersCreate: true}}
	if requireAny(p, auth.CrewsRun, auth.WorkOrdersCreate) != nil {
		t.Error("one of them is enough")
	}
	if requireAny(&auth.Principal{}, auth.CrewsRun, auth.WorkOrdersCreate) == nil {
		t.Error("none is refused")
	}
}
