package store

import (
	"encoding/json"
	"testing"
	"time"

	"crewstudio/internal/model"
)

func fileWatchIn(file string) model.WatchInput {
	in, err := model.NormalizeWatch(model.WatchInput{Source: model.WatchSource{Kind: model.WatchSourceWorkspaceFile, File: "watch/" + file},
		IDField: "id", TrackedFields: []string{"due"},
		Followups: &model.FollowupLadder{DateField: "due", Steps: []model.FollowupStep{{Name: "due_soon", Offset: "-3d", Notify: "owner"}}}})
	if err != nil {
		panic(err)
	}
	return in
}

func TestWatchSaveSnapshotAndChecks(t *testing.T) {
	f := newFixture(t)
	uid := f.admin()
	ctx := WithActor(bg(), Actor{ID: uid, Name: "Admin"})
	crew := f.simpleCrew("c")
	sc := must(f.s.SaveSchedule(bg(), "", schedIn("sweep", crew.ID), nil))

	_, err := f.s.WatchOfSchedule(bg(), sc.ID)
	wantNotFound(t, err)
	w := must(f.s.SaveWatch(ctx, sc.ID, fileWatchIn("a.csv")))
	if w.ScheduleName != "sweep" || w.CrewID == nil || *w.CrewID != crew.ID || !w.DryRun || w.MaxItems != 2000 ||
		w.Followups == nil || w.Followups.Steps[0].Name != "due_soon" || w.SnapshotAt != nil || w.Source.Format != "csv" {
		t.Errorf("saved = %+v", w)
	}

	at := time.Now().UTC().Truncate(time.Second)
	items := map[string]map[string]any{"A": {"due": "2026-10-10", "n": json.Number("1.5")}, "B": {"due": nil}}
	mustOK(t, f.s.SaveSnapshot(bg(), w.ID, w.UpdatedAt, items, at))
	snap := must(f.s.LoadSnapshot(bg(), w.ID))
	if len(snap) != 2 || snap["A"]["n"] != json.Number("1.5") || snap["B"]["due"] != nil {
		t.Errorf("snapshot = %+v", snap)
	}
	got := must(f.s.WatchOfSchedule(bg(), sc.ID))
	if got.SnapshotItems != 2 || got.SnapshotAt == nil || !got.SnapshotAt.Equal(at) {
		t.Errorf("after save = %+v", got)
	}
	later := at.Add(time.Hour)
	mustOK(t, f.s.ConfirmSnapshot(bg(), w.ID, w.UpdatedAt, later))

	// Changing something that keeps the snapshot comparable keeps it; changing the source drops it
	in := fileWatchIn("a.csv")
	in.RunWhen = model.WatchRunAlways
	if kept := must(f.s.SaveWatch(ctx, sc.ID, in)); kept.SnapshotItems != 2 || !kept.SnapshotAt.Equal(later) || kept.RunWhen != model.WatchRunAlways || kept.ID != w.ID {
		t.Errorf("kept = %+v", kept)
	}
	reset := must(f.s.SaveWatch(ctx, sc.ID, fileWatchIn("b.csv")))
	if reset.SnapshotItems != 0 || reset.SnapshotAt != nil {
		t.Errorf("reset = %+v", reset)
	}
	// A check that read the watch before it changed saves nothing over the new definition
	if err := f.s.SaveSnapshot(bg(), w.ID, w.UpdatedAt, items, at); err != ErrWatchChanged {
		t.Errorf("stale save: %v", err)
	}
	if err := f.s.ConfirmSnapshot(bg(), w.ID, w.UpdatedAt, at); err != ErrWatchChanged {
		t.Errorf("stale confirm: %v", err)
	}
	if stale := must(f.s.WatchOfSchedule(bg(), sc.ID)); stale.SnapshotItems != 0 || stale.SnapshotAt != nil {
		t.Errorf("stale snapshot written: %+v", stale)
	}
	mustOK(t, f.s.SaveSnapshot(bg(), w.ID, reset.UpdatedAt, items, at))
	mustOK(t, f.s.ClearSnapshot(bg(), w.ID))
	if err := f.s.SaveSnapshot(bg(), w.ID, reset.UpdatedAt, items, at); err != ErrWatchChanged {
		t.Errorf("save after a clear: %v", err)
	}
	if cleared := must(f.s.WatchOfSchedule(bg(), sc.ID)); cleared.SnapshotItems != 0 || cleared.SnapshotAt != nil {
		t.Errorf("cleared = %+v", cleared)
	}

	// Checks: status follows the latest one; errors show until a good check
	msg := "Watch: boom"
	mustOK(t, f.s.RecordWatchCheck(bg(), w.ID, model.WatchCheck{CheckedAt: at, Outcome: model.WatchCheckError, Message: msg}))
	if e := must(f.s.WatchOfSchedule(bg(), sc.ID)); e.LastError == nil || *e.LastError != msg || e.LastChangedAt != nil {
		t.Errorf("after error = %+v", e)
	}
	wo := f.workOrder("wo", crew)
	mustOK(t, f.s.RecordWatchCheck(bg(), w.ID, model.WatchCheck{CheckedAt: later, Outcome: model.WatchCheckStarted, Added: 2, WorkOrderID: &wo.ID}))
	mustOK(t, f.s.RecordWatchCheck(bg(), w.ID, model.WatchCheck{CheckedAt: later.Add(time.Hour), Outcome: model.WatchCheckNoChanges, Unchanged: 2}))
	e := must(f.s.WatchOfSchedule(bg(), sc.ID))
	if e.LastError != nil || !e.LastChangedAt.Equal(later) || !e.LastCheckedAt.Equal(later.Add(time.Hour)) {
		t.Errorf("status = %+v", e)
	}
	checks := must(f.s.ListWatchChecks(bg(), w.ID, 2))
	if len(checks) != 2 || checks[0].Outcome != model.WatchCheckNoChanges || *checks[1].WorkOrderID != wo.ID {
		t.Errorf("checks = %+v", checks)
	}
	// Old history is swept
	mustOK(t, f.s.RecordWatchCheck(bg(), w.ID, model.WatchCheck{CheckedAt: later.Add(100 * 24 * time.Hour), Outcome: model.WatchCheckNoChanges}))
	if all := must(f.s.ListWatchChecks(bg(), w.ID, 50)); len(all) != 1 {
		t.Errorf("after sweep: %d checks", len(all))
	}

	if list := must(f.s.CrewWatches(bg(), crew.ID)); len(list) != 1 || list[0].ID != w.ID {
		t.Errorf("crew watches = %+v", list)
	}
	if list := must(f.s.CrewWatches(bg(), missingID)); len(list) != 0 {
		t.Errorf("other crew = %+v", list)
	}
	mustOK(t, f.s.DeleteWatch(bg(), sc.ID))
	wantNotFound(t, f.s.DeleteWatch(bg(), sc.ID))
}

