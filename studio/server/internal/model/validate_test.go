package model

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

const uuidA = "22222222-0000-4000-8000-000000000001"
const uuidB = "22222222-0000-4000-8000-000000000002"

func ptr[T any](v T) *T { return &v }

func detailsOf(t *testing.T, err error) []string {
	t.Helper()
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	return verr.Details
}

func hasDetail(details []string, prefix string) bool {
	return slices.ContainsFunc(details, func(d string) bool { return strings.HasPrefix(d, prefix) })
}

func TestNormalizeLLM(t *testing.T) {
	tests := []struct {
		name    string
		in      LLM
		wantErr []string
	}{
		{"valid", LLM{Name: " Luna ", Model: "openai/gpt-6-luna", Temperature: ptr(0.7), MaxCompletionTokens: ptr(4096)}, nil},
		{"nulls allowed", LLM{Name: "Luna", Model: "openai/gpt-6-luna"}, nil},
		{"missing name and model", LLM{}, []string{"name:", "model:"}},
		{"model with spaces", LLM{Name: "x", Model: "gpt 6"}, []string{"model:"}},
		{"temperature out of range", LLM{Name: "x", Model: "m", Temperature: ptr(2.5)}, []string{"temperature:"}},
		{"tokens out of range", LLM{Name: "x", Model: "m", MaxCompletionTokens: ptr(0)}, []string{"max_completion_tokens:"}},
		{"reasoning model", LLM{Name: "Sol", Model: "openai/gpt-6.1-sol", ReasoningEffort: ptr("high"), API: "responses"}, nil},
		{"bad effort", LLM{Name: "x", Model: "m", ReasoningEffort: ptr("extreme")}, []string{"reasoning_effort:"}},
		{"responses api on non-openai", LLM{Name: "x", Model: "anthropic/claude-opus-5-5", API: "responses"}, []string{"api:"}},
		{"unknown api", LLM{Name: "x", Model: "m", API: "grpc"}, []string{"api:"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeLLM(tt.in)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.Name != strings.TrimSpace(tt.in.Name) {
					t.Errorf("name not trimmed: %q", got.Name)
				}
				return
			}
			details := detailsOf(t, err)
			for _, want := range tt.wantErr {
				if !hasDetail(details, want) {
					t.Errorf("missing detail %q in %v", want, details)
				}
			}
		})
	}
}

func TestNormalizeLLMDoesNotMutateInput(t *testing.T) {
	in := LLM{Name: "  spaced  ", Model: " m "}
	if _, err := NormalizeLLM(in); err != nil {
		t.Fatal(err)
	}
	if in.Name != "  spaced  " {
		t.Errorf("input was mutated: %q", in.Name)
	}
}

func TestNormalizePrice(t *testing.T) {
	if _, err := NormalizePrice(ModelPrice{Model: "gpt-6-luna", InputPerMTok: 0.1, CachedInputPerMTok: 0.01, OutputPerMTok: 0.5}); err != nil {
		t.Fatalf("valid price rejected: %v", err)
	}
	details := detailsOf(t, func() error {
		_, err := NormalizePrice(ModelPrice{Model: "", InputPerMTok: -1, OutputPerMTok: 1e9})
		return err
	}())
	for _, want := range []string{"model:", "input_per_mtok:", "output_per_mtok:"} {
		if !hasDetail(details, want) {
			t.Errorf("missing %q in %v", want, details)
		}
	}
	// A cache-write price is optional, but when given it must be a price
	write := 2.5
	if _, err := NormalizePrice(ModelPrice{Model: "claude-sonnet-5-5", InputPerMTok: 2, CacheWritePerMTok: &write, OutputPerMTok: 10}); err != nil {
		t.Errorf("valid cache-write price rejected: %v", err)
	}
	negative := -1.0
	details = detailsOf(t, func() error {
		_, err := NormalizePrice(ModelPrice{Model: "claude-sonnet-5-5", CacheWritePerMTok: &negative})
		return err
	}())
	if !hasDetail(details, "cache_write_per_mtok:") {
		t.Errorf("negative cache-write price accepted: %v", details)
	}
}

func TestNormalizeSettings(t *testing.T) {
	valid := Settings{DefaultBudgetUSD: 2, HardStopMultiplier: 1.25, SearchModel: "gemini-3.8-flash",
		GroundedSearchCostPer1000: 14, GroundedSearchesFree: 5000, GroundingFreePeriod: "month", MaxConcurrentRuns: 2}
	if _, err := NormalizeSettings(valid); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	bad := valid
	bad.HardStopMultiplier = 0.5
	bad.MaxConcurrentRuns = 0
	bad.DefaultBudgetUSD = 0
	bad.GroundingFreePeriod = "week"
	details := detailsOf(t, func() error { _, err := NormalizeSettings(bad); return err }())
	for _, want := range []string{"hard_stop_multiplier:", "max_concurrent_runs:", "default_budget_usd:", "grounding_free_period:"} {
		if !hasDetail(details, want) {
			t.Errorf("missing %q in %v", want, details)
		}
	}
}

