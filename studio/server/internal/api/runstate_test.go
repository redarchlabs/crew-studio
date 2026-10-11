package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/journal"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

type stateRun struct {
	run      model.Run
	crew     model.Crew
	schedule model.Schedule
	token    string
}

// scheduledRun starts (in the database only) a run of the PE crew from a schedule, with a run token.
func (e testEnv) scheduledRun(t *testing.T) stateRun {
	t.Helper()
	ctx := store.WithActor(context.Background(), store.Actor{ID: e.adminID, Name: "Ada Admin"})
	crew := e.crewByName(t, "PE Due Diligence Crew")
	sc, err := e.st.SaveSchedule(ctx, "", model.Schedule{Name: "atlas sweep", CrewID: &crew.ID, Cron: "0 7 * * 1-5",
		Timezone: "UTC", OverlapPolicy: model.OverlapSkip, Inputs: map[string]string{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wo, err := e.st.CreateWorkOrder(ctx, model.WorkOrderInput{Title: "sweep", CrewID: &crew.ID},
		store.WorkOrderMeta{TargetName: crew.Name, AreaIDs: crew.AreaIDs, Source: model.SourceSchedule, SourceID: sc.ID})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := e.st.CreateRun(ctx, model.RunSpec{Crew: crew, BudgetUSD: 1}, store.RunMeta{AreaIDs: crew.AreaIDs, WorkOrderID: wo.ID})
	if err != nil {
		t.Fatal(err)
	}
	token, err := e.st.CreateRunToken(ctx, run.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return stateRun{run: run, crew: crew, schedule: sc, token: token}
}

// call is a run-state request with the run's token and agent name.
func (e testEnv) call(t *testing.T, sr stateRun, method, path string, body any) response {
	t.Helper()
	return e.as("").do(t, method, "/api/run-state"+path, body, "Authorization", "Bearer "+sr.token, agentRoleHeader, "PMO\t%43oordinator")
}

func TestRunStateNeedsTheRunToken(t *testing.T) {
	e := newEnv(t)
	sr := e.scheduledRun(t)
	// A signed-in admin's session is not enough
	wantStatus(t, e.do(t, "GET", "/api/run-state/notes", nil), http.StatusUnauthorized)
	wantStatus(t, e.as("").do(t, "GET", "/api/run-state/notes", nil, "Authorization", "Bearer csr_made-up"), http.StatusUnauthorized)
	wantStatus(t, e.as("").do(t, "GET", "/api/run-state/notes", nil, "Authorization", "Bearer "+strings.Repeat("x", 300)), http.StatusUnauthorized)
	// The run token reaches nothing else
	wantStatus(t, e.as("").do(t, "GET", "/api/crews", nil, "Authorization", "Bearer "+sr.token), http.StatusUnauthorized)

	scope := decodeData[store.RunScope](t, e.call(t, sr, "GET", "/scope", nil))
	if scope.RunID != sr.run.ID || scope.CrewID != sr.crew.ID || scope.ScheduleID == nil || *scope.ScheduleID != sr.schedule.ID {
		t.Errorf("scope = %+v", scope)
	}

	if err := e.st.ExpireRunToken(context.Background(), sr.run.ID, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.call(t, sr, "GET", "/notes", nil), http.StatusUnauthorized)

	// A run whose crew was deleted has no state
	other := e.scheduledRunOfNewToken(t, sr)
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE runs SET crew_id = NULL WHERE id = $1`, sr.run.ID); err != nil {
		t.Fatal(err)
	}
	r := e.call(t, other, "GET", "/notes", nil)
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "crew_deleted" {
		t.Errorf("error = %+v", r.body.Error)
	}
}

// scheduledRunOfNewToken gives the same run a fresh token.
func (e testEnv) scheduledRunOfNewToken(t *testing.T, sr stateRun) stateRun {
	t.Helper()
	token, err := e.st.CreateRunToken(context.Background(), sr.run.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	sr.token = token
	return sr
}

func TestRunStateNotes(t *testing.T) {
	e := newEnv(t)
	sr := e.scheduledRun(t)

	r := e.call(t, sr, "GET", "/notes/weekly-report?subject=project:atlas", nil)
	wantStatus(t, r, http.StatusNotFound)
	zero := 0
	r = e.call(t, sr, "PUT", "/notes/weekly-report", map[string]any{"subject": "project:atlas", "content": "week 41", "expected_version": zero})
	wantStatus(t, r, http.StatusOK)
	if v := decodeData[model.NoteVersion](t, r); v.Version != 1 || v.RunID == nil || *v.RunID != sr.run.ID || v.WrittenBy != "run" {
		t.Errorf("v1 = %+v", v)
	}
	r = e.call(t, sr, "PUT", "/notes/weekly-report", map[string]any{"subject": "project:atlas", "content": "again", "expected_version": zero})
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "version_conflict" {
		t.Errorf("error = %+v", r.body.Error)
	}
	got := decodeData[model.NoteVersion](t, e.call(t, sr, "GET", "/notes/weekly-report?subject=project:atlas", nil))
	if got.Content != "week 41" || got.Version != 1 {
		t.Errorf("read = %+v", got)
	}
	wantStatus(t, e.call(t, sr, "PUT", "/notes/weekly-report", map[string]any{"content": strings.Repeat("x", model.MaxNoteBytes+1)}), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "PUT", "/notes/bad%20name", map[string]any{"content": "x"}), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "GET", "/notes?subject="+url.QueryEscape("a\tb"), nil), http.StatusUnprocessableEntity)

	notes := decodeData[[]model.NoteInfo](t, e.call(t, sr, "GET", "/notes", nil))
	if len(notes) != 1 || notes[0].Name != "weekly-report" || notes[0].Bytes != 7 {
		t.Errorf("notes = %+v", notes)
	}
	if notes := decodeData[[]model.NoteInfo](t, e.call(t, sr, "GET", "/notes?subject=other", nil)); len(notes) != 0 {
		t.Errorf("other subject = %+v", notes)
	}

	// Each call left a readable line in the run's journal, credited to the calling agent
	r = e.do(t, "GET", "/api/runs/"+sr.run.ID+"/journal", nil)
	wantStatus(t, r, http.StatusOK)
	j := decodeData[journal.Journal](t, r)
	var lines []string
	for _, entry := range j.Entries {
		if entry.Kind == "state" {
			lines = append(lines, entry.Title)
			if entry.AgentRole != "PMO Coordinator" {
				t.Errorf("agent = %q", entry.AgentRole)
			}
		}
	}
	joined := strings.Join(lines, " | ")
	for _, want := range []string{"Notes: weekly-report not found", "Notes: wrote weekly-report v1 (7 bytes)", "Notes: read weekly-report v1",
		"refused: The note \"weekly-report\" is at version 1", "Notes: listed 1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("journal lacks %q: %s", want, joined)
		}
	}
}

func TestRunStateActionLog(t *testing.T) {
	e := newEnv(t)
	sr := e.scheduledRun(t)
	rec := map[string]any{"subject": "item:PRJ-15", "action": "followup:due_soon", "details": map[string]any{"to": "sam"}, "idempotency_key": "PRJ-15/due_soon/2026-10-04"}

	r := e.call(t, sr, "POST", "/log", rec)
	wantStatus(t, r, http.StatusCreated)
	first := decodeData[logRecordResponse](t, r)
	if first.Duplicate || first.Entry.RunID == nil || first.Entry.ScheduleID == nil || *first.Entry.ScheduleID != sr.schedule.ID {
		t.Errorf("first = %+v", first)
	}
	r = e.call(t, sr, "POST", "/log", rec)
	wantStatus(t, r, http.StatusOK)
	if again := decodeData[logRecordResponse](t, r); !again.Duplicate || again.Entry.ID != first.Entry.ID {
		t.Errorf("again = %+v", again)
	}
	wantStatus(t, e.call(t, sr, "POST", "/log", map[string]any{"subject": "", "action": "x"}), http.StatusUnprocessableEntity)

	check := decodeData[logCheckResponse](t, e.call(t, sr, "GET", "/log/check?subject=item:PRJ-15&action=followup:due_soon&within=7d", nil))
	if check.Entry == nil || check.Entry.ID != first.Entry.ID {
		t.Errorf("check = %+v", check)
	}
	if none := decodeData[logCheckResponse](t, e.call(t, sr, "GET", "/log/check?subject=item:PRJ-16&action=followup:due_soon", nil)); none.Entry != nil {
		t.Errorf("other subject = %+v", none)
	}
	wantStatus(t, e.call(t, sr, "GET", "/log/check?subject=item:PRJ-15&action=followup:due_soon&within=soon", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "GET", "/log/check?action=followup:due_soon", nil), http.StatusUnprocessableEntity)

	events, err := e.st.ListEvents(context.Background(), sr.run.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var messages []string
	for _, ev := range events {
		if ev.Type == "state" {
			messages = append(messages, ev.Payload["message"].(string))
		}
	}
	joined := strings.Join(messages, " | ")
	for _, want := range []string{"Log: recorded followup:due_soon for item:PRJ-15", "was already recorded",
		"Log: checked followup:due_soon for item:PRJ-15: last at", "(within 7d)", "Log: checked followup:due_soon for item:PRJ-16: none"} {
		if !strings.Contains(joined, want) {
			t.Errorf("events lack %q: %s", want, joined)
		}
	}
}

func TestRunStateIsLimitedToItsOwnScope(t *testing.T) {
	e := newEnv(t)
	scheduled := e.scheduledRun(t)
	// A manual run of the same crew has no schedule: it sees crew-wide state only
	manualRun, _, err := e.st.CreateRun(context.Background(), model.RunSpec{Crew: scheduled.crew, BudgetUSD: 1}, store.RunMeta{AreaIDs: scheduled.crew.AreaIDs})
	if err != nil {
		t.Fatal(err)
	}
	manual := e.scheduledRunOfNewToken(t, stateRun{run: manualRun, crew: scheduled.crew})
	wantStatus(t, e.call(t, scheduled, "PUT", "/notes/risk-register", map[string]any{"content": "sweep only"}), http.StatusOK)
	wantStatus(t, e.call(t, manual, "GET", "/notes/risk-register", nil), http.StatusNotFound)
	wantStatus(t, e.call(t, manual, "PUT", "/notes/risk-register", map[string]any{"content": "crew-wide"}), http.StatusOK)
	if got := decodeData[model.NoteVersion](t, e.call(t, scheduled, "GET", "/notes/risk-register", nil)); got.Content != "sweep only" {
		t.Errorf("schedule note = %+v", got)
	}
}

// Review fixes: a run whose schedule is gone must not fall back to crew-wide state, and one run cannot write
// without limit.
func TestRunStateRefusesADeletedScheduleAndBoundsWrites(t *testing.T) {
	e := newEnv(t)
	sr := e.scheduledRun(t)
	ctx := context.Background()
	if _, err := e.st.Pool().Exec(ctx,
		`INSERT INTO action_log (crew_id, subject, action, run_id) SELECT $1, 's', 'a', $2 FROM generate_series(1, $3)`,
		sr.crew.ID, sr.run.ID, maxRunStateWrites); err != nil {
		t.Fatal(err)
	}
	r := e.call(t, sr, "POST", "/log", map[string]any{"subject": "s", "action": "a"})
	wantStatus(t, r, http.StatusTooManyRequests)
	wantStatus(t, e.call(t, sr, "PUT", "/notes/n", map[string]any{"content": "x"}), http.StatusTooManyRequests)
	wantStatus(t, e.call(t, sr, "GET", "/notes", nil), http.StatusOK)

	if _, err := e.st.Pool().Exec(ctx, `DELETE FROM schedules WHERE id = $1`, sr.schedule.ID); err != nil {
		t.Fatal(err)
	}
	r = e.call(t, sr, "GET", "/notes", nil)
	wantStatus(t, r, http.StatusConflict)
	if r.body.Error.Code != "schedule_deleted" {
		t.Errorf("error = %+v", r.body.Error)
	}
}
