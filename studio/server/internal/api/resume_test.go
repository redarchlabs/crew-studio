package api

import (
	"context"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// failAfterFirstTask starts the crew's run, saves the first task's output as the runner does when a task finishes, and
// fails the run.
func (e testEnv) failAfterFirstTask(t *testing.T, runID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.st.StartRun(ctx, runID); err != nil {
		t.Fatal(err)
	}
	// What the task cost is summed from its LLM calls
	if err := e.st.AppendLLMCall(ctx, runID, model.LLMCall{Seq: 1, AgentRole: "Analyst", TaskName: "Research", Model: "openai/gpt-6-luna",
		CostUSD: 0.3, Messages: []any{}}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SaveTaskOutput(ctx, runID, model.TaskOutput{Position: 0, TaskName: "Research", AgentRole: "Analyst",
		Raw: "FIRST TASK NOTES"}); err != nil {
		t.Fatal(err)
	}
	msg := "RateLimitError: no credits remaining"
	total := 0.31
	if _, err := e.st.FinishRun(ctx, runID, store.RunResult{Status: model.RunFailed, Error: &msg, CostUSD: &total}); err != nil {
		t.Fatal(err)
	}
}

func (e testEnv) failedCrewRun(t *testing.T) (model.Crew, model.Run) {
	t.Helper()
	crew := e.crewByName(t, "AI Research Crew")
	r := e.do(t, "POST", "/api/crews/"+crew.ID+"/runs", map[string]any{"budget_usd": 3})
	wantStatus(t, r, http.StatusCreated)
	run := decodeData[model.Run](t, r)
	e.failAfterFirstTask(t, run.ID)
	return crew, run
}

func (e testEnv) runDetail(t *testing.T, id string) model.Run {
	t.Helper()
	r := e.do(t, "GET", "/api/runs/"+id, nil)
	wantStatus(t, r, http.StatusOK)
	return decodeData[model.Run](t, r)
}

func TestResumeReusesFinishedTasks(t *testing.T) {
	e := newEnv(t)
	crew, src := e.failedCrewRun(t)

	detail := e.runDetail(t, src.ID)
	hint := detail.Resumable
	if hint == nil || hint.StartPosition != 1 || hint.ReusedTasks != 1 || hint.ReusedCostUSD != 0.3 {
		t.Fatalf("resumable = %+v", hint)
	}
	if hint.TaskName != detail.Spec.Tasks[1].Name {
		t.Errorf("task name = %q", hint.TaskName)
	}
	if hint.Warnings == nil || len(hint.Warnings) != 0 || hint.SpentOnTaskUSD != 0 {
		t.Errorf("hint = %+v", hint)
	}
	// What the failed task spent before it stopped is shown too
	if err := e.st.AppendLLMCall(context.Background(), src.ID, model.LLMCall{Seq: 2, TaskName: hint.TaskName, CostUSD: 0.07, Messages: []any{}}); err != nil {
		t.Fatal(err)
	}
	detail = e.runDetail(t, src.ID)
	if detail.Resumable.SpentOnTaskUSD != 0.07 || detail.TaskCosts[hint.TaskName] != 0.07 || detail.TaskCosts["Research"] != 0.3 {
		t.Errorf("spent = %+v, task costs %v", detail.Resumable, detail.TaskCosts)
	}

	before := e.runs.notified.Load()
	r := e.do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{})
	wantStatus(t, r, http.StatusCreated)
	resumed := decodeData[model.Run](t, r)
	if resumed.ResumedFromRunID == nil || *resumed.ResumedFromRunID != src.ID || resumed.Status != model.RunQueued {
		t.Fatalf("resumed = %+v", resumed)
	}
	if e.runs.notified.Load() != before+1 {
		t.Error("the runner was not told about the new run")
	}

	got := e.runDetail(t, resumed.ID)
	if got.ResumePosition == nil || *got.ResumePosition != 1 || got.BudgetUSD != 3 || got.CrewID == nil || *got.CrewID != crew.ID {
		t.Fatalf("resumed run = %+v", got)
	}
	outs := *got.TaskOutputs
	if len(outs) != 1 || outs[0].Raw != "FIRST TASK NOTES" || outs[0].ReusedFromRunID == nil || *outs[0].ReusedFromRunID != src.ID ||
		outs[0].CostUSD == nil || *outs[0].CostUSD != 0.3 {
		t.Fatalf("reused outputs = %+v", outs)
	}
	if got.Spec.Resume == nil || got.Spec.Resume.StartPosition != 1 || got.Spec.Resume.FromRunID != src.ID ||
		len(got.Spec.Resume.Outputs) != 1 || got.Spec.Resume.Outputs[0].Raw != "FIRST TASK NOTES" {
		t.Fatalf("spec resume = %+v", got.Spec.Resume)
	}
	if got.Resumable != nil {
		t.Error("a queued run is not resumable")
	}
	if links := e.runDetail(t, src.ID).ResumedBy; links == nil || len(*links) != 1 || (*links)[0] != resumed.ID {
		t.Errorf("resumed_by = %v", links)
	}

	// A second resume while the first is still queued would redo the same work
	r = e.do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "resume_active" {
		t.Errorf("code = %s", r.body.Error.Code)
	}

	// Resuming the resumed run still points the reused output at the run that produced it
	msg := "failed again"
	if _, err := e.st.FinishRun(context.Background(), resumed.ID, store.RunResult{Status: model.RunFailed, Error: &msg}); err != nil {
		t.Fatal(err)
	}
	r = e.do(t, "POST", "/api/runs/"+resumed.ID+"/resume", map[string]any{"budget_usd": 1.5})
	wantStatus(t, r, http.StatusCreated)
	again := e.runDetail(t, decodeData[model.Run](t, r).ID)
	if out := (*again.TaskOutputs)[0]; *out.ReusedFromRunID != src.ID || again.BudgetUSD != 1.5 {
		t.Fatalf("chained resume = %+v budget %v", out, again.BudgetUSD)
	}
}

func TestResumeIdempotencyKey(t *testing.T) {
	e := newEnv(t)
	_, src := e.failedCrewRun(t)
	first := e.do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{}, "Idempotency-Key", "resume-1")
	wantStatus(t, first, http.StatusCreated)
	second := e.do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{}, "Idempotency-Key", "resume-1")
	wantStatus(t, second, http.StatusOK)
	if decodeData[model.Run](t, first).ID != decodeData[model.Run](t, second).ID {
		t.Fatal("a repeated key must return the first resume")
	}
}