func TestNormalizeAgent(t *testing.T) {
	known := map[string]bool{"google_search": true}
	base := Agent{Name: "Analyst", Role: "Analyst", Goal: "g", Backstory: "b", MaxIter: 10, Tools: []string{"google_search", "google_search"}}

	got, err := NormalizeAgent(base, known)
	if err != nil {
		t.Fatalf("valid agent rejected: %v", err)
	}
	if len(got.Tools) != 1 {
		t.Errorf("duplicate tools not removed: %v", got.Tools)
	}

	bad := base
	bad.Tools = []string{"rm_rf"}
	bad.MaxIter = 0
	bad.LLMID = ptr("not-a-uuid")
	bad.Role = ""
	details := detailsOf(t, func() error { _, err := NormalizeAgent(bad, known); return err }())
	for _, want := range []string{"tools:", "max_iter:", "llm_id:", "role:"} {
		if !hasDetail(details, want) {
			t.Errorf("missing %q in %v", want, details)
		}
	}
}

func TestNormalizeAgentNilToolsBecomesEmpty(t *testing.T) {
	got, err := NormalizeAgent(Agent{Name: "a", Role: "r", Goal: "g", Backstory: "b", MaxIter: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tools == nil {
		t.Error("tools should be an empty slice, not nil")
	}
}

func TestNormalizeTask(t *testing.T) {
	ok := Task{ID: uuidA, Name: "t", Description: "d", ExpectedOutput: "e", ContextTaskIDs: []string{uuidB}}
	if _, err := NormalizeTask(ok); err != nil {
		t.Fatalf("valid task rejected: %v", err)
	}
	selfRef := ok
	selfRef.ContextTaskIDs = []string{uuidA, uuidB, uuidB}
	details := detailsOf(t, func() error { _, err := NormalizeTask(selfRef); return err }())
	if !hasDetail(details, "context_task_ids:") {
		t.Errorf("self/duplicate context not rejected: %v", details)
	}
}

func TestNormalizeCrew(t *testing.T) {
	ok := Crew{Name: "c", Process: ProcessSequential, BudgetUSD: 2, TaskIDs: []string{uuidA}, AgentIDs: []string{uuidB},
		Inputs: []CrewInput{{Key: "company", Label: "Company", Required: true}}}
	if _, err := NormalizeCrew(ok); err != nil {
		t.Fatalf("valid crew rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(c Crew) Crew
		want   string
	}{
		{"bad process", func(c Crew) Crew { c.Process = "parallel"; return c }, "process:"},
		{"hierarchical without manager", func(c Crew) Crew { c.Process = ProcessHierarchical; return c }, "manager_llm_id:"},
		{"bad input key", func(c Crew) Crew { c.Inputs = []CrewInput{{Key: "Company Name"}}; return c }, "inputs[0].key:"},
		{"duplicate input key", func(c Crew) Crew {
			c.Inputs = []CrewInput{{Key: "a"}, {Key: "a"}}
			return c
		}, "inputs[1].key:"},
		{"duplicate task", func(c Crew) Crew { c.TaskIDs = []string{uuidA, uuidA}; return c }, "task_ids:"},
		{"zero budget", func(c Crew) Crew { c.BudgetUSD = 0; return c }, "budget_usd:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeCrew(tt.mutate(ok))
			if details := detailsOf(t, err); !hasDetail(details, tt.want) {
				t.Errorf("want %q in %v", tt.want, details)
			}
		})
	}
}

func TestNormalizeCrewDefaultsLabelToKey(t *testing.T) {
	got, err := NormalizeCrew(Crew{Name: "c", Process: ProcessSequential, BudgetUSD: 1, Inputs: []CrewInput{{Key: "topic"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Inputs[0].Label != "topic" {
		t.Errorf("label = %q, want topic", got.Inputs[0].Label)
	}
}

func TestPlaceholders(t *testing.T) {
	got := Placeholders(`Research {company} in {location}. Again {company}. JSON {"a": 1} and {9bad} and {snake_case-x}`)
	want := []string{"company", "location", "snake_case-x"}
	if !slices.Equal(got, want) {
		t.Errorf("Placeholders = %v, want %v", got, want)
	}
}

func TestInterpolate(t *testing.T) {
	got := Interpolate("Hi {name}, {missing} stays", map[string]string{"name": "Ada"})
	if got != "Hi Ada, {missing} stays" {
		t.Errorf("Interpolate = %q", got)
	}
}
