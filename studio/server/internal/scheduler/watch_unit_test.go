package scheduler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/connections"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/watch"
)

type watchEnv struct {
	*env
	dir string
}

func newWatchEnv(t *testing.T) *watchEnv {
	e := newEnv(t)
	dir := t.TempDir()
	e.s.SetWatches(watch.New(e.st, connections.New(e.st, nil), dir))
	return &watchEnv{env: e, dir: dir}
}

func (w *watchEnv) writeCSV(name, body string) {
	w.t.Helper()
	ok(w.t, os.MkdirAll(filepath.Join(w.dir, "watch"), 0o755))
	ok(w.t, os.WriteFile(filepath.Join(w.dir, "watch", name), []byte(body), 0o644))
}

func (w *watchEnv) fileWatch(scheduleID string, edit func(*model.WatchInput)) model.Watch {
	w.t.Helper()
	in := model.WatchInput{Source: model.WatchSource{Kind: model.WatchSourceWorkspaceFile, File: "watch/courses.csv"},
		IDField: "id", TrackedFields: []string{"due", "status"}}
	if edit != nil {
		edit(&in)
	}
	clean, err := model.NormalizeWatch(in)
	ok(w.t, err)
	saved, err := w.st.SaveWatch(bg(), scheduleID, clean)
	ok(w.t, err)
	return saved
}

// due makes the schedule due again and ticks.
func (w *watchEnv) tickAgain(scheduleID string) {
	w.t.Helper()
	w.now = w.now.Add(time.Minute) // checks are listed newest first
	_, err := w.st.Pool().Exec(bg(), `UPDATE schedules SET next_run_at = $2 WHERE id = $1`, scheduleID, w.now.Add(-time.Minute))
	ok(w.t, err)
	w.s.Tick(bg())
}

func (w *watchEnv) checks(watchID string) []model.WatchCheck {
	w.t.Helper()
	items, err := w.st.ListWatchChecks(bg(), watchID, 50)
	ok(w.t, err)
	return items
}

func decodeChanges(t *testing.T, wo model.WorkOrder) watch.Changes {
	t.Helper()
	var ch watch.Changes
	ok(t, json.Unmarshal([]byte(wo.Inputs[model.WatchInputChanges]), &ch))
	return ch
}

func allow(s *model.Schedule) { s.OverlapPolicy = model.OverlapAllow }

func TestWatchedScheduleRunsOnlyWhenSomethingChanged(t *testing.T) {
	w := newWatchEnv(t)
	w.writeCSV("courses.csv", "id,due,status\nC1,2026-10-10,Open\nC2,2026-10-12,Open\n")
	sc := w.schedule("courses", w.runAs, allow, time.Minute)
	wt := w.fileWatch(sc.ID, nil)

	// First check: everything is added, previous_check_at is null, and a work order starts
	w.s.Tick(bg())
	wos := w.workOrders()
	if len(wos) != 1 {
		t.Fatalf("%d work orders after the first check", len(wos))
	}
	firstID := wos[0].ID
	first := decodeChanges(t, wos[0])
	if len(first.Added) != 2 || first.PreviousCheckAt != nil || first.Source != "workspace:watch/courses.csv" ||
		wos[0].Inputs[model.WatchInputDryRun] != "true" || wos[0].Inputs["company"] != "Acme" {
		t.Errorf("first changes = %+v inputs %v", first, wos[0].Inputs)
	}
	got, err := w.st.WatchOfSchedule(bg(), sc.ID)
	ok(t, err)
	if got.SnapshotItems != 2 || got.SnapshotAt == nil || got.LastChangedAt == nil || got.LastCheckedAt == nil {
		t.Errorf("after first check: %+v", got)
	}

	// Nothing changed: no work order, a "no changes" check, and the schedule shows no problem
	w.tickAgain(sc.ID)
	if n := len(w.workOrders()); n != 1 {
		t.Fatalf("%d work orders after an unchanged check", n)
	}
	checks := w.checks(wt.ID)
	if len(checks) != 2 || checks[0].Outcome != model.WatchCheckNoChanges || checks[0].Unchanged != 2 || checks[0].Message != "checked, no changes" ||
		checks[1].Outcome != model.WatchCheckStarted || checks[1].WorkOrderID == nil || checks[1].Added != 2 {
		t.Errorf("checks = %+v", checks)
	}
	if s := w.get(sc.ID); s.LastError != nil {
		t.Errorf("last error = %v", *s.LastError)
	}

	// A change: the new work order gets exactly that change, measured from the previous check
	w.writeCSV("courses.csv", "id,due,status\nC1,2026-10-20,Open\nC3,2026-11-01,Open\n")
	w.tickAgain(sc.ID)
	wos = w.workOrders()
	if len(wos) != 2 {
		t.Fatalf("%d work orders after a change", len(wos))
	}
	latest := wos[0]
	if latest.ID == firstID {
		latest = wos[1]
	}
	ch := decodeChanges(t, latest)
	if ch.PreviousCheckAt == nil || len(ch.Added) != 1 || ch.Added[0].ID != "C3" || len(ch.Removed) != 1 || ch.Removed[0].ID != "C2" ||
		len(ch.Changed) != 1 || ch.Changed[0].Fields["due"].New != "2026-10-20" {
		t.Errorf("second changes = %+v", ch)
	}

	stats, err := w.st.WatchStats(bg(), wt.ID, sc.CrewID, 30, w.now.Add(time.Hour))
	ok(t, err)
	if stats.Checks != 3 || stats.RunsStarted != 2 || stats.RunsSkipped != 1 {
		t.Errorf("stats = %+v", stats)
	}
}