func TestResumeRefusals(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	crew, src := e.failedCrewRun(t)
	spec := *e.runDetail(t, src.ID).Spec
	// Made in the store: each resume attempt below counts towards the per-person limit on starting runs
	newRun := func() model.Run {
		run, _, err := e.st.CreateRun(ctx, spec, store.RunMeta{AreaIDs: crew.AreaIDs, CreatedBy: e.adminID})
		if err != nil {
			t.Fatal(err)
		}
		return run
	}

	// Nothing finished: run it again instead
	empty := newRun()
	_, _ = e.st.StartRun(ctx, empty.ID)
	msg := "boom"
	_, _ = e.st.FinishRun(ctx, empty.ID, store.RunResult{Status: model.RunFailed, Error: &msg})
	if e.runDetail(t, empty.ID).Resumable != nil {
		t.Error("a run that finished no task is not resumable")
	}
	r := e.do(t, "POST", "/api/runs/"+empty.ID+"/resume", map[string]any{})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "not_resumable" {
		t.Errorf("code = %s", r.body.Error.Code)
	}

	// Succeeded runs are not resumed
	done := newRun()
	_, _ = e.st.FinishRun(ctx, done.ID, store.RunResult{Status: model.RunSucceeded})
	wantStatus(t, e.do(t, "POST", "/api/runs/"+done.ID+"/resume", map[string]any{}), http.StatusConflict)

	wantStatus(t, e.do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{"budget_usd": 0}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", "/api/runs/"+missingID+"/resume", map[string]any{}), http.StatusNotFound)

	// Viewers cannot resume; people outside the run's area do not see it
	_, viewer := e.person(t, "rv@example.com", []string{viewerRole}, []string{itArea}, nil)
	wantStatus(t, e.as(viewer).do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{}), http.StatusForbidden)
	if e.as(viewer).runDetail(t, src.ID).Resumable != nil {
		t.Error("the hint is only for people who may resume")
	}
	_, outsider := e.person(t, "ro@example.com", []string{builderRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(outsider).do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{}), http.StatusNotFound)
}

