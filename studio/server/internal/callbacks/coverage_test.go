package callbacks

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/storage"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRunLoopsExitOnCancelledContext(t *testing.T) {
	st := store.New(testdb.New(t, "callbacks_cov_test"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, run := range map[string]func(context.Context){
		"deliverer":    New(st, quietLog()).Run,
		"housekeeping": NewHousekeeping(st, storage.NewMemory(), quietLog()).Run,
	} {
		done := make(chan struct{})
		go func() { run(ctx); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Errorf("%s loop did not stop on a cancelled context", name)
		}
	}
	// With no file storage, housekeeping only purges sessions
	NewHousekeeping(st, nil, quietLog()).Tick(context.Background())
}

func TestBodyIncludesWorkOrderAndClipsOutput(t *testing.T) {
	st := store.New(testdb.New(t, "callbacks_cov_test"))
	ctx := context.Background()
	crews, _ := st.ListCrews(ctx, store.AllAreas)
	run, _, err := st.CreateRun(ctx, model.RunSpec{Crew: crews[0], BudgetUSD: 1}, store.RunMeta{})
	if err != nil {
		t.Fatal(err)
	}
	d := New(st, quietLog())
	if _, err := d.body(ctx, store.Delivery{RunID: "99999999-0000-4000-8000-000000000000"}); err == nil {
		t.Error("a missing run must be an error")
	}
	if got, err := d.body(ctx, store.Delivery{RunID: run.ID}); err != nil || !strings.Contains(string(got), `"run.finished"`) {
		t.Errorf("body = %s, %v", got, err)
	}
	long := strings.Repeat("x", maxOutputChars+10)
	if got := clip(&long); !strings.Contains(*got, "[cut;") || len(*got) > maxOutputChars+100 {
		t.Errorf("clip = %d chars", len(*got))
	}
	short := "ok"
	if got := clip(&short); got != &short {
		t.Error("short output is unchanged")
	}
	if clip(nil) != nil {
		t.Error("nil stays nil")
	}
}

func TestSecretSources(t *testing.T) {
	st := store.New(testdb.New(t, "callbacks_cov_test"))
	d := New(st, quietLog())
	_, err := d.secret(context.Background(), store.Delivery{SecretSource: "mystery"})
	if err == nil || !strings.Contains(err.Error(), "unknown secret source") {
		t.Errorf("err = %v", err)
	}
	if _, err := d.secret(context.Background(), store.Delivery{SecretSource: store.CallbackFromTrigger, SecretOwnerID: "99999999-0000-4000-8000-000000000000"}); err == nil {
		t.Error("unknown trigger must fail")
	}
	if _, err := d.secret(context.Background(), store.Delivery{SecretSource: store.CallbackFromServiceAccount, SecretOwnerID: "99999999-0000-4000-8000-000000000000"}); err == nil {
		t.Error("unknown service account must fail")
	}
}

func TestHousekeepingPrunesMemoriesHourly(t *testing.T) {
	st := store.New(testdb.New(t, "callbacks_cov_test"))
	ctx := context.Background()
	crews, _ := st.ListCrews(ctx, store.AllAreas)
	res, err := st.SaveMemories(ctx, crews[0].ID, []store.MemoryWrite{{Scope: "/", Content: "old", ContentHash: "h", Status: model.MemoryActive,
		WrittenBy: "t", EmbedModel: "m", Vector: []float32{1}}}, store.MemoryCaps{PerCrew: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool().Exec(ctx, `UPDATE memories SET created_at = now() - interval '400 days' WHERE id = $1`, res.Saved[0].ID); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h := NewHousekeeping(st, nil, quietLog())
	h.now = func() time.Time { return now }
	h.lastPrune = now.Add(-time.Minute)
	h.Tick(ctx)
	if n, _ := st.CountMemories(ctx, store.MemoryFilter{CrewID: crews[0].ID}); n != 1 {
		t.Errorf("pruned before the hour: %d left", n)
	}
	now = now.Add(memoryPruneEvery)
	h.Tick(ctx)
	if n, _ := st.CountMemories(ctx, store.MemoryFilter{CrewID: crews[0].ID}); n != 0 {
		t.Errorf("not pruned: %d left", n)
	}
	// A failing prune is logged, not fatal
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	h.lastPrune = time.Time{}
	h.pruneMemories(cancelled)
	h.store.Pool().Close()
	h.lastPrune = time.Time{}
	h.pruneMemories(ctx)
}