func TestWatchErrorsAndBusyTicksAreRecorded(t *testing.T) {
	w := newWatchEnv(t)
	sc := w.schedule("missing", w.runAs, nil, time.Minute)
	wt := w.fileWatch(sc.ID, nil)

	w.s.Tick(bg())
	s := w.get(sc.ID)
	if len(w.workOrders()) != 0 || s.LastError == nil || !strings.Contains(*s.LastError, "Watch: there is no file watch/courses.csv") || !s.Enabled {
		t.Errorf("schedule after a failed check = %+v", s)
	}
	got, _ := w.st.WatchOfSchedule(bg(), sc.ID)
	if got.LastError == nil || w.checks(wt.ID)[0].Outcome != model.WatchCheckError {
		t.Errorf("watch after a failed check = %+v", got)
	}

	// With overlap "skip" and a work order still queued, the source is not even read
	w.writeCSV("courses.csv", "id,due,status\nC1,2026-10-10,Open\n")
	w.tickAgain(sc.ID)
	if len(w.workOrders()) != 1 {
		t.Fatal("no work order once the file exists")
	}
	got, _ = w.st.WatchOfSchedule(bg(), sc.ID)
	if got.LastError != nil {
		t.Errorf("a good check keeps the old error: %v", *got.LastError)
	}
	w.writeCSV("courses.csv", "id,due,status\nC1,2026-10-11,Open\n")
	w.tickAgain(sc.ID)
	checks := w.checks(wt.ID)
	if len(w.workOrders()) != 1 || checks[0].Outcome != model.WatchCheckBusy {
		t.Errorf("busy tick: %d work orders, checks %+v", len(w.workOrders()), checks[0])
	}

	// A crew that can no longer run makes the check fail without saving the snapshot
	w.writeCSV("courses.csv", "id,due,status\nC9,2026-10-11,Open\n")
	sc2 := w.schedule("no-company", w.runAs, func(s *model.Schedule) { allow(s); s.Inputs = map[string]string{} }, time.Minute)
	wt2 := w.fileWatch(sc2.ID, nil)
	w.s.Tick(bg())
	c2 := w.checks(wt2.ID)
	got2, _ := w.st.WatchOfSchedule(bg(), sc2.ID)
	if len(c2) != 1 || c2[0].Outcome != model.WatchCheckError || !strings.Contains(c2[0].Message, "could not be started") || got2.SnapshotItems != 0 {
		t.Errorf("launch failure: %+v snapshot %d", c2, got2.SnapshotItems)
	}
}

func TestWatchFollowupLadderAndDryRun(t *testing.T) {
	w := newWatchEnv(t)
	today := w.now.Format(time.DateOnly)
	w.writeCSV("courses.csv", "id,due,status\nC1,"+today+",Open\nC2,2099-01-01,Open\nC3,"+today+",Done\n")
	sc := w.schedule("ladder", w.runAs, allow, time.Minute)
	ladder := &model.FollowupLadder{DateField: "due", SkipWhen: map[string][]string{"status": {"Done"}},
		Steps: []model.FollowupStep{{Name: "due_today", Offset: "0d", Notify: "owner"}}}
	wt := w.fileWatch(sc.ID, func(in *model.WatchInput) { in.RunWhen = model.WatchRunChangedOrFollowups; in.Followups = ladder })

	w.s.Tick(bg()) // first check: everything added
	w.tickAgain(sc.ID)
	wos := w.workOrders()
	if len(wos) != 2 {
		t.Fatalf("a due follow-up should start a run on its own: %d work orders", len(wos))
	}
	var f watch.Followups
	ok(t, json.Unmarshal([]byte(wos[0].Inputs[model.WatchInputFollowups]), &f))
	if len(f.Due) != 1 || f.Due[0].Item != "C1" || f.Due[0].Action != "followup:due_today" || !f.DryRun || f.HowToRecord == "" {
		t.Fatalf("followups_due = %+v", f)
	}

	// The crew records it (a dry run, so the entry is a dry-run one): no longer due, nothing to do
	sched := sc.ID
	dry := store.StateScope{CrewID: w.crew.ID, ScheduleID: &sched, DryRun: true}
	_, _, err := w.st.RecordAction(bg(), dry, nil, model.LogRecord{Subject: "C1", Action: "followup:due_today", Details: map[string]any{}})
	ok(t, err)
	w.tickAgain(sc.ID)
	if n := len(w.workOrders()); n != 2 {
		t.Fatalf("recorded follow-up still started a run: %d", n)
	}

	// Turning dry run off: dry-run entries do not count as sent, so the step is due again
	w.fileWatch(sc.ID, func(in *model.WatchInput) {
		in.RunWhen, in.Followups, in.DryRun = model.WatchRunChangedOrFollowups, ladder, ptr(false)
	})
	w.tickAgain(sc.ID)
	wos = w.workOrders()
	if len(wos) != 3 || wos[0].Inputs[model.WatchInputDryRun] != "false" {
		t.Fatalf("after turning dry run off: %d work orders, inputs %v", len(wos), wos[0].Inputs)
	}
	if checks := w.checks(wt.ID); checks[0].FollowupsDue != 1 || checks[0].Outcome != model.WatchCheckStarted {
		t.Errorf("checks = %+v", checks[0])
	}
}

