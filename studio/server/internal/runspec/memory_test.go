package runspec

import (
	"strings"
	"testing"

	"crewstudio/internal/model"
)

func memoryLib() (model.Crew, Library) {
	cheap, dear, unpriced, extra := "l-cheap", "l-dear", "l-unpriced", "l-extra"
	lib := Library{
		LLMs: map[string]model.LLM{
			cheap: {ID: cheap, Name: "Cheap", Model: "openai/cheap"}, dear: {ID: dear, Name: "Dear", Model: "dear"},
			unpriced: {ID: unpriced, Name: "Unpriced", Model: "nothing"}, extra: {ID: extra, Name: "Extra", Model: "extra"},
		},
		Agents: map[string]model.Agent{
			"a1": {ID: "a1", LLMID: &dear}, "a2": {ID: "a2", LLMID: &cheap}, "a3": {ID: "a3", LLMID: &unpriced}, "a4": {ID: "a4"},
		},
		Pricing: []model.ModelPrice{{Model: "cheap", InputPerMTok: 0.1, OutputPerMTok: 0.4}, {Model: "dear", InputPerMTok: 2, OutputPerMTok: 8},
			{Model: "extra", InputPerMTok: 1, OutputPerMTok: 1}},
	}
	crew := model.Crew{ID: "c1", AgentIDs: []string{"a1", "a2", "a3", "a4", "missing"}}
	return crew, lib
}

func TestCheapestAgentLLM(t *testing.T) {
	crew, lib := memoryLib()
	if got := CheapestAgentLLM(crew, lib); got != "l-cheap" {
		t.Errorf("cheapest = %q", got)
	}
	// On a tie the name decides
	lib.Pricing[1] = model.ModelPrice{Model: "dear", InputPerMTok: 0.1, OutputPerMTok: 0.4}
	lib.LLMs["l-dear"] = model.LLM{ID: "l-dear", Name: "Alpha", Model: "dear"}
	if got := CheapestAgentLLM(crew, lib); got != "l-dear" {
		t.Errorf("tie = %q", got)
	}
	crew.AgentIDs = []string{"a3", "a4"}
	if got := CheapestAgentLLM(crew, lib); got != "" {
		t.Errorf("none priced = %q", got)
	}
}

func TestFreezeMemory(t *testing.T) {
	crew, lib := memoryLib()
	base := model.DefaultMemorySettings(crew.ID)
	newSpec := func() *model.RunSpec {
		return &model.RunSpec{Crew: crew, LLMs: []model.LLM{lib.LLMs["l-dear"], lib.LLMs["l-cheap"]}}
	}

	spec := newSpec()
	if p := FreezeMemory(spec, base, lib, "m:768"); p != nil || spec.Memory != nil {
		t.Errorf("off: %v %+v", p, spec.Memory)
	}

	// Recall only, shallow: no model needed
	s := base
	s.Mode = model.MemoryRecallOnly
	spec = newSpec()
	if p := FreezeMemory(spec, s, lib, "m:768"); p != nil || spec.Memory == nil || spec.Memory.LLMID != "" ||
		spec.Memory.EmbedModel != "m:768" || spec.Memory.RecallLimit != 5 || !spec.Memory.ReadOnly() {
		t.Errorf("recall only: %v %+v", p, spec.Memory)
	}

	// Automatic with no model chosen: the crew's cheapest agent model
	s.Mode = model.MemoryAutomatic
	spec = newSpec()
	if p := FreezeMemory(spec, s, lib, "m"); p != nil || spec.Memory.LLMID != "l-cheap" || len(spec.LLMs) != 2 || spec.Memory.ReadOnly() {
		t.Errorf("automatic: %v %+v %d", p, spec.Memory, len(spec.LLMs))
	}

	// A chosen model outside the crew is added to the frozen LLMs, in name order
	extra := "l-extra"
	s.LLMID = &extra
	spec = newSpec()
	if p := FreezeMemory(spec, s, lib, "m"); p != nil || spec.Memory.LLMID != extra || len(spec.LLMs) != 3 || spec.LLMs[2].ID != extra {
		t.Errorf("chosen: %v %+v %v", p, spec.Memory, spec.LLMs)
	}

	// Deep recall needs a model too; an unpriced or missing one stops the run
	s = base
	s.Mode, s.RecallDepth = model.MemoryExplicit, model.MemoryDeep
	unpriced, gone := "l-unpriced", "l-gone"
	s.LLMID = &unpriced
	if p := FreezeMemory(newSpec(), s, lib, "m"); len(p) != 1 || !strings.Contains(p[0], "no price") {
		t.Errorf("unpriced: %v", p)
	}
	s.LLMID = &gone
	if p := FreezeMemory(newSpec(), s, lib, "m"); len(p) != 1 || !strings.Contains(p[0], "no longer exists") {
		t.Errorf("gone: %v", p)
	}
	s.LLMID = nil
	spec = newSpec()
	spec.Crew.AgentIDs = []string{"a3"}
	if p := FreezeMemory(spec, s, lib, "m"); len(p) != 1 || !strings.Contains(p[0], "pick one") {
		t.Errorf("no model: %v", p)
	}
}
