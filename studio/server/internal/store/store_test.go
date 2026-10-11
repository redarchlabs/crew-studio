package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/testdb"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return New(testdb.New(t, "store_test"))
}

func ptr[T any](v T) *T { return &v }

func wantConflict(t *testing.T, err error, fragment string) {
	t.Helper()
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("want ConflictError, got %v", err)
	}
	if !strings.Contains(conflict.Message, fragment) {
		t.Errorf("conflict message %q does not mention %q", conflict.Message, fragment)
	}
}

func TestSeedImportsExistingCrews(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	crews, err := s.ListCrews(ctx, AllAreas)
	if err != nil {
		t.Fatal(err)
	}
	// The two imported crews and the PMO template (migration 0016)
	if len(crews) != 3 {
		t.Fatalf("want 3 seeded crews, got %d", len(crews))
	}
	var pe model.Crew
	for _, c := range crews {
		if c.Name == "PE Due Diligence Crew" {
			pe = c
		}
	}
	if len(pe.TaskIDs) != 5 || len(pe.AgentIDs) != 5 || len(pe.Inputs) != 2 {
		t.Errorf("PE crew = %d tasks, %d agents, %d inputs", len(pe.TaskIDs), len(pe.AgentIDs), len(pe.Inputs))
	}
	memo, err := s.GetTask(ctx, pe.TaskIDs[4])
	if err != nil {
		t.Fatal(err)
	}
	if memo.Name != "Investment Memo" || len(memo.ContextTaskIDs) != 4 {
		t.Errorf("memo task = %q with %d context tasks", memo.Name, len(memo.ContextTaskIDs))
	}
	prices, err := s.ListPrices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	models := map[string]bool{}
	for _, p := range prices {
		models[p.Model] = true
	}
	for _, want := range []string{"gpt-6-luna", "gemini-3.8-flash", "gpt-6-astra", "gpt-6.1-sol", "claude-opus-5-5"} {
		if !models[want] {
			t.Errorf("missing seeded price for %s", want)
		}
	}
	if models["gemini-2.5-flash"] {
		t.Error("gemini-2.5-flash should have been replaced by gemini-3.8-flash")
	}
	settings, _ := s.GetSettings(ctx)
	if settings.SearchModel != "gemini-3.8-flash" || settings.GroundingFreePeriod != "month" || settings.GroundedSearchesFree != 5000 {
		t.Errorf("settings = %+v", settings)
	}
}

