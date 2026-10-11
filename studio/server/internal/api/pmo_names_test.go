package api

import (
	"net/http"
	"strings"
	"testing"

	"crewstudio/internal/model"
	"crewstudio/internal/pmo"
)

// "PMO" names its coordinator "PMO Coordinator", the template's own agent: the problem points at the Name field.
func TestPMONameCollisionPointsAtTheName(t *testing.T) {
	e := newReportEnv(t)
	jira := e.jira(t, "Acme Jira", financeArea)
	r := e.do(t, "POST", "/api/pmo/setup", map[string]any{"name": "PMO", "area_ids": []string{financeArea}, "tracker_connection_id": jira.ID, "timezone": "UTC"})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if got := strings.Join(r.body.Error.Details, "; "); got != `name: an agent named "PMO Coordinator" already exists; choose another name` {
		t.Errorf("details = %q", got)
	}
	// The default name does not collide
	r = e.do(t, "POST", "/api/pmo/setup", map[string]any{"area_ids": []string{financeArea}, "tracker_connection_id": jira.ID, "timezone": "UTC"})
	wantStatus(t, r, http.StatusCreated)
}

func TestMissingProviderKeysAreWarnings(t *testing.T) {
	e := newReportEnv(t)
	e.jira(t, "Acme Jira", financeArea)
	opts := decodeData[pmo.Options](t, e.do(t, "GET", "/api/pmo/setup", nil))
	warned, clean := 0, 0
	for _, l := range opts.LLMs {
		switch env := model.ProviderKeyEnv(l.Kind); {
		case env == "OPENAI_API_KEY" || env == "":
			if l.Warning != "" {
				t.Errorf("%s (%s) warned: %s", l.Name, l.Kind, l.Warning)
			}
			clean++
		default:
			if !strings.Contains(l.Warning, env+" is not set in the provider env file") {
				t.Errorf("%s (%s) warning = %q", l.Name, l.Kind, l.Warning)
			}
			warned++
		}
	}
	if warned == 0 || clean == 0 {
		t.Errorf("expected seeded LLMs of both kinds: %+v", opts.LLMs)
	}

	llm := decodeData[model.LLM](t, e.do(t, "POST", "/api/llms", map[string]any{"name": "Haiku", "model": "anthropic/claude-haiku-4-5", "api": "completions"}))
	agent := decodeData[model.Agent](t, e.do(t, "POST", "/api/agents", map[string]any{"name": "Writer", "role": "r", "goal": "g", "backstory": "b",
		"max_iter": 3, "llm_id": llm.ID, "area_ids": []string{financeArea}}))
	task := decodeData[model.Task](t, e.do(t, "POST", "/api/tasks", map[string]any{"name": "Write", "description": "d", "expected_output": "e",
		"agent_id": agent.ID, "area_ids": []string{financeArea}}))
	crew := decodeData[model.Crew](t, e.do(t, "POST", "/api/crews", map[string]any{"name": "Writers", "process": "sequential", "agent_ids": []string{agent.ID},
		"task_ids": []string{task.ID}, "budget_usd": 1, "area_ids": []string{financeArea}}))
	v := decodeData[validation](t, e.do(t, "GET", "/api/crews/"+crew.ID+"/validate", nil))
	if len(v.Warnings) != 1 || !strings.Contains(v.Warnings[0], "LLM Haiku (anthropic/claude-haiku-4-5): ANTHROPIC_API_KEY is not set") {
		t.Errorf("validation = %+v", v)
	}
}
