package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

type noTools struct{}

func (noTools) ByKey() (map[string]model.Tool, error) { return map[string]model.Tool{}, nil }

type notifier struct{ n atomic.Int64 }

func (c *notifier) Notify() { c.n.Add(1) }

var schemaSeq atomic.Int64

func bg() context.Context { return context.Background() }

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

type env struct {
	t        *testing.T
	st       *store.Store
	l        *launch.Launcher
	s        *Scheduler
	notifier *notifier
	now      time.Time
	area     string
	crew     model.Crew
	runAs    string // user id of the person schedules run as
	runAsCtx context.Context
}

func newEnv(t *testing.T) *env {
	t.Helper()
	schema := fmt.Sprintf("sched_unit_%d_%d", os.Getpid(), schemaSeq.Add(1))
	pool := testdb.New(t, schema)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })
	st := store.New(pool)
	e := &env{t: t, st: st, notifier: &notifier{}, now: time.Now().UTC().Truncate(time.Second)}
	e.l = launch.New(st, noTools{}, e.notifier)
	e.s = New(st, e.l, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.s.now = func() time.Time { return e.now }

	a, err := st.CreateArea(bg(), model.Area{Name: "Alpha"})
	ok(t, err)
	e.area = a.ID
	llm, err := st.CreateLLM(bg(), model.LLM{Name: "L", Model: "openai/sched-model"})
	ok(t, err)
	_, err = st.CreatePrice(bg(), model.ModelPrice{Model: "sched-model"})
	ok(t, err)
	agent, err := st.CreateAgent(bg(), model.Agent{Name: "Ag", Role: "r", Goal: "g", Backstory: "b", LLMID: &llm.ID, MaxIter: 3, Tools: []string{}, AreaIDs: []string{a.ID}})
	ok(t, err)
	task, err := st.CreateTask(bg(), model.Task{Name: "Tk", Description: "Research {company}", ExpectedOutput: "e", AgentID: &agent.ID, AreaIDs: []string{a.ID}})
	ok(t, err)
	e.crew, err = st.CreateCrew(bg(), model.Crew{Name: "Cr", Process: model.ProcessSequential, BudgetUSD: 2, AgentIDs: []string{agent.ID}, TaskIDs: []string{task.ID},
		AreaIDs: []string{a.ID}, Inputs: []model.CrewInput{{Key: "company", Required: true}}})
	ok(t, err)

	e.runAs = e.user("runner", []string{auth.WorkOrdersCreate}, []string{a.ID})
	return e
}

func (e *env) user(name string, perms, areas []string) string {
	e.t.Helper()
	u, err := e.st.CreateUser(bg(), model.UserInput{Kind: auth.KindHuman, Email: name + "@example.com", Name: name, Status: model.UserActive, Permissions: perms, AreaIDs: areas})
	ok(e.t, err)
	return u.ID
}

// schedule saves a schedule that runs as userID and is due `due` before the fixed now (negative = future).
func (e *env) schedule(name string, userID string, mutate func(*model.Schedule), due time.Duration) model.Schedule {
	e.t.Helper()
	in := model.Schedule{Name: name, CrewID: &e.crew.ID, Cron: "0 9 * * *", Timezone: "UTC",
		OverlapPolicy: model.OverlapSkip, Enabled: true, Inputs: map[string]string{"company": "Acme"}, Brief: "daily brief", BudgetUSD: ptr(1.5)}
	if mutate != nil {
		mutate(&in)
	}
	next := e.now.Add(-due)
	ctx := store.WithActor(bg(), store.Actor{ID: userID, Name: "Saver"})
	sc, err := e.st.SaveSchedule(ctx, "", in, &next)
	ok(e.t, err)
	return sc
}

func (e *env) get(id string) model.Schedule {
	e.t.Helper()
	sc, err := e.st.GetSchedule(bg(), id)
	ok(e.t, err)
	return sc
}

