package runspec

import (
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/model"
)

func ptr[T any](v T) *T { return &v }

func fixture() Library {
	llm := model.LLM{ID: "l1", Name: "Luna", Model: "openai/gpt-6-luna"}
	return Library{
		LLMs: map[string]model.LLM{"l1": llm},
		Agents: map[string]model.Agent{
			"a1": {ID: "a1", Name: "Researcher", Role: "Researcher of {topic}", Goal: "g", Backstory: "b", LLMID: ptr("l1"), Tools: []string{"google_search"}},
			"a2": {ID: "a2", Name: "Writer", Role: "Writer", Goal: "g", Backstory: "b", LLMID: ptr("l1")},
		},
		Tasks: map[string]model.Task{
			"t1": {ID: "t1", Name: "Research", Description: "Look into {topic}", ExpectedOutput: "notes", AgentID: ptr("a1")},
			"t2": {ID: "t2", Name: "Write", Description: "Write it up", ExpectedOutput: "report", AgentID: ptr("a2"), ContextTaskIDs: []string{"t1"}},
		},
		Tools: map[string]model.Tool{
			"google_search": {Key: "google_search", RequiresEnv: []string{"GEMINI_API_KEY"}, Available: true},
		},
		Pricing: []model.ModelPrice{
			{Model: "gpt-6-luna", InputPerMTok: 0.1},
			{Model: "gemini-3.8-flash", InputPerMTok: 0.3},
		},
		Settings: model.Settings{SearchModel: "gemini-3.8-flash", DefaultBudgetUSD: 2, HardStopMultiplier: 1.25},
	}
}

func crewFixture() model.Crew {
	return model.Crew{
		ID: "c1", Name: "Crew", Process: model.ProcessSequential, BudgetUSD: 2,
		AgentIDs: []string{"a1", "a2"}, TaskIDs: []string{"t1", "t2"},
		Inputs: []model.CrewInput{{Key: "topic", Label: "Topic", Required: true}},
	}
}

func containsProblem(problems []string, fragment string) bool {
	return slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, fragment) })
}

func TestCheckValidCrew(t *testing.T) {
	if problems := Check(crewFixture(), fixture()); len(problems) != 0 {
		t.Fatalf("valid crew reported problems: %v", problems)
	}
}

func TestCheckProblems(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c model.Crew, lib Library) (model.Crew, Library)
		want   string
	}{
		{"no tasks", func(c model.Crew, l Library) (model.Crew, Library) { c.TaskIDs = nil; return c, l }, "has no tasks"},
		{"no agents", func(c model.Crew, l Library) (model.Crew, Library) { c.AgentIDs = nil; return c, l }, "has no agents"},
		{"agent not in crew", func(c model.Crew, l Library) (model.Crew, Library) {
			c.AgentIDs = []string{"a1"}
			return c, l
		}, `"Writer", which is not in the crew`},
		{"context after task", func(c model.Crew, l Library) (model.Crew, Library) {
			c.TaskIDs = []string{"t2", "t1"}
			return c, l
		}, "must come earlier"},
		{"missing input", func(c model.Crew, l Library) (model.Crew, Library) { c.Inputs = nil; return c, l }, "{topic}"},
		{"tool unavailable", func(c model.Crew, l Library) (model.Crew, Library) {
			l.Tools = map[string]model.Tool{"google_search": {Key: "google_search", RequiresEnv: []string{"GEMINI_API_KEY"}}}
			return c, l
		}, "GEMINI_API_KEY"},
		{"no price for model", func(c model.Crew, l Library) (model.Crew, Library) {
			l.Pricing = []model.ModelPrice{{Model: "gemini-3.8-flash"}}
			return c, l
		}, `no price for model "openai/gpt-6-luna"`},
		{"no price for search model", func(c model.Crew, l Library) (model.Crew, Library) {
			l.Pricing = []model.ModelPrice{{Model: "gpt-6-luna"}}
			return c, l
		}, `search model "gemini-3.8-flash"`},
		{"no price for a tool's model", func(c model.Crew, l Library) (model.Crew, Library) {
			a := l.Agents["a2"]
			a.Tools = []string{"generate_video"}
			l.Agents = map[string]model.Agent{"a1": l.Agents["a1"], "a2": a}
			l.Tools = map[string]model.Tool{
				"google_search":  l.Tools["google_search"],
				"generate_video": {Key: "generate_video", PricedModel: "gemini-omni-1.1-flash", Available: true},
			}
			return c, l
		}, `model "gemini-omni-1.1-flash" that a tool bills for`},
		{"agent without llm", func(c model.Crew, l Library) (model.Crew, Library) {
			a := l.Agents["a2"]
			a.LLMID = nil
			l.Agents = map[string]model.Agent{"a1": l.Agents["a1"], "a2": a}
			return c, l
		}, "has no LLM"},
		{"task without agent", func(c model.Crew, l Library) (model.Crew, Library) {
			task := l.Tasks["t2"]
			task.AgentID = nil
			l.Tasks = map[string]model.Task{"t1": l.Tasks["t1"], "t2": task}
			return c, l
		}, "has no agent"},
		{"hierarchical without manager", func(c model.Crew, l Library) (model.Crew, Library) {
			c.Process = model.ProcessHierarchical
			return c, l
		}, "manager LLM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crew, lib := tt.mutate(crewFixture(), fixture())
			problems := Check(crew, lib)
			if !containsProblem(problems, tt.want) {
				t.Errorf("want problem containing %q, got %v", tt.want, problems)
			}
		})
	}
}

