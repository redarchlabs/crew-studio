package model

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestNormalizeAgentChatSettings(t *testing.T) {
	agent := "22222222-0000-4000-8000-000000000001"
	other := "22222222-0000-4000-8000-000000000002"
	crew := "44444444-0000-4000-8000-000000000001"
	blank := " "
	conn := "55555555-0000-4000-8000-000000000001"
	got, err := NormalizeAgentChatSettings(AgentChatSettings{AgentID: agent, Enabled: true, LLMID: &blank,
		SpecialistIDs: []string{other}, CrewIDs: []string{crew}, ConnectionIDs: []string{conn}, DailyBudgetUSD: 2})
	if err != nil || got.LLMID != nil || got.PersonDailyBudgetUSD != DefaultChatPersonBudgetUSD ||
		len(got.SpecialistIDs) != 1 || len(got.CrewIDs) != 1 || len(got.ConnectionIDs) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got, err := NormalizeAgentChatSettings(AgentChatSettings{AgentID: agent, DailyBudgetUSD: 1}); err != nil ||
		got.SpecialistIDs == nil || got.CrewIDs == nil || got.ConnectionIDs == nil {
		t.Errorf("nil lists: %+v, %v", got, err)
	}

	many := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("33333333-0000-4000-8000-%012d", i+1)
		}
		return out
	}
	bad := "nope"
	cases := map[string]AgentChatSettings{
		"bad llm":              {AgentID: agent, LLMID: &bad, DailyBudgetUSD: 1},
		"itself":               {AgentID: agent, SpecialistIDs: []string{agent}, DailyBudgetUSD: 1},
		"specialist twice":     {AgentID: agent, SpecialistIDs: []string{other, other}, DailyBudgetUSD: 1},
		"bad specialist":       {AgentID: agent, SpecialistIDs: []string{"x"}, DailyBudgetUSD: 1},
		"crew twice":           {AgentID: agent, CrewIDs: []string{crew, crew}, DailyBudgetUSD: 1},
		"too many specialists": {AgentID: agent, SpecialistIDs: many(MaxChatSpecialists + 1), DailyBudgetUSD: 1},
		"too many crews":       {AgentID: agent, CrewIDs: many(MaxChatCrews + 1), DailyBudgetUSD: 1},
		"connection twice":     {AgentID: agent, ConnectionIDs: []string{conn, conn}, DailyBudgetUSD: 1},
		"bad connection":       {AgentID: agent, ConnectionIDs: []string{"x"}, DailyBudgetUSD: 1},
		"too many connections": {AgentID: agent, ConnectionIDs: many(MaxChatConnections + 1), DailyBudgetUSD: 1},
		"no budget":            {AgentID: agent},
		"budget too high":      {AgentID: agent, DailyBudgetUSD: MaxChatDailyBudgetUSD + 1},
		"person budget":        {AgentID: agent, DailyBudgetUSD: 1, PersonDailyBudgetUSD: -1},
		"person too high":      {AgentID: agent, DailyBudgetUSD: 1, PersonDailyBudgetUSD: MaxChatPersonBudgetUSD + 1},
	}
	for name, c := range cases {
		if _, err := NormalizeAgentChatSettings(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if d := DefaultAgentChatSettings("a1"); d.Enabled || d.AgentID != "a1" || d.SpecialistIDs == nil || d.CrewIDs == nil || d.ConnectionIDs == nil ||
		d.DailyBudgetUSD != DefaultChatDailyBudgetUSD {
		t.Errorf("default = %+v", d)
	}
}

func TestChatInputs(t *testing.T) {
	cases := []struct {
		name string
		in   []CrewInput
		want []ChatInput
	}{
		{"none", nil, []ChatInput{}},
		{"label and required carried, default left out", []CrewInput{
			{Key: "company", Label: "Company legal name", Default: "Northwind", Required: true},
			{Key: "location", Label: "Where it is based"},
		}, []ChatInput{{Key: "company", Label: "Company legal name", Required: true}, {Key: "location", Label: "Where it is based"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ChatInputs(tc.in); !slices.Equal(got, tc.want) {
				t.Errorf("ChatInputs = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Hand-off settings: left out (0) they take their defaults; given, they must be in range.
func TestNormalizeAgentChatHandoffSettings(t *testing.T) {
	agent := "22222222-0000-4000-8000-000000000001"
	got, err := NormalizeAgentChatSettings(AgentChatSettings{AgentID: agent, DailyBudgetUSD: 1, HandoffsEnabled: true})
	if err != nil || !got.HandoffsEnabled || got.HandoffBudgetUSD != DefaultHandoffBudgetUSD ||
		got.HandoffTimeoutMinutes != DefaultHandoffTimeoutMinutes || got.MaxActiveHandoffs != DefaultMaxActiveHandoffs {
		t.Fatalf("defaults: %+v, %v", got, err)
	}
	cases := []struct {
		name    string
		in      AgentChatSettings
		problem string
	}{
		{"budget below zero", AgentChatSettings{HandoffBudgetUSD: -1}, "handoff_budget_usd"},
		{"budget too high", AgentChatSettings{HandoffBudgetUSD: MaxHandoffBudgetUSD + 1}, "handoff_budget_usd"},
		{"timeout below one", AgentChatSettings{HandoffTimeoutMinutes: -5}, "handoff_timeout_minutes"},
		{"timeout too long", AgentChatSettings{HandoffTimeoutMinutes: MaxHandoffTimeoutMinutes + 1}, "handoff_timeout_minutes"},
		{"too few at once", AgentChatSettings{MaxActiveHandoffs: -1}, "max_active_handoffs"},
		{"too many at once", AgentChatSettings{MaxActiveHandoffs: MaxActiveHandoffsLimit + 1}, "max_active_handoffs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			in.AgentID, in.DailyBudgetUSD = agent, 1
			_, err := NormalizeAgentChatSettings(in)
			if err == nil || !strings.Contains(err.Error(), tc.problem) {
				t.Errorf("err = %v, want %s", err, tc.problem)
			}
		})
	}
	if d := DefaultAgentChatSettings("a1"); d.HandoffsEnabled || d.HandoffBudgetUSD != DefaultHandoffBudgetUSD ||
		d.HandoffTimeoutMinutes != DefaultHandoffTimeoutMinutes || d.MaxActiveHandoffs != DefaultMaxActiveHandoffs {
		t.Errorf("default = %+v", d)
	}
}

func TestIsChatKind(t *testing.T) {
	for kind, want := range map[string]bool{RunKindChat: true, RunKindHandoff: true, RunKindCrew: false, "": false} {
		if IsChatKind(kind) != want {
			t.Errorf("IsChatKind(%q) = %v", kind, !want)
		}
	}
}

func TestNormalizeAgentChatQuestionSettings(t *testing.T) {
	agent := "22222222-0000-4000-8000-000000000001"
	got, err := NormalizeAgentChatSettings(AgentChatSettings{AgentID: agent, DailyBudgetUSD: 1, QuestionRules: "  Ask before picking prices  "})
	if err != nil || got.QuestionsEnabled || got.QuestionRules != "Ask before picking prices" || got.QuestionTimeoutMinutes != DefaultQuestionTimeoutMinutes ||
		got.QuestionOnTimeout != EscalationProceed || got.MaxQuestions != DefaultChatMaxQuestions {
		t.Fatalf("defaults: %+v, %v", got, err)
	}
	if got.QuestionEscalation() != nil {
		t.Error("questions off still froze an escalation")
	}
	on, err := NormalizeAgentChatSettings(AgentChatSettings{AgentID: agent, DailyBudgetUSD: 1, QuestionsEnabled: true, QuestionRules: "Ask first",
		QuestionTimeoutMinutes: 5, QuestionOnTimeout: EscalationFail, MaxQuestions: 2})
	if err != nil {
		t.Fatal(err)
	}
	if esc := on.QuestionEscalation(); esc == nil || *esc != (EscalationSpec{Rules: "Ask first", TimeoutMinutes: 5, OnTimeout: EscalationFail, MaxQuestions: 2}) {
		t.Errorf("escalation = %+v", esc)
	}
	cases := []struct {
		name    string
		in      AgentChatSettings
		problem string
	}{
		{"rules required when on", AgentChatSettings{QuestionsEnabled: true, QuestionRules: "   "}, "question_rules"},
		{"rules too long", AgentChatSettings{QuestionRules: strings.Repeat("x", MaxEscalationRulesLen+1)}, "question_rules"},
		{"timeout below one", AgentChatSettings{QuestionTimeoutMinutes: -1}, "question_timeout_minutes"},
		{"timeout too long", AgentChatSettings{QuestionTimeoutMinutes: MaxEscalationTimeoutMinutes + 1}, "question_timeout_minutes"},
		{"unknown on timeout", AgentChatSettings{QuestionOnTimeout: "wait"}, "question_on_timeout"},
		{"too few questions", AgentChatSettings{MaxQuestions: -1}, "max_questions"},
		{"too many questions", AgentChatSettings{MaxQuestions: MaxEscalationMaxQuestions + 1}, "max_questions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			in.AgentID, in.DailyBudgetUSD = agent, 1
			_, err := NormalizeAgentChatSettings(in)
			if err == nil || !strings.Contains(err.Error(), tc.problem) {
				t.Errorf("err = %v, want %s", err, tc.problem)
			}
		})
	}
	if d := DefaultAgentChatSettings("a1"); d.QuestionsEnabled || d.QuestionTimeoutMinutes != DefaultQuestionTimeoutMinutes ||
		d.QuestionOnTimeout != EscalationProceed || d.MaxQuestions != DefaultChatMaxQuestions {
		t.Errorf("default = %+v", d)
	}
}