func (e *env) workOrders() []model.WorkOrder {
	e.t.Helper()
	items, _, err := e.st.ListWorkOrders(bg(), store.WorkOrderFilter{Scope: store.AllAreas, Limit: 200})
	ok(e.t, err)
	return items
}

func TestTickFiresDueScheduleAndAdvancesIt(t *testing.T) {
	e := newEnv(t)
	sc := e.schedule("daily", e.runAs, nil, time.Minute)

	e.s.Tick(bg())

	wos := e.workOrders()
	if len(wos) != 1 {
		t.Fatalf("%d work orders", len(wos))
	}
	wo := wos[0]
	if wo.Source != model.SourceSchedule || wo.SourceID == nil || *wo.SourceID != sc.ID || wo.Status != "queued" || wo.Brief != "daily brief" ||
		wo.Inputs["company"] != "Acme" || *wo.BudgetUSD != 1.5 || wo.CreatedBy == nil || *wo.CreatedBy != e.runAs {
		t.Errorf("work order = %+v", wo)
	}
	wantTitle := "daily · " + e.now.Format("2006-01-02 15:04 MST")
	if wo.Title != wantTitle {
		t.Errorf("title = %q, want %q", wo.Title, wantTitle)
	}
	got := e.get(sc.ID)
	wantNext, _ := Next(sc.Cron, sc.Timezone, e.now)
	if got.NextRunAt == nil || !got.NextRunAt.Equal(*wantNext) || got.LastRunAt == nil || got.LastWorkOrderID == nil || *got.LastWorkOrderID != wo.ID ||
		got.LastError != nil || !got.Enabled {
		t.Errorf("schedule after = %+v", got)
	}
	if e.notifier.n.Load() != 1 {
		t.Errorf("run dispatcher woken %d times", e.notifier.n.Load())
	}
	// a second tick has nothing to do
	e.s.Tick(bg())
	if len(e.workOrders()) != 1 {
		t.Error("fired twice")
	}
}

func TestTickIgnoresNotDueDisabledAndUnscheduled(t *testing.T) {
	e := newEnv(t)
	e.schedule("future", e.runAs, nil, -time.Hour)
	e.schedule("disabled", e.runAs, func(s *model.Schedule) { s.Enabled = false }, time.Hour)
	e.schedule("exactly-now", e.runAs, nil, 0) // next_run_at == now counts as due
	e.s.Tick(bg())
	wos := e.workOrders()
	if len(wos) != 1 || !strings.HasPrefix(wos[0].Title, "exactly-now") {
		t.Errorf("work orders = %+v", wos)
	}
}

func TestTickMissedScheduleFiresOnce(t *testing.T) {
	e := newEnv(t)
	sc := e.schedule("missed", e.runAs, func(s *model.Schedule) { s.Cron = "*/15 * * * *" }, 72*time.Hour)
	e.s.Tick(bg())
	e.s.Tick(bg())
	if n := len(e.workOrders()); n != 1 {
		t.Fatalf("a schedule missed for 3 days fired %d times, want once", n)
	}
	if got := e.get(sc.ID); got.NextRunAt == nil || !got.NextRunAt.After(e.now) {
		t.Errorf("next = %v", got.NextRunAt)
	}
}

