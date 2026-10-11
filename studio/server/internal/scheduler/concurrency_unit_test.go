package scheduler

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crewstudio/internal/connections"
	"crewstudio/internal/model"
	"crewstudio/internal/watch"
)

// slowSource blocks on "watch/slow.csv" until released (or its context ends) and answers at once otherwise.
type slowSource struct {
	release chan struct{}
	started chan struct{}
	waits   *atomic.Int32
}

func (s slowSource) Fetch(ctx context.Context, req watch.Request) (watch.Fetched, error) {
	if req.Def.Source.File == "watch/slow.csv" {
		s.waits.Add(1)
		close(s.started)
		select {
		case <-s.release:
		case <-ctx.Done():
			return watch.Fetched{}, ctx.Err()
		}
	}
	return watch.Fetched{Label: req.Def.Source.File, Items: []watch.Item{{ID: "A", Fields: map[string]any{"status": "Open"}}}}, nil
}

func fileWatchOf(t *testing.T, e *env, scheduleID, file string) model.Watch {
	t.Helper()
	clean, err := model.NormalizeWatch(model.WatchInput{Source: model.WatchSource{Kind: model.WatchSourceWorkspaceFile, File: file},
		IDField: "id", TrackedFields: []string{"status"}})
	ok(t, err)
	w, err := e.st.SaveWatch(bg(), scheduleID, clean)
	ok(t, err)
	return w
}

func TestASlowWatchDoesNotHoldUpOtherSchedules(t *testing.T) {
	e := newEnv(t)
	src := slowSource{release: make(chan struct{}), started: make(chan struct{}), waits: &atomic.Int32{}}
	e.s.SetWatches(watch.New(e.st, connections.New(e.st, nil), t.TempDir()).WithSource(model.WatchSourceWorkspaceFile, src))
	slow := e.schedule("slow", e.runAs, allow, 2*time.Minute) // due first
	fast := e.schedule("fast", e.runAs, allow, time.Minute)
	fileWatchOf(t, e, slow.ID, "watch/slow.csv")
	fastWatch := fileWatchOf(t, e, fast.ID, "watch/fast.csv")

	done := make(chan struct{})
	go func() {
		e.s.Tick(bg())
		close(done)
	}()
	<-src.started
	deadline := time.Now().Add(10 * time.Second)
	for {
		checks, err := e.st.ListWatchChecks(bg(), fastWatch.ID, 5)
		ok(t, err)
		if len(checks) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fast schedule waited for the slow one")
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("the tick ended before the slow schedule finished")
	default:
	}
	close(src.release)
	<-done
	if n := len(e.workOrders()); n != 2 {
		t.Errorf("work orders = %d, want 2", n)
	}
}

func TestAHangingWatchTimesOut(t *testing.T) {
	old := watchFetchTimeout
	watchFetchTimeout = 200 * time.Millisecond
	t.Cleanup(func() { watchFetchTimeout = old })
	e := newEnv(t)
	src := slowSource{release: make(chan struct{}), started: make(chan struct{}), waits: &atomic.Int32{}}
	e.s.SetWatches(watch.New(e.st, connections.New(e.st, nil), t.TempDir()).WithSource(model.WatchSourceWorkspaceFile, src))
	sc := e.schedule("slow", e.runAs, allow, time.Minute)
	w := fileWatchOf(t, e, sc.ID, "watch/slow.csv")
	e.s.Tick(bg())
	checks, err := e.st.ListWatchChecks(bg(), w.ID, 5)
	ok(t, err)
	if len(checks) != 1 || checks[0].Outcome != model.WatchCheckError || !strings.Contains(checks[0].Message, "did not answer within") {
		t.Errorf("checks = %+v", checks)
	}
}