func TestToolModelWithPriceIsFine(t *testing.T) {
	crew, lib := crewFixture(), fixture()
	a := lib.Agents["a2"]
	a.Tools = []string{"generate_video"}
	lib.Agents["a2"] = a
	lib.Tools["generate_video"] = model.Tool{Key: "generate_video", PricedModel: "gemini-omni-1.1-flash", Available: true}
	lib.Pricing = append(lib.Pricing, model.ModelPrice{Model: "gemini-omni-1.1-flash", OutputPerMTok: 17.5})
	if problems := Check(crew, lib); len(problems) != 0 {
		t.Errorf("priced tool model reported problems: %v", problems)
	}
}

func TestHierarchicalAllowsTaskWithoutAgent(t *testing.T) {
	lib := fixture()
	task := lib.Tasks["t2"]
	task.AgentID = nil
	lib.Tasks = map[string]model.Task{"t1": lib.Tasks["t1"], "t2": task}
	crew := crewFixture()
	crew.Process = model.ProcessHierarchical
	crew.ManagerLLMID = ptr("l1")
	if problems := Check(crew, lib); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
}

func TestHierarchicalRejectsForwardContext(t *testing.T) {
	crew := crewFixture()
	crew.Process = model.ProcessHierarchical
	crew.ManagerLLMID = ptr("l1")
	crew.TaskIDs = []string{"t2", "t1"}
	if problems := Check(crew, fixture()); !containsProblem(problems, "must come earlier") {
		t.Errorf("problems = %v", problems)
	}
}

func TestResolveInputs(t *testing.T) {
	crew := model.Crew{Inputs: []model.CrewInput{
		{Key: "company", Required: true},
		{Key: "location", Default: "Ohio"},
	}}

	got, problems := ResolveInputs(crew, map[string]string{"company": "  Acme  "})
	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if got["company"] != "Acme" || got["location"] != "Ohio" {
		t.Errorf("got %v", got)
	}

	_, problems = ResolveInputs(crew, map[string]string{"company": " ", "rogue": "x"})
	if !containsProblem(problems, "company") || !containsProblem(problems, "rogue") {
		t.Errorf("want missing + unknown problems, got %v", problems)
	}
}

func TestResolveInputsRejectsHugeValues(t *testing.T) {
	crew := model.Crew{Inputs: []model.CrewInput{{Key: "notes"}}}
	_, problems := ResolveInputs(crew, map[string]string{"notes": strings.Repeat("x", MaxInputValueLen+1)})
	if !containsProblem(problems, "notes") {
		t.Errorf("want too-long problem, got %v", problems)
	}
}