func TestTickOverlapPolicies(t *testing.T) {
	e := newEnv(t)
	skip := e.schedule("skipper", e.runAs, nil, time.Minute)
	e.s.Tick(bg())
	if len(e.workOrders()) != 1 {
		t.Fatal("first fire")
	}
	// make it due again while the first work order is still queued
	due := e.now.Add(-time.Minute)
	_, err := e.st.SaveSchedule(store.WithActor(bg(), store.Actor{ID: e.runAs, Name: "x"}), skip.ID, model.Schedule{Name: "skipper", CrewID: &e.crew.ID,
		Cron: "0 9 * * *", Timezone: "UTC", OverlapPolicy: model.OverlapSkip, Enabled: true, Inputs: map[string]string{"company": "Acme"}}, &due)
	ok(t, err)
	e.s.Tick(bg())
	if n := len(e.workOrders()); n != 1 {
		t.Errorf("skip policy started another work order (%d)", n)
	}
	got := e.get(skip.ID)
	if got.LastError == nil || !strings.HasPrefix(*got.LastError, "Skipped at ") || !strings.Contains(*got.LastError, "still running") || !got.Enabled {
		t.Errorf("skip not recorded: %+v", got.LastError)
	}
	if got.NextRunAt == nil || !got.NextRunAt.After(e.now) {
		t.Error("a skipped fire must still advance the schedule")
	}

	// once the first one finishes, the next fire goes ahead
	run, _, _ := e.st.ListRuns(bg(), store.RunFilter{Scope: store.AllAreas, Limit: 5})
	_, err = e.st.FinishRun(bg(), run[0].ID, store.RunResult{Status: model.RunSucceeded})
	ok(t, err)
	_, err = e.st.SaveSchedule(store.WithActor(bg(), store.Actor{ID: e.runAs, Name: "x"}), skip.ID, model.Schedule{Name: "skipper", CrewID: &e.crew.ID,
		Cron: "0 9 * * *", Timezone: "UTC", OverlapPolicy: model.OverlapSkip, Enabled: true, Inputs: map[string]string{"company": "Acme"}}, &due)
	ok(t, err)
	e.s.Tick(bg())
	if n := len(e.workOrders()); n != 2 {
		t.Errorf("after the first finished, %d work orders", n)
	}
	if got := e.get(skip.ID); got.LastError != nil {
		t.Errorf("error should clear on success: %q", *got.LastError)
	}

	// allow: overlap is permitted
	allow := e.schedule("allower", e.runAs, func(s *model.Schedule) { s.OverlapPolicy = model.OverlapAllow }, time.Minute)
	e.s.Tick(bg())
	before := len(e.workOrders())
	due2 := e.now.Add(-time.Minute)
	_, err = e.st.SaveSchedule(store.WithActor(bg(), store.Actor{ID: e.runAs, Name: "x"}), allow.ID, model.Schedule{Name: "allower", CrewID: &e.crew.ID,
		Cron: "0 9 * * *", Timezone: "UTC", OverlapPolicy: model.OverlapAllow, Enabled: true, Inputs: map[string]string{"company": "Acme"}}, &due2)
	ok(t, err)
	e.s.Tick(bg())
	if n := len(e.workOrders()); n != before+1 {
		t.Errorf("allow policy: %d -> %d work orders", before, n)
	}
}

func TestTickPermanentErrorsPauseTheSchedule(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(e *env) model.Schedule
		wantInErr []string
		wantDraft bool
	}{
		{"run-as user disabled", func(e *env) model.Schedule {
			e.user("admin", []string{auth.AdminUsers}, nil)
			uid := e.user("gone", []string{auth.WorkOrdersCreate}, []string{e.area})
			sc := e.schedule("s", uid, nil, time.Minute)
			_, err := e.st.UpdateUser(bg(), uid, model.UserInput{Kind: auth.KindHuman, Email: "gone@example.com", Name: "gone", Status: model.UserDisabled})
			ok(e.t, err)
			return sc
		}, []string{"Paused:", "Nobody to run as"}, false},
		{"run-as user deleted", func(e *env) model.Schedule {
			e.user("admin", []string{auth.AdminUsers}, nil)
			uid := e.user("gone", []string{auth.WorkOrdersCreate}, []string{e.area})
			sc := e.schedule("s", uid, nil, time.Minute)
			ok(e.t, e.st.DeleteUser(bg(), uid))
			return sc
		}, []string{"Paused:", "no longer exists"}, false},
		{"run-as lost the permission", func(e *env) model.Schedule {
			uid := e.user("limited", []string{auth.CrewsView}, []string{e.area})
			return e.schedule("s", uid, nil, time.Minute)
		}, []string{"Paused:", auth.WorkOrdersCreate}, false},
		{"target outside run-as areas", func(e *env) model.Schedule {
			other, err := e.st.CreateArea(bg(), model.Area{Name: "Beta"})
			ok(e.t, err)
			uid := e.user("elsewhere", []string{auth.WorkOrdersCreate}, []string{other.ID})
			return e.schedule("s", uid, nil, time.Minute)
		}, []string{"Paused:", "no such crew"}, false},
		{"crew cannot run (missing input)", func(e *env) model.Schedule {
			return e.schedule("s", e.runAs, func(s *model.Schedule) { s.Inputs = map[string]string{} }, time.Minute)
		}, []string{"Paused:", "inputs.company"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			sc := tt.setup(e)
			e.s.Tick(bg())
			got := e.get(sc.ID)
			if got.Enabled {
				t.Error("schedule must be paused")
			}
			if got.LastError == nil {
				t.Fatal("no error recorded")
			}
			for _, want := range tt.wantInErr {
				if !strings.Contains(*got.LastError, want) {
					t.Errorf("last_error %q lacks %q", *got.LastError, want)
				}
			}
			if tt.wantDraft {
				if got.LastWorkOrderID == nil {
					t.Error("the kept draft should be linked so the person can fix it")
				}
			} else if n := len(e.workOrders()); n != 0 {
				t.Errorf("%d work orders created", n)
			}
			// a paused schedule does not fire again
			e.s.Tick(bg())
			e.s.Tick(bg())
			if e.get(sc.ID).Enabled {
				t.Error("re-enabled by itself")
			}
		})
	}
}

