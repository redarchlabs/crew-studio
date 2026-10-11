package runner

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// The fake runner reports what it got under state_api (the token travels on stdin, not in the environment),
// then waits for the test to record a state event before finishing.
const stateScript = `
import json, os, sys, time
req = json.load(sys.stdin)
api = req.get("state_api") or {}
def emit(t, p): print(json.dumps({"type": t, "payload": p}), flush=True)
emit("crew_started", {"crew_name": "x", "url": api.get("url", ""), "token": api.get("token", ""),
  "token_in_env": any(v == api.get("token") for v in os.environ.values())})
for _ in range(200):
    if os.path.exists("go"):
        break
    time.sleep(0.05)
emit("crew_completed", {"output": "ok", "cost_usd": 0})
`

func startedPayload(t *testing.T, h harness, runID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		events, _ := h.store.ListEvents(context.Background(), runID, 0, 100)
		for _, e := range events {
			if e.Type == "crew_started" {
				return e.Payload
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("runner never reported its request")
	return nil
}

func TestRunGetsATokenOnStdinThatEndsWithTheRun(t *testing.T) {
	h := newHarness(t)
	m := h.manager(t, stateScript)
	m.proc.StateURL = "http://127.0.0.1:9"
	ended := make(chan string, 1)
	m.OnRunEnd(func(runID string) { ended <- runID })
	startManager(t, m)
	run := h.queue(t)

	got := startedPayload(t, h, run.ID)
	token, _ := got["token"].(string)
	if got["url"] != "http://127.0.0.1:9/api/run-state" || len(token) < 40 || got["token_in_env"] != false {
		t.Fatalf("runner saw %+v", got)
	}
	ctx := context.Background()
	scope, err := h.store.RunTokenScope(ctx, token)
	if err != nil || scope.RunID != run.ID || scope.CrewID != h.crew.ID {
		t.Fatalf("scope = %+v, %v", scope, err)
	}
	if !scope.ExpiresAt.After(time.Now().Add(time.Minute)) {
		t.Errorf("token should last for the run's time limit, expires %v", scope.ExpiresAt)
	}

	// While the run executes, state events go through its recorder
	if err := m.RecordStateEvent(ctx, run.ID, map[string]any{"message": "Notes: read weekly-report v1"}); err != nil {
		t.Fatal(err)
	}
	if err := touch(m.pathsFor(run.ID).workspace + "/go"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, h, run.ID, model.RunSucceeded)
	// Whatever kept per-run state (memory's vector cache) is told the run ended
	select {
	case id := <-ended:
		if id != run.ID {
			t.Errorf("ended = %s, want %s", id, run.ID)
		}
	case <-time.After(10 * time.Second):
		t.Error("run end hook not called")
	}

	scope, err = h.store.RunTokenScope(ctx, token)
	if err != nil || scope.ExpiresAt.After(time.Now().Add(runTokenGrace)) {
		t.Errorf("after the run the token lasts only the grace period: %+v, %v", scope, err)
	}
	// Afterwards (the grace period) they are appended after the last event
	waitIdle(t, m)
	if err := m.RecordStateEvent(ctx, run.ID, map[string]any{"message": "Log: recorded x"}); err != nil {
		t.Fatal(err)
	}
	types := eventTypes(t, h.store, run.ID)
	if n := countOf(types, StateEventType); n != 2 || types[len(types)-1] != StateEventType {
		t.Errorf("events = %v", types)
	}
	var conflict *store.ConflictError
	if err := m.RecordStateEvent(ctx, "99999999-0000-4000-8000-000000000000", nil); !errors.As(err, &conflict) {
		t.Errorf("missing run: %v", err)
	}
}

func TestNoStateURLMeansNoToken(t *testing.T) {
	h := newHarness(t)
	m := h.manager(t, stateScript)
	startManager(t, m)
	run := h.queue(t)
	if got := startedPayload(t, h, run.ID); got["token"] != "" {
		t.Errorf("token without a state URL: %+v", got)
	}
	_ = touch(m.pathsFor(run.ID).workspace + "/go")
	waitFor(t, h, run.ID, model.RunSucceeded)
}

func countOf(items []string, want string) int {
	n := 0
	for _, s := range items {
		if s == want {
			n++
		}
	}
	return n
}

func waitIdle(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for m.ActiveCount() > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if m.ActiveCount() > 0 {
		t.Fatal("runs still active")
	}
}

func touch(path string) error {
	return os.WriteFile(path, nil, 0o600)
}