func TestWatchStatsEstimateSavings(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	sc := must(f.s.SaveSchedule(bg(), "", schedIn("s", crew.ID), nil))
	w := must(f.s.SaveWatch(bg(), sc.ID, fileWatchIn("a.csv")))
	now := time.Now()
	for i, outcome := range []string{model.WatchCheckNoChanges, model.WatchCheckNoChanges, model.WatchCheckStarted, model.WatchCheckBusy} {
		mustOK(t, f.s.RecordWatchCheck(bg(), w.ID, model.WatchCheck{CheckedAt: now.Add(-time.Duration(i) * time.Hour), Outcome: outcome}))
	}
	mustOK(t, f.s.RecordWatchCheck(bg(), w.ID, model.WatchCheck{CheckedAt: now.Add(-40 * 24 * time.Hour), Outcome: model.WatchCheckNoChanges}))
	for _, tokens := range []int{1000, 3000} {
		run := f.run(crew, RunMeta{})
		cost := 0.02
		_, err := f.s.FinishRun(bg(), run.ID, RunResult{Status: model.RunSucceeded, CostUSD: &cost,
			Usage: &model.TokenUsage{PromptTokens: tokens - 100, CompletionTokens: 100}})
		mustOK(t, err)
	}
	st := must(f.s.WatchStats(bg(), w.ID, &crew.ID, 30, now))
	if st.Checks != 4 || st.RunsSkipped != 2 || st.RunsStarted != 1 || st.AvgRunTokens != 2000 || st.EstTokensSaved != 4000 || st.EstCostSavedUSD < 0.039 || st.EstCostSavedUSD > 0.041 {
		t.Errorf("stats = %+v", st)
	}
	if st := must(f.s.WatchStats(bg(), w.ID, nil, 30, now)); st.AvgRunTokens != 0 || st.RunsSkipped != 2 {
		t.Errorf("no crew stats = %+v", st)
	}
}