func TestTickTransientErrorDoesNotPause(t *testing.T) {
	e := newEnv(t)
	sc := e.schedule("flaky", e.runAs, func(s *model.Schedule) { s.OverlapPolicy = model.OverlapAllow }, time.Minute)
	// simulate a database fault only the work-order insert hits
	_, err := e.st.Pool().Exec(bg(), `ALTER TABLE work_orders RENAME TO work_orders_hidden`)
	ok(t, err)
	e.s.Tick(bg())
	_, err = e.st.Pool().Exec(bg(), `ALTER TABLE work_orders_hidden RENAME TO work_orders`)
	ok(t, err)

	got := e.get(sc.ID)
	if !got.Enabled {
		t.Error("a database hiccup must not pause the schedule")
	}
	if got.LastError == nil || strings.HasPrefix(*got.LastError, "Paused") {
		t.Errorf("last_error = %v", got.LastError)
	}
	if got.NextRunAt == nil || !got.NextRunAt.After(e.now) {
		t.Error("schedule should still move on to its next time")
	}
	// next time round it works and the error clears
	e.now = *got.NextRunAt
	e.s.Tick(bg())
	if n := len(e.workOrders()); n != 1 {
		t.Errorf("recovered fire: %d work orders", n)
	}
	if got := e.get(sc.ID); got.LastError != nil || !got.Enabled {
		t.Errorf("after recovery = %+v", got)
	}
}

func TestTickOverlapCheckFailureIsRecordedWithoutPausing(t *testing.T) {
	e := newEnv(t)
	sc := e.schedule("s", e.runAs, nil, time.Minute) // skip policy checks work_orders first
	_, err := e.st.Pool().Exec(bg(), `ALTER TABLE work_orders RENAME TO work_orders_hidden`)
	ok(t, err)
	e.s.Tick(bg())
	_, err = e.st.Pool().Exec(bg(), `ALTER TABLE work_orders_hidden RENAME TO work_orders`)
	ok(t, err)
	got := e.get(sc.ID)
	if !got.Enabled || got.LastError == nil || !strings.Contains(*got.LastError, "check for a running work order") {
		t.Errorf("schedule = enabled:%v err:%v", got.Enabled, got.LastError)
	}
	if len(e.workOrders()) != 0 {
		t.Error("no work order expected")
	}
}