func TestRunNowWithAWatch(t *testing.T) {
	w := newWatchEnv(t)
	w.writeCSV("courses.csv", "id,due,status\nC1,2026-10-10,Open\n")
	sc := w.schedule("now", w.runAs, allow, -time.Hour)
	wt := w.fileWatch(sc.ID, nil)
	p, err := w.l.RunAs(bg(), sc.RunAs)
	ok(t, err)

	for i := 0; i < 2; i++ { // the second time nothing changed, but Run now still starts
		wo, err := w.s.Launch(bg(), p, sc, w.now.Add(time.Duration(i)*time.Minute))
		ok(t, err)
		if wo.Inputs[model.WatchInputChanges] == "" {
			t.Fatalf("run now %d without changes input: %v", i, wo.Inputs)
		}
	}
	checks := w.checks(wt.ID)
	if len(checks) != 2 || !strings.HasPrefix(checks[0].Message, "Run now by runner: started by hand") || checks[0].Unchanged != 1 {
		t.Errorf("checks = %+v", checks)
	}

	ok(t, os.Remove(filepath.Join(w.dir, "watch", "courses.csv")))
	_, err = w.s.Launch(bg(), p, sc, w.now)
	var problems *launch.ProblemsError
	if !errors.As(err, &problems) || !strings.Contains(strings.Join(problems.Details, " "), "there is no file") {
		t.Errorf("run now with a broken source: %v", err)
	}
}

func TestHTTPWatchNeedsAUsableConnection(t *testing.T) {
	w := newWatchEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = rw.Write([]byte(`{"items":[{"key":"A","status":"Open"}]}`))
	}))
	defer srv.Close()
	conn, err := w.st.CreateConnection(bg(), "99999999-0000-4000-8000-0000000000a1",
		model.ConnectionInput{Name: "API", Kind: "http", Config: map[string]string{"base_url": srv.URL}, AreaIDs: []string{w.area}}, nil)
	ok(t, err)
	sc := w.schedule("api", w.runAs, allow, time.Minute)
	in, err := model.NormalizeWatch(model.WatchInput{ConnectionID: &conn.ID, Source: model.WatchSource{Kind: model.WatchSourceHTTPJSON, Path: "/x", ItemsPath: "items"},
		IDField: "key", TrackedFields: []string{"status"}})
	ok(t, err)
	wt, err := w.st.SaveWatch(bg(), sc.ID, in)
	ok(t, err)
	if wt.ConnectionName != "API" {
		t.Errorf("connection name = %q", wt.ConnectionName)
	}

	w.s.Tick(bg())
	if c := w.checks(wt.ID)[0]; c.Outcome != model.WatchCheckError || !strings.Contains(c.Message, "may not use the connection API") {
		t.Fatalf("unlinked connection: %+v", c)
	}
	ok(t, w.st.SetCrewConnections(bg(), w.crew.ID, []string{conn.ID}, store.AllAreas))
	w.tickAgain(sc.ID)
	if c := w.checks(wt.ID)[0]; c.Outcome != model.WatchCheckStarted || c.Added != 1 {
		t.Fatalf("linked connection: %+v", c)
	}
	// A connection in use by a watch cannot be deleted
	if err := w.st.DeleteConnection(bg(), conn.ID); err == nil {
		t.Error("deleted a connection a watch uses")
	}
	ok(t, w.st.DeleteWatch(bg(), sc.ID))
	if err := w.st.DeleteWatch(bg(), sc.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}