func TestResumeWithCurrentSettings(t *testing.T) {
	e := newEnv(t)
	crew, src := e.failedCrewRun(t)
	spec := e.runDetail(t, src.ID).Spec

	// The finished task's agent was edited: its output cannot be reused with the current settings
	agentID := *spec.Tasks[0].AgentID
	agent := decodeData[model.Agent](t, e.do(t, "GET", "/api/agents/"+agentID, nil))
	original := agent.Goal
	agent.Goal = "A sharper goal"
	wantStatus(t, e.do(t, "PUT", "/api/agents/"+agentID, agent), http.StatusOK)
	r := e.do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{"use_current": true})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	agent = decodeData[model.Agent](t, e.do(t, "GET", "/api/agents/"+agentID, nil))
	agent.Goal = original
	wantStatus(t, e.do(t, "PUT", "/api/agents/"+agentID, agent), http.StatusOK)

	// The task still to run changed after the run: use_current picks up the change
	next := decodeData[model.Task](t, e.do(t, "GET", "/api/tasks/"+spec.Tasks[1].ID, nil))
	next.Description = "A sharper description."
	wantStatus(t, e.do(t, "PUT", "/api/tasks/"+next.ID, next), http.StatusOK)
	r = e.do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{"use_current": true})
	wantStatus(t, r, http.StatusCreated)
	got := e.runDetail(t, decodeData[model.Run](t, r).ID)
	if got.Spec.Tasks[1].Description != "A sharper description." || got.Spec.Resume == nil || got.Spec.Resume.StartPosition != 1 {
		t.Fatalf("task %q, resume %+v", got.Spec.Tasks[1].Description, got.Spec.Resume)
	}
	if frozen := e.runDetail(t, src.ID).Spec; frozen.Tasks[1].Description == "A sharper description." {
		t.Fatal("the earlier run's frozen spec must not change")
	}

	// Once the finished task is no longer the crew's first, the current settings cannot be used
	msg := "boom"
	_, _ = e.st.FinishRun(context.Background(), got.ID, store.RunResult{Status: model.RunFailed, Error: &msg})
	crew = e.crewByName(t, crew.Name)
	crew.TaskIDs = []string{crew.TaskIDs[1], crew.TaskIDs[0]}
	wantStatus(t, e.do(t, "PUT", "/api/crews/"+crew.ID, crew), http.StatusOK)
	r = e.do(t, "POST", "/api/runs/"+got.ID+"/resume", map[string]any{"use_current": true})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "first task (or its agent) was changed or moved") {
		t.Errorf("details = %v", r.body.Error.Details)
	}
	// ... but it can still resume as it was
	wantStatus(t, e.do(t, "POST", "/api/runs/"+got.ID+"/resume", map[string]any{}), http.StatusCreated)
}

func TestResumeWorkOrderAttempt(t *testing.T) {
	e := newEnv(t)
	crew := e.crewByName(t, "AI Research Crew")
	wo := decodeData[model.WorkOrder](t, e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "Research", CrewID: &crew.ID, Submit: true}))
	if wo.CurrentRunID == nil {
		t.Fatalf("wo = %+v", wo)
	}
	first := *wo.CurrentRunID
	e.failAfterFirstTask(t, first)

	// Someone who may create work orders but not run crews resumes the attempt
	_, woc := e.person(t, "wor@example.com", nil, []string{itArea}, []string{auth.WorkOrdersView, auth.WorkOrdersCreate})
	if e.as(woc).runDetail(t, first).Resumable == nil {
		t.Fatal("the latest attempt is resumable")
	}
	r := e.as(woc).do(t, "POST", "/api/runs/"+first+"/resume", map[string]any{})
	wantStatus(t, r, http.StatusCreated)
	attempt := decodeData[model.Run](t, r)
	if attempt.WorkOrderID == nil || *attempt.WorkOrderID != wo.ID || attempt.TriggerType != "work_order" {
		t.Fatalf("attempt = %+v", attempt)
	}
	now := decodeData[model.WorkOrder](t, e.do(t, "GET", "/api/work-orders/"+wo.ID, nil))
	if now.CurrentRunID == nil || *now.CurrentRunID != attempt.ID || now.RunCount != 2 {
		t.Fatalf("work order = %+v", now)
	}

	// Only the latest attempt can be resumed
	msg := "boom"
	_, _ = e.st.FinishRun(context.Background(), attempt.ID, store.RunResult{Status: model.RunFailed, Error: &msg})
	if e.runDetail(t, first).Resumable != nil {
		t.Error("an older attempt is not resumable")
	}
	r = e.do(t, "POST", "/api/runs/"+first+"/resume", map[string]any{})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "not_latest_attempt" {
		t.Errorf("code = %s", r.body.Error.Code)
	}

	// crews.run alone does not resume a work order's attempt
	_, runner := e.person(t, "cr@example.com", nil, []string{itArea}, []string{auth.RunsView, auth.CrewsRun})
	wantStatus(t, e.as(runner).do(t, "POST", "/api/runs/"+attempt.ID+"/resume", map[string]any{}), http.StatusForbidden)
}