func TestTickPausesScheduleWithBrokenCronOrZone(t *testing.T) {
	for name, set := range map[string]string{
		"bad cron":       `UPDATE schedules SET cron = 'not a cron'`,
		"too frequent":   `UPDATE schedules SET cron = '* * * * *'`,
		"removed zone":   `UPDATE schedules SET timezone = 'Mars/Olympus'`,
		"@every in cron": `UPDATE schedules SET cron = '@every 1m'`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			sc := e.schedule("s", e.runAs, nil, time.Minute)
			_, err := e.st.Pool().Exec(bg(), set)
			ok(t, err)
			e.s.Tick(bg())
			got := e.get(sc.ID)
			if got.Enabled || got.LastError == nil || !strings.HasPrefix(*got.LastError, "Paused: ") {
				t.Errorf("schedule = enabled:%v err:%v", got.Enabled, got.LastError)
			}
			if len(e.workOrders()) != 0 {
				t.Error("fired despite the broken schedule")
			}
		})
	}
}

func TestTickConcurrentSchedulersFireOnce(t *testing.T) {
	e := newEnv(t)
	const n = 5
	for i := 0; i < n; i++ {
		e.schedule(fmt.Sprintf("s%d", i), e.runAs, nil, time.Minute)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 6; i++ {
		other := New(e.st, e.l, slog.New(slog.NewTextHandler(io.Discard, nil)))
		other.now = func() time.Time { return e.now }
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			other.Tick(bg())
		}()
	}
	close(start)
	wg.Wait()
	if got := len(e.workOrders()); got != n {
		t.Errorf("%d work orders for %d schedules across 6 processes", got, n)
	}
}

func TestTickBatchSize(t *testing.T) {
	e := newEnv(t)
	const total = batchSize + 3
	for i := 0; i < total; i++ {
		e.schedule(fmt.Sprintf("b%02d", i), e.runAs, func(s *model.Schedule) { s.OverlapPolicy = model.OverlapAllow }, time.Duration(total-i)*time.Minute)
	}
	e.s.Tick(bg())
	if n := len(e.workOrders()); n != batchSize {
		t.Fatalf("first tick fired %d, want the batch size %d", n, batchSize)
	}
	e.s.Tick(bg())
	if n := len(e.workOrders()); n != total {
		t.Errorf("second tick: %d of %d", n, total)
	}
}

func TestTickSurvivesStoreErrors(t *testing.T) {
	e := newEnv(t)
	e.schedule("s", e.runAs, nil, time.Minute)
	ctx, cancel := context.WithCancel(bg())
	cancel()
	e.s.Tick(ctx) // canceled: returns quietly
	_, err := e.st.Pool().Exec(bg(), `ALTER TABLE schedules RENAME TO schedules_hidden`)
	ok(t, err)
	e.s.Tick(bg()) // listing fails: logged, no panic
	_, err = e.st.Pool().Exec(bg(), `ALTER TABLE schedules_hidden RENAME TO schedules`)
	ok(t, err)
	if len(e.workOrders()) != 0 {
		t.Error("nothing should have fired")
	}
	e.s.Tick(bg())
	if len(e.workOrders()) != 1 {
		t.Error("the schedule must still fire once the store recovers")
	}
}

func TestLaunchTitleInScheduleTimeZoneAndActor(t *testing.T) {
	e := newEnv(t)
	p, err := e.l.RunAs(bg(), &e.runAs)
	ok(t, err)
	sc := e.schedule("tokyo", e.runAs, func(s *model.Schedule) { s.Timezone = "Asia/Tokyo" }, time.Minute)
	now := time.Date(2026, 10, 3, 16, 30, 0, 0, time.UTC)
	wo, err := e.s.Launch(bg(), p, sc, now)
	ok(t, err)
	if wo.Title != "tokyo · 2026-10-04 01:30 JST" {
		t.Errorf("title = %q", wo.Title)
	}
	revs, _, err := e.st.ListRevisions(bg(), store.RevisionFilter{EntityType: "work_order", EntityID: wo.ID, Types: []string{"work_order"}, Scope: store.AllAreas, Limit: 5})
	ok(t, err)
	if len(revs) == 0 || revs[len(revs)-1].ActorName != "runner (schedule tokyo)" {
		t.Errorf("actor = %+v", revs)
	}
	// an unknown zone falls back to UTC rather than failing
	bad := sc
	bad.Timezone = "Mars/Olympus"
	bad.Name = "badzone"
	wo, err = e.s.Launch(bg(), p, bad, now)
	ok(t, err)
	if wo.Title != "badzone · 2026-10-03 16:30 UTC" {
		t.Errorf("title = %q", wo.Title)
	}
}