func TestLLMLifecycleAndHistory(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	created, err := s.CreateLLM(ctx, model.LLM{Name: "Test LLM", Model: "openai/test", Temperature: ptr(0.3), ReasoningEffort: ptr("high"), API: "responses"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Temperature == nil || *created.Temperature != 0.3 || created.MaxCompletionTokens != nil ||
		*created.ReasoningEffort != "high" || created.API != "responses" {
		t.Errorf("created = %+v", created)
	}

	_, err = s.CreateLLM(ctx, model.LLM{Name: "Test LLM", Model: "x"})
	wantConflict(t, err, "already exists")

	updated, err := s.UpdateLLM(ctx, created.ID, model.LLM{Name: "Renamed", Model: "openai/test", MaxCompletionTokens: ptr(100)})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Renamed" || updated.Temperature != nil || !updated.UpdatedAt.After(created.UpdatedAt.Add(-time.Second)) {
		t.Errorf("updated = %+v", updated)
	}

	if err := s.DeleteLLM(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetLLM(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound after delete, got %v", err)
	}

	revs, total, err := s.ListRevisions(ctx, RevisionFilter{EntityType: "llm", EntityID: created.ID, Types: []string{"llm"}, Scope: AllAreas, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || revs[0].Action != "delete" || revs[2].Action != "create" {
		t.Errorf("revisions = %d, first %q", total, revs[0].Action)
	}
}

func TestDeleteInUseIsRefused(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	llm, _ := s.CreateLLM(ctx, model.LLM{Name: "L", Model: "m"})
	agent, err := s.CreateAgent(ctx, model.Agent{Name: "A", Role: "r", Goal: "g", Backstory: "b", LLMID: &llm.ID, MaxIter: 5, Tools: []string{"google_search"}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateTask(ctx, model.Task{Name: "T1", Description: "d", ExpectedOutput: "e", AgentID: &agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateTask(ctx, model.Task{Name: "T2", Description: "d", ExpectedOutput: "e", AgentID: &agent.ID, ContextTaskIDs: []string{first.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.ContextTaskIDs) != 1 || second.ContextTaskIDs[0] != first.ID {
		t.Errorf("context = %v", second.ContextTaskIDs)
	}

	wantConflict(t, s.DeleteLLM(ctx, llm.ID), `agent "A"`)
	wantConflict(t, s.DeleteAgent(ctx, agent.ID), `task "T1"`)
	wantConflict(t, s.DeleteTask(ctx, first.ID), `task "T2" (as context)`)

	if err := s.DeleteTask(ctx, second.ID); err != nil {
		t.Fatalf("delete unused task: %v", err)
	}
}

func TestCrewMembersKeepOrder(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	crews, _ := s.ListCrews(ctx, AllAreas)
	src := crews[0]

	reversed := make([]string, len(src.TaskIDs))
	for i, id := range src.TaskIDs {
		reversed[len(src.TaskIDs)-1-i] = id
	}
	in := src
	in.Name = "Copy"
	in.TaskIDs = reversed
	created, err := s.CreateCrew(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	for i := range reversed {
		if created.TaskIDs[i] != reversed[i] {
			t.Fatalf("task order not kept: %v vs %v", created.TaskIDs, reversed)
		}
	}

	_, err = s.UpdateCrew(ctx, created.ID, model.Crew{Name: "Copy", Process: "sequential", BudgetUSD: 1,
		TaskIDs: []string{"99999999-0000-4000-8000-000000000000"}})
	wantConflict(t, err, "does not exist")

	if err := s.DeleteCrew(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRunLifecycle(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	crews, _ := s.ListCrews(ctx, AllAreas)
	crew := crews[0]

	run, _, err := s.CreateRun(ctx, model.RunSpec{Crew: crew, Inputs: map[string]string{"company": "Acme"}, BudgetUSD: 1.5}, RunMeta{AreaIDs: crew.AreaIDs})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != model.RunQueued || run.Inputs["company"] != "Acme" {
		t.Errorf("run = %+v", run)
	}
	wantConflict(t, s.DeleteRun(ctx, run.ID), "Cancel")

	ids, _ := s.QueuedRunIDs(ctx, 5)
	if len(ids) != 1 || ids[0] != run.ID {
		t.Errorf("queued = %v", ids)
	}
	started, err := s.StartRun(ctx, run.ID)
	if err != nil || !started {
		t.Fatalf("start = %v, %v", started, err)
	}
	if again, _ := s.StartRun(ctx, run.ID); again {
		t.Error("a running run must not start twice")
	}

	now := time.Now()
	for i, ev := range []model.RunEvent{
		{Seq: 1, Type: "task_started", Payload: map[string]any{"task_name": "x"}, CreatedAt: now},
		{Seq: 2, Type: "log", Payload: map[string]any{"line": "hi"}, CreatedAt: now},
		{Seq: 3, Type: "search", Payload: map[string]any{"grounded": true}, CreatedAt: now},
	} {
		if err := s.AppendEvent(ctx, run.ID, ev); err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
	}
	if err := s.AppendLLMCall(ctx, run.ID, model.LLMCall{Seq: 4, Model: "m", Messages: []map[string]string{{"role": "user", "content": "hi"}},
		Response: "hello", Usage: model.TokenUsage{PromptTokens: 3}, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	events, _ := s.ListEvents(ctx, run.ID, 1, 10)
	if len(events) != 2 || events[0].Seq != 2 {
		t.Errorf("events after 1 = %+v", events)
	}
	interactionEvents, _ := s.InteractionEvents(ctx, run.ID)
	if len(interactionEvents) != 2 {
		t.Errorf("interaction events should skip logs, got %d", len(interactionEvents))
	}
	if seq, _ := s.MaxEventSeq(ctx, run.ID); seq != 3 {
		t.Errorf("max seq = %d", seq)
	}
	if n, _ := s.GroundedSearchesInPeriod(ctx, "month"); n != 1 {
		t.Errorf("grounded today = %d", n)
	}
	calls, _ := s.ListLLMCalls(ctx, run.ID)
	if len(calls) != 1 || calls[0].Response != "hello" {
		t.Errorf("llm calls = %+v", calls)
	}

	finished, err := s.FinishRun(ctx, run.ID, RunResult{Status: model.RunSucceeded, Output: ptr("memo"), CostUSD: ptr(0.42),
		Usage: &model.TokenUsage{PromptTokens: 10}, TaskOutputs: []model.TaskOutput{{Position: 0, TaskName: "x", Raw: "out"}}})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != model.RunSucceeded || finished.FinishedAt == nil || *finished.CostUSD != 0.42 {
		t.Errorf("finished = %+v", finished)
	}
	if _, err := s.FinishRun(ctx, run.ID, RunResult{Status: model.RunFailed}); !errors.Is(err, ErrNotFound) {
		t.Errorf("finishing twice should be refused, got %v", err)
	}

	detail, err := s.GetRunDetail(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Spec == nil || detail.Spec.Crew.ID != crew.ID || detail.TaskOutputs == nil || len(*detail.TaskOutputs) != 1 {
		t.Errorf("detail = %+v", detail)
	}

	runs, total, _ := s.ListRuns(ctx, RunFilter{CrewID: crew.ID, Scope: AllAreas, Limit: 10})
	if total != 1 || len(runs) != 1 {
		t.Errorf("list runs = %d/%d", len(runs), total)
	}
	overview, err := s.Overview(ctx, AllAreas)
	if err != nil || overview.Counts.Runs != 1 || overview.SpendLast30dUSD != 0.42 {
		t.Errorf("overview = %+v, %v", overview, err)
	}

	if err := s.DeleteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
}

func TestFailOrphanedRuns(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	crews, _ := s.ListCrews(ctx, AllAreas)
	run, _, _ := s.CreateRun(ctx, model.RunSpec{Crew: crews[0], BudgetUSD: 1}, RunMeta{})
	_, _ = s.StartRun(ctx, run.ID)

	ids, err := s.FailOrphanedRuns(ctx)
	if err != nil || len(ids) != 1 {
		t.Fatalf("orphaned = %v, %v", ids, err)
	}
	got, _ := s.GetRun(ctx, run.ID)
	if got.Status != model.RunFailed || got.Error == nil {
		t.Errorf("run = %+v", got)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	st, err := s.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st.MaxConcurrentRuns = 4
	updated, err := s.UpdateSettings(ctx, st)
	if err != nil || updated.MaxConcurrentRuns != 4 {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	price, err := s.CreatePrice(ctx, model.ModelPrice{Model: "new-model", InputPerMTok: 1, OutputPerMTok: 2})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreatePrice(ctx, model.ModelPrice{Model: "new-model"})
	wantConflict(t, err, "new-model")
	if _, err := s.UpdatePrice(ctx, price.ID, model.ModelPrice{Model: "new-model", InputPerMTok: 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePrice(ctx, price.ID); err != nil {
		t.Fatal(err)
	}
}