func TestDryRunEntriesAndLatestFollowups(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	sc := must(f.s.SaveSchedule(bg(), "", schedIn("s", crew.ID), nil))
	real := StateScope{CrewID: crew.ID, ScheduleID: &sc.ID}
	dry := StateScope{CrewID: crew.ID, ScheduleID: &sc.ID, DryRun: true}
	rec := model.LogRecord{Subject: "C1", Action: "followup:due_soon", Details: map[string]any{}}

	entry, _, err := f.s.RecordAction(bg(), dry, nil, rec)
	mustOK(t, err)
	if !entry.DryRun {
		t.Errorf("dry-run entry = %+v", entry)
	}
	if got := must(f.s.CheckAction(bg(), real, "C1", "followup:due_soon", nil)); got != nil {
		t.Errorf("a real check saw a dry-run entry: %+v", got)
	}
	if got := must(f.s.CheckAction(bg(), dry, "C1", "followup:due_soon", nil)); got == nil {
		t.Error("a dry-run check missed its own entry")
	}
	if sent := must(f.s.LatestFollowups(bg(), crew.ID, sc.ID, false)); len(sent) != 0 {
		t.Errorf("dry-run entry counted as sent: %+v", sent)
	}
	if sent := must(f.s.LatestFollowups(bg(), crew.ID, sc.ID, true)); len(sent) != 1 || sent[0].Subject != "C1" {
		t.Errorf("dry-run follow-ups = %+v", sent)
	}
	_, _, err = f.s.RecordAction(bg(), real, nil, rec)
	mustOK(t, err)
	_, _, err = f.s.RecordAction(bg(), real, nil, model.LogRecord{Subject: "C1", Action: "report:weekly", Details: map[string]any{}})
	mustOK(t, err)
	if sent := must(f.s.LatestFollowups(bg(), crew.ID, sc.ID, false)); len(sent) != 1 || sent[0].Action != "followup:due_soon" {
		t.Errorf("follow-ups = %+v", sent)
	}

	// The same idempotency key, first in a dry run and then for real: the real write replaces the dry-run entry
	keyed := model.LogRecord{Subject: "C2", Action: "followup:due_soon", Details: map[string]any{}, IdempotencyKey: "C2/due_soon"}
	first, dup, err := f.s.RecordAction(bg(), dry, nil, keyed)
	mustOK(t, err)
	again, dup2, err := f.s.RecordAction(bg(), dry, nil, keyed)
	mustOK(t, err)
	if dup || !dup2 || again.ID != first.ID || !again.DryRun {
		t.Errorf("dry-run repeat: dup=%v/%v %+v", dup, dup2, again)
	}
	live, dup, err := f.s.RecordAction(bg(), real, nil, keyed)
	mustOK(t, err)
	if dup || live.ID != first.ID || live.DryRun {
		t.Errorf("live after dry run: dup=%v %+v", dup, live)
	}
	if _, dup, _ := f.s.RecordAction(bg(), dry, nil, keyed); !dup {
		t.Error("a dry run overwrote a real entry")
	}

	// A schedule's run whose dry_run input is "true" gets a dry-run token scope; other runs never do
	spec := f.spec(crew)
	spec.Inputs = map[string]string{"dry_run": "true"}
	wo, err := f.s.CreateWorkOrder(bg(), model.WorkOrderInput{Title: "w", CrewID: &crew.ID},
		WorkOrderMeta{TargetName: crew.Name, AreaIDs: crew.AreaIDs, Source: model.SourceSchedule, SourceID: sc.ID})
	mustOK(t, err)
	run, _, err := f.s.CreateRun(bg(), spec, RunMeta{WorkOrderID: wo.ID})
	mustOK(t, err)
	token := must(f.s.CreateRunToken(bg(), run.ID, time.Now().Add(time.Hour)))
	if scope := must(f.s.RunTokenScope(bg(), token)); !scope.DryRun {
		t.Errorf("scope = %+v", scope)
	}
	manual, _, err := f.s.CreateRun(bg(), spec, RunMeta{})
	mustOK(t, err)
	token = must(f.s.CreateRunToken(bg(), manual.ID, time.Now().Add(time.Hour)))
	if scope := must(f.s.RunTokenScope(bg(), token)); scope.DryRun {
		t.Errorf("a manual run claimed dry run: %+v", scope)
	}
	plain := f.run(crew, RunMeta{})
	token = must(f.s.CreateRunToken(bg(), plain.ID, time.Now().Add(time.Hour)))
	if scope := must(f.s.RunTokenScope(bg(), token)); scope.DryRun {
		t.Errorf("plain scope = %+v", scope)
	}
}

func TestConnectionForCrew(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("A"), f.area("B")
	crew := f.simpleCrew("c", a)
	inA := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-0000000000b1", connIn("in-a", "http", a), nil))
	inB := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-0000000000b2", connIn("in-b", "http", b), nil))
	mustOK(t, f.s.SetCrewConnections(bg(), crew.ID, []string{inA.ID, inB.ID}, AllAreas))
	if use := must(f.s.ConnectionForCrew(bg(), inA.ID, crew.ID)); !use.Linked || !use.SharesArea || use.Connection.Name != "in-a" {
		t.Errorf("in-a = %+v", use)
	}
	if use := must(f.s.ConnectionForCrew(bg(), inB.ID, crew.ID)); !use.Linked || use.SharesArea {
		t.Errorf("in-b = %+v", use)
	}
	_, err := f.s.ConnectionForCrew(bg(), missingID, crew.ID)
	wantNotFound(t, err)
}