func TestTaskOutputsSavedAsTasksFinish(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, src := e.failedCrewRun(t)
	resumed := decodeData[model.Run](t, e.do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{}))
	_, _ = e.st.StartRun(ctx, resumed.ID)

	// A reused output is never replaced, by a finished task or by the final result
	if err := e.st.SaveTaskOutput(ctx, resumed.ID, model.TaskOutput{Position: 0, TaskName: "x", Raw: "OVERWRITTEN"}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AppendLLMCall(ctx, resumed.ID, model.LLMCall{Seq: 1, TaskName: "Write", CostUSD: 0.15, Messages: []any{}}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AppendEvent(ctx, resumed.ID, model.RunEvent{Seq: 2, Type: "search", CreatedAt: time.Now(),
		Payload: map[string]any{"task_name": "Write", "cost_usd": 0.05}}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SaveTaskOutput(ctx, resumed.ID, model.TaskOutput{Position: 1, TaskName: "Write", Raw: "REPORT"}); err != nil {
		t.Fatal(err)
	}
	out := "REPORT"
	if _, err := e.st.FinishRun(ctx, resumed.ID, store.RunResult{Status: model.RunSucceeded, Output: &out, TaskOutputs: []model.TaskOutput{
		{Position: 0, TaskName: "x", Raw: "OVERWRITTEN"}, {Position: 1, TaskName: "Write", Raw: "REPORT"}}}); err != nil {
		t.Fatal(err)
	}
	outs := *e.runDetail(t, resumed.ID).TaskOutputs
	if outs[0].Raw != "FIRST TASK NOTES" || outs[0].ReusedFromRunID == nil {
		t.Errorf("reused output = %+v", outs[0])
	}
	if outs[1].Raw != "REPORT" || outs[1].ReusedFromRunID != nil || outs[1].CostUSD == nil || math.Abs(*outs[1].CostUSD-0.2) > 1e-9 {
		t.Errorf("new output = %+v", outs[1])
	}

	// Finished runs take no more outputs
	if err := e.st.SaveTaskOutput(ctx, resumed.ID, model.TaskOutput{Position: 2, TaskName: "late", Raw: "late"}); err != nil {
		t.Fatal(err)
	}
	if n := len(*e.runDetail(t, resumed.ID).TaskOutputs); n != 2 {
		t.Errorf("outputs after finish = %d", n)
	}
	if err := e.st.SaveTaskOutput(ctx, missingID, model.TaskOutput{Position: 0, Raw: "x"}); err == nil {
		t.Error("a missing run is an error")
	}
}

func TestResumeWorkOrderRetryReturnsTheFirstResume(t *testing.T) {
	e := newEnv(t)
	crew := e.crewByName(t, "AI Research Crew")
	wo := decodeData[model.WorkOrder](t, e.do(t, "POST", "/api/work-orders", model.WorkOrderInput{Title: "Retry", CrewID: &crew.ID, Submit: true}))
	e.failAfterFirstTask(t, *wo.CurrentRunID)

	first := e.do(t, "POST", "/api/runs/"+*wo.CurrentRunID+"/resume", map[string]any{}, "Idempotency-Key", "wo-retry")
	wantStatus(t, first, http.StatusCreated)
	// The first request succeeded, so the failed run is no longer the latest attempt; the retry still gets its answer
	retry := e.do(t, "POST", "/api/runs/"+*wo.CurrentRunID+"/resume", map[string]any{}, "Idempotency-Key", "wo-retry")
	wantStatus(t, retry, http.StatusOK)
	if decodeData[model.Run](t, first).ID != decodeData[model.Run](t, retry).ID {
		t.Fatal("the retry must return the first resume")
	}
	// A different key is a new request, and that attempt is no longer the latest
	wantStatus(t, e.do(t, "POST", "/api/runs/"+*wo.CurrentRunID+"/resume", map[string]any{}, "Idempotency-Key", "other"), http.StatusConflict)

	// Chained: the resumed attempt fails too and is resumed with the crew's current settings
	attempt := decodeData[model.Run](t, first)
	msg := "boom"
	_, _ = e.st.FinishRun(context.Background(), attempt.ID, store.RunResult{Status: model.RunFailed, Error: &msg})
	r := e.do(t, "POST", "/api/runs/"+attempt.ID+"/resume", map[string]any{"use_current": true})
	wantStatus(t, r, http.StatusCreated)
	third := e.runDetail(t, decodeData[model.Run](t, r).ID)
	if third.WorkOrderID == nil || *third.WorkOrderID != wo.ID || third.Spec.WorkOrder == nil || *(*third.TaskOutputs)[0].ReusedFromRunID != *wo.CurrentRunID {
		t.Fatalf("third attempt = %+v", third)
	}
	if n := decodeData[model.WorkOrder](t, e.do(t, "GET", "/api/work-orders/"+wo.ID, nil)).RunCount; n != 3 {
		t.Errorf("attempts = %d", n)
	}
}

func TestConcurrentResumesStartOneRun(t *testing.T) {
	e := newEnv(t)
	_, src := e.failedCrewRun(t)
	var wg sync.WaitGroup
	statuses := make([]int, 4)
	for i := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i] = e.do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{}).status
		}()
	}
	wg.Wait()
	created := 0
	for _, s := range statuses {
		switch s {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Errorf("status %d", s)
		}
	}
	if created != 1 {
		t.Fatalf("statuses = %v, want exactly one run", statuses)
	}
}