func TestResolveInputsAcceptsWatchInputs(t *testing.T) {
	crew := model.Crew{Inputs: []model.CrewInput{{Key: "company", Default: "Acme"}, {Key: "changes", Default: "none"}}}
	long := strings.Repeat("x", MaxInputValueLen+1)
	got, problems := ResolveWatchedInputs(crew, map[string]string{"changes": long, "followups_due": " {} ", "dry_run": "true"})
	if len(problems) != 0 || got["changes"] != long || got["followups_due"] != "{}" || got["dry_run"] != "true" || got["company"] != "Acme" {
		t.Errorf("got %v, problems %v", got, problems)
	}
	_, problems = ResolveWatchedInputs(crew, map[string]string{"followups_due": strings.Repeat("x", MaxWatchInputLen+1)})
	if !containsProblem(problems, "followups_due") {
		t.Errorf("want too-long watch input problem, got %v", problems)
	}
	// Anyone else cannot add them, nor exceed the normal cap on a declared one
	_, problems = ResolveInputs(crew, map[string]string{"dry_run": "true", "changes": long})
	if !containsProblem(problems, "dry_run") || !containsProblem(problems, "changes") {
		t.Errorf("want undeclared and too-long problems, got %v", problems)
	}
}

func TestBuildFreezesOrderedSpec(t *testing.T) {
	lib := fixture()
	crew := crewFixture()
	crew.TaskIDs = []string{"t1", "t2"}
	spec := Build(crew, lib, map[string]string{"topic": "AI"}, 3)

	if spec.BudgetUSD != 3 || spec.Inputs["topic"] != "AI" {
		t.Errorf("budget/inputs not set: %+v", spec)
	}
	if len(spec.Tasks) != 2 || spec.Tasks[0].ID != "t1" || spec.Tasks[1].ID != "t2" {
		t.Errorf("tasks not in crew order: %+v", spec.Tasks)
	}
	if len(spec.Agents) != 2 || spec.Agents[0].ID != "a1" {
		t.Errorf("agents not in crew order: %+v", spec.Agents)
	}
	if len(spec.LLMs) != 1 {
		t.Errorf("want only referenced LLMs, got %d", len(spec.LLMs))
	}
}

func TestMatchPrice(t *testing.T) {
	prices := []model.ModelPrice{{Model: "gpt-6-luna", OutputPerMTok: 1}, {Model: "openai/gpt-6-luna", OutputPerMTok: 2}}
	if p := MatchPrice("openai/gpt-6-luna", prices); p == nil || p.OutputPerMTok != 2 {
		t.Errorf("exact match should win, got %+v", p)
	}
	if p := MatchPrice("azure/gpt-6-luna", prices); p == nil || p.OutputPerMTok != 1 {
		t.Errorf("suffix match failed, got %+v", p)
	}
	if p := MatchPrice("unknown", prices); p != nil {
		t.Errorf("want nil, got %+v", p)
	}
}

func TestTaskCrewOrdersContextAndBorrowsInputs(t *testing.T) {
	agent := "a"
	lib := Library{
		Agents: map[string]model.Agent{"a": {ID: "a", Role: "Analyst for {client}"}},
		Tasks: map[string]model.Task{
			"t1": {ID: "t1", Name: "Gather", Description: "Gather on {company}", AgentID: &agent},
			"t2": {ID: "t2", Name: "Check", Description: "Check {company} in {location}", AgentID: &agent, ContextTaskIDs: []string{"t1"}},
			"t3": {ID: "t3", Name: "Memo", Description: "Memo", AgentID: &agent, ContextTaskIDs: []string{"t2", "t1"}},
		},
		Crews: []model.Crew{{TaskIDs: []string{"t1", "t2", "t3"}, Inputs: []model.CrewInput{
			{Key: "company", Label: "Company", Default: "Acme", Required: true}, {Key: "location", Label: "Where", Required: false}}}},
	}
	crew, problems := TaskCrew("t3", lib)
	if len(problems) > 0 || len(crew.TaskIDs) != 3 || crew.TaskIDs[0] != "t1" || crew.TaskIDs[2] != "t3" || crew.Name != "Memo" {
		t.Fatalf("crew = %+v problems %v", crew, problems)
	}
	inputs := map[string]model.CrewInput{}
	for _, in := range crew.Inputs {
		inputs[in.Key] = in
	}
	if inputs["company"].Default != "Acme" || inputs["location"].Required || !inputs["client"].Required || len(inputs) != 3 {
		t.Errorf("inputs = %+v", crew.Inputs)
	}

	lib.Tasks["t1"] = model.Task{ID: "t1", Name: "Gather", ContextTaskIDs: []string{"t3"}}
	if _, problems := TaskCrew("t3", lib); len(problems) == 0 || !strings.Contains(problems[0], "loop") {
		t.Errorf("a context loop must be reported, got %v", problems)
	}
	if _, problems := TaskCrew("nope", lib); len(problems) == 0 {
		t.Error("a missing task must be reported")
	}
}