func TestLaunchScheduleAndErrors(t *testing.T) {
	e := newEnv(t)
	p, _ := e.l.RunAs(bg(), &e.runAs)
	sc := model.Schedule{Name: "t", CrewID: &e.crew.ID, Inputs: map[string]string{"company": "Acme"}}
	wo, err := e.s.Launch(bg(), p, sc, e.now)
	ok(t, err)
	if wo.CrewID == nil || *wo.CrewID != e.crew.ID || wo.Status != "queued" || wo.Source != model.SourceSchedule {
		t.Errorf("wo = %+v", wo)
	}
	// permission failures surface as ForbiddenError, with the draft not created
	limited := &auth.Principal{UserID: e.runAs, Name: "x", Permissions: map[string]bool{}}
	_, err = e.s.Launch(bg(), limited, sc, e.now)
	var fe *launch.ForbiddenError
	if !errors.As(err, &fe) {
		t.Errorf("err = %v", err)
	}
}

func TestPermanentAndDescribe(t *testing.T) {
	forbidden := &launch.ForbiddenError{Permission: "work_orders.create"}
	problems := &launch.ProblemsError{Message: "This cannot run yet.", Details: []string{"a", "b"}}
	wrapped := fmt.Errorf("outer: %w", forbidden)
	tests := []struct {
		name string
		err  error
		perm bool
		desc string
	}{
		{"forbidden", forbidden, true, "the person it runs as no longer has the work_orders.create permission."},
		{"wrapped forbidden", wrapped, true, "the person it runs as no longer has the work_orders.create permission."},
		{"problems", problems, true, "This cannot run yet. a b"},
		{"not found", store.ErrNotFound, true, "its task or crew is outside the areas of the person it runs as."},
		{"wrapped not found", fmt.Errorf("x: %w", store.ErrNotFound), true, "its task or crew is outside the areas of the person it runs as."},
		{"active work order is transient", store.ErrWorkOrderActive, false, store.ErrWorkOrderActive.Error()},
		{"plain error", errors.New("connection reset"), false, "connection reset"},
		{"conflict", &store.ConflictError{Message: "taken"}, false, "taken"},
		{"context canceled", context.Canceled, false, "context canceled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := permanent(tt.err); got != tt.perm {
				t.Errorf("permanent = %v", got)
			}
			if got := describe(tt.err); got != tt.desc {
				t.Errorf("describe = %q, want %q", got, tt.desc)
			}
		})
	}
	if permanent(nil) {
		t.Error("nil is not permanent")
	}
}

func TestNotifyDoesNotBlockAndRunLoopFires(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 5; i++ {
		e.s.Notify() // buffered once; further calls must not block
	}
	if len(e.s.wake) != 1 {
		t.Errorf("wake buffer = %d", len(e.s.wake))
	}
	<-e.s.wake

	e.schedule("looped", e.runAs, nil, time.Minute)
	ctx, cancel := context.WithCancel(bg())
	done := make(chan struct{})
	go func() { e.s.Run(ctx); close(done) }()
	deadline := time.After(10 * time.Second)
	for len(e.workOrders()) == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("Run did not fire the due schedule")
		case <-time.After(50 * time.Millisecond):
		}
	}
	// a schedule saved later is picked up on Notify without waiting for the 20s tick
	e.schedule("later", e.runAs, nil, time.Minute)
	e.s.Notify()
	for len(e.workOrders()) < 2 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("Notify did not wake the loop")
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop when its context ended")
	}
}