func TestResumeRespectsWhatChangedSince(t *testing.T) {
	e := newEnv(t)
	crew, src := e.failedCrewRun(t)

	// An edited finished task cannot be reused with the current settings
	spec := e.runDetail(t, src.ID).Spec
	task := decodeData[model.Task](t, e.do(t, "GET", "/api/tasks/"+spec.Tasks[0].ID, nil))
	task.Description += " Also check the dates."
	wantStatus(t, e.do(t, "PUT", "/api/tasks/"+task.ID, task), http.StatusOK)
	r := e.do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{"use_current": true})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "first task (or its agent) was changed") {
		t.Errorf("details = %v", r.body.Error.Details)
	}

	// A crew moved out of the person's areas cannot be resumed by them, as it could not be run
	_, itBuilder := e.person(t, "itb@example.com", []string{builderRole}, []string{itArea}, nil)
	crew = e.crewByName(t, crew.Name)
	crew.AreaIDs = []string{financeArea}
	wantStatus(t, e.do(t, "PUT", "/api/crews/"+crew.ID, crew), http.StatusOK)
	r = e.as(itBuilder).do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "was deleted") {
		t.Errorf("details = %v", r.body.Error.Details)
	}
	// The admin can, and the new run is filed under the crew's areas now
	r = e.do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{})
	wantStatus(t, r, http.StatusCreated)
	resumed := decodeData[model.Run](t, r)
	if len(resumed.AreaIDs) != 1 || resumed.AreaIDs[0] != financeArea {
		t.Errorf("areas = %v", resumed.AreaIDs)
	}

	// The IT builder still sees the earlier run, but not the resume filed under Finance
	if links := e.as(itBuilder).runDetail(t, src.ID).ResumedBy; links == nil || len(*links) != 0 {
		t.Errorf("resumed_by for an IT reader = %v", links)
	}
	_, finance := e.person(t, "fin@example.com", []string{viewerRole}, []string{financeArea}, nil)
	seen := e.as(finance).runDetail(t, resumed.ID)
	if seen.ResumedFromRunID != nil || !(*seen.TaskOutputs)[0].ReusedFromUnavailable {
		t.Errorf("a Finance reader must not be linked to the IT run: %+v %+v", seen.ResumedFromRunID, (*seen.TaskOutputs)[0])
	}

	// A deleted crew cannot be resumed at all
	msg := "boom"
	_, _ = e.st.FinishRun(context.Background(), resumed.ID, store.RunResult{Status: model.RunFailed, Error: &msg})
	wantStatus(t, e.do(t, "DELETE", "/api/crews/"+crew.ID, nil), http.StatusNoContent)
	r = e.do(t, "POST", "/api/runs/"+resumed.ID+"/resume", map[string]any{})
	wantStatus(t, r, http.StatusUnprocessableEntity)
}

func TestReusedOutputOfADeletedRun(t *testing.T) {
	e := newEnv(t)
	_, src := e.failedCrewRun(t)
	resumed := decodeData[model.Run](t, e.do(t, "POST", "/api/runs/"+src.ID+"/resume", map[string]any{}))
	wantStatus(t, e.do(t, "DELETE", "/api/runs/"+src.ID, nil), http.StatusNoContent)
	got := e.runDetail(t, resumed.ID)
	out := (*got.TaskOutputs)[0]
	if got.ResumedFromRunID != nil || !out.ReusedFromUnavailable || out.Raw != "FIRST TASK NOTES" || *out.CostUSD != 0.3 {
		t.Fatalf("after deleting the earlier run: %+v %+v", got.ResumedFromRunID, out)
	}
}
