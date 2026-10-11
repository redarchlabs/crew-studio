package runner

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/storage"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

// Fake runners stand in for run_crew.py so these tests need no CrewAI and spend nothing.
const succeedScript = `
import json, sys
req = json.load(sys.stdin)
assert req["spec"]["inputs"]["company"] == "Acme"
def emit(t, p): print(json.dumps({"type": t, "payload": p}), flush=True)
print("\x1b[32mcrewai console line\x1b[0m", file=sys.stderr, flush=True)
emit("task_started", {"task_name": "Research", "agent_role": "Analyst"})
emit("llm_call", {"agent_role": "Analyst", "task_name": "Research", "model": "openai/gpt-6-luna",
  "usage": {"prompt_tokens": 100, "completion_tokens": 10}, "cost_usd": 0.01, "spent_usd": 0.01,
  "messages": [{"role": "user", "content": "go"}], "response": "done"})
emit("task_completed", {"task_name": "Research", "agent_role": "Analyst", "output": "notes"})
emit("crew_completed", {"output": "final memo", "cost_usd": 0.01, "usage": {"prompt_tokens": 100, "completion_tokens": 10,
  "cached_prompt_tokens": 0, "successful_requests": 1}, "task_outputs": [{"position": 0, "task_name": "Research", "agent_role": "Analyst", "raw": "notes"}]})
`

const failScript = `
import json
print(json.dumps({"type": "crew_failed", "payload": {"error": "LLMCallBlockedError: budget", "budget_stopped": True, "cost_usd": 2.5}}), flush=True)
raise SystemExit(1)
`

const crashScript = `
import sys
print("Traceback: something broke", file=sys.stderr)
raise SystemExit(3)
`

const sleepScript = `
import json, time
print(json.dumps({"type": "crew_started", "payload": {"crew_name": "x"}}), flush=True)
time.sleep(60)
`

type harness struct {
	store  *store.Store
	broker *Broker
	files  *storage.Memory
	crew   model.Crew
}

func newHarness(t *testing.T) harness {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	st := store.New(testdb.New(t, "runner_test"))
	crews, err := st.ListCrews(context.Background(), store.AllAreas)
	if err != nil {
		t.Fatal(err)
	}
	return harness{store: st, broker: NewBroker(), files: storage.NewMemory(), crew: crews[0]}
}

func (h harness) manager(t *testing.T, script string) *Manager {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake_runner.py")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	python, _ := exec.LookPath("python3")
	return NewManager(h.store, h.broker, ProcessConfig{
		Python: python, Script: path, Workspace: filepath.Join(dir, "ws"), DataDir: filepath.Join(dir, "data"),
		ProviderEnvFile: filepath.Join(dir, ".env"), MaxDuration: time.Minute,
	}, h.files, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func (h harness) queue(t *testing.T) model.Run {
	t.Helper()
	run, _, err := h.store.CreateRun(context.Background(), model.RunSpec{Crew: h.crew, Inputs: map[string]string{"company": "Acme"}, BudgetUSD: 1}, store.RunMeta{AreaIDs: h.crew.AreaIDs})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func waitFor(t *testing.T, h harness, id string, want model.RunStatus) model.Run {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		run, err := h.store.GetRunDetail(context.Background(), id)
		if err == nil && run.Status == want {
			return run
		}
		time.Sleep(50 * time.Millisecond)
	}
	run, _ := h.store.GetRun(context.Background(), id)
	t.Fatalf("run %s did not reach %s; status %s, error %v", id, want, run.Status, run.Error)
	return model.Run{}
}

func startManager(t *testing.T, m *Manager) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func eventTypes(t *testing.T, st *store.Store, id string) []string {
	t.Helper()
	events, err := st.ListEvents(context.Background(), id, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

func TestSuccessfulRunRecordsEverything(t *testing.T) {
	h := newHarness(t)
	m := h.manager(t, succeedScript)
	run := h.queue(t)
	live, stop := h.broker.Subscribe(run.ID)
	defer stop()
	startManager(t, m)

	done := waitFor(t, h, run.ID, model.RunSucceeded)
	if done.Output == nil || *done.Output != "final memo" || done.CostUSD == nil || *done.CostUSD != 0.01 {
		t.Errorf("run = %+v", done)
	}
	if done.TaskOutputs == nil || len(*done.TaskOutputs) != 1 || (*done.TaskOutputs)[0].Raw != "notes" || done.Usage.PromptTokens != 100 {
		t.Errorf("outputs/usage = %+v %+v", done.TaskOutputs, done.Usage)
	}

	types := strings.Join(eventTypes(t, h.store, run.ID), ",")
	for _, want := range []string{"run_status", "log", "task_started", "llm_call", "task_completed", "crew_completed"} {
		if !strings.Contains(types, want) {
			t.Errorf("events %s missing %s", types, want)
		}
	}
	if !strings.HasSuffix(types, "run_status") {
		t.Errorf("last event should be run_status: %s", types)
	}
	calls, _ := h.store.ListLLMCalls(context.Background(), run.ID)
	if len(calls) != 1 || calls[0].Response != "done" {
		t.Errorf("llm calls = %+v", calls)
	}

	var liveCount int
	for range live {
		liveCount++
	}
	if liveCount == 0 {
		t.Error("subscriber received no live events")
	}
}

func TestFailedRunKeepsRunnerError(t *testing.T) {
	h := newHarness(t)
	startManager(t, h.manager(t, failScript))
	run := waitFor(t, h, h.queue(t).ID, model.RunFailed)
	if run.Error == nil || !strings.Contains(*run.Error, "budget") || run.CostUSD == nil || *run.CostUSD != 2.5 {
		t.Errorf("run = %+v", run)
	}
}

func TestCrashReportsLastOutput(t *testing.T) {
	h := newHarness(t)
	startManager(t, h.manager(t, crashScript))
	run := waitFor(t, h, h.queue(t).ID, model.RunFailed)
	if run.Error == nil || !strings.Contains(*run.Error, "something broke") {
		t.Errorf("error = %v", run.Error)
	}
}

func TestCancelRunningRun(t *testing.T) {
	h := newHarness(t)
	m := h.manager(t, sleepScript)
	startManager(t, m)
	run := h.queue(t)
	waitFor(t, h, run.ID, model.RunRunning)

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(strings.Join(eventTypes(t, h.store, run.ID), ","), "crew_started") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	got, err := m.Cancel(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.RunCancelled {
		t.Errorf("status after cancel = %s", got.Status)
	}
	if m.ActiveCount() != 0 {
		t.Error("cancelled run still active")
	}
}

func TestCancelQueuedRun(t *testing.T) {
	h := newHarness(t)
	m := h.manager(t, succeedScript) // never started: no Run loop
	run := h.queue(t)
	got, err := m.Cancel(context.Background(), run.ID)
	if err != nil || got.Status != model.RunCancelled {
		t.Fatalf("got %+v, %v", got, err)
	}
	types := eventTypes(t, h.store, run.ID)
	if len(types) != 1 || types[0] != "run_status" {
		t.Errorf("events = %v", types)
	}
}

func TestFinishedRunNeverStaysRunning(t *testing.T) {
	h := newHarness(t)
	// NUL bytes would make Postgres reject the whole result; they must be stripped, not leave the run stuck
	startManager(t, h.manager(t, `
import json
print(json.dumps({"type": "crew_completed", "payload": {"output": "memo\u0000", "task_outputs": [{"position": 0, "task_name": "T", "agent_role": "A", "raw": "x\u0000"}]}}), flush=True)
`))
	run := waitFor(t, h, h.queue(t).ID, model.RunSucceeded)
	if run.Output == nil || *run.Output != "memo" {
		t.Errorf("output = %v", run.Output)
	}
}

func TestConcurrencyLimitFromSettings(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	settings, _ := h.store.GetSettings(ctx)
	settings.MaxConcurrentRuns = 1
	if _, err := h.store.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	m := h.manager(t, sleepScript)
	first, second := h.queue(t), h.queue(t)
	startManager(t, m)
	waitFor(t, h, first.ID, model.RunRunning)
	time.Sleep(300 * time.Millisecond)
	if got, _ := h.store.GetRun(ctx, second.ID); got.Status != model.RunQueued {
		t.Errorf("second run should wait, status %s", got.Status)
	}
	_, _ = m.Cancel(ctx, first.ID)
	waitFor(t, h, second.ID, model.RunRunning)
	_, _ = m.Cancel(ctx, second.ID)
}

func TestOrphanedRunsFailOnStartup(t *testing.T) {
	h := newHarness(t)
	run := h.queue(t)
	if ok, _ := h.store.StartRun(context.Background(), run.ID); !ok {
		t.Fatal("could not start run")
	}
	// Mark the run cancelled-by-crash: a fresh manager must fail it before dispatching
	m := h.manager(t, succeedScript)
	startManager(t, m)
	got := waitFor(t, h, run.ID, model.RunFailed)
	if got.Error == nil || !strings.Contains(*got.Error, "restarted") {
		t.Errorf("error = %v", got.Error)
	}
}

// filesScript checks its input file arrived, writes an output file and a transcript line, then succeeds.
const filesScript = `
import json, os, sys
req = json.load(sys.stdin)
ws = req["workspace"]
with open(os.path.join(ws, "attachments", "brief.txt")) as f:
    assert f.read() == "read me"
assert not os.path.commonpath([ws, req["transcript_path"]]) == ws, "transcript must be outside the workspace"
os.makedirs(os.path.join(ws, "out"))
with open(os.path.join(ws, "out", "report.md"), "w") as f:
    f.write("# Report")
with open(req["transcript_path"], "a") as f:
    f.write(json.dumps({"type": "crew_started", "payload": {"full": "x" * 5000}}) + "\n")
print("console sk-proj-abcdefghijklmnopqrstuvwx line", file=sys.stderr, flush=True)
print(json.dumps({"type": "crew_completed", "payload": {"output": "done", "cost_usd": 0, "task_outputs": []}}), flush=True)
`

func TestWorkOrderRunMovesFilesInAndOut(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.files.Put(ctx, "work-orders/x/in", strings.NewReader("read me"), 7, "text/plain"); err != nil {
		t.Fatal(err)
	}
	wo, err := h.store.CreateWorkOrder(ctx, model.WorkOrderInput{Title: "t", CrewID: &h.crew.ID},
		store.WorkOrderMeta{TargetName: h.crew.Name, AreaIDs: h.crew.AreaIDs})
	if err != nil {
		t.Fatal(err)
	}
	input, err := h.store.AddAttachment(ctx, model.Attachment{ID: "0b6f6a7e-1f6e-4c5c-9a7e-3f1d2c4b5a69", WorkOrderID: &wo.ID,
		Kind: model.AttachmentInput, Filename: "brief.txt", ContentType: "text/plain", SizeBytes: 7, SHA256: "x", StorageKey: "work-orders/x/in"})
	if err != nil {
		t.Fatal(err)
	}
	spec := model.RunSpec{Crew: h.crew, Inputs: map[string]string{"company": "Acme"}, BudgetUSD: 1, WorkOrder: &model.WorkOrderSpec{
		ID: wo.ID, Attachments: []model.SpecAttachment{{ID: input.ID, Filename: "brief.txt", SizeBytes: 7, Path: "attachments/brief.txt"}}}}
	run, _, err := h.store.CreateRun(ctx, spec, store.RunMeta{WorkOrderID: wo.ID, AreaIDs: h.crew.AreaIDs})
	if err != nil {
		t.Fatal(err)
	}
	m := h.manager(t, filesScript)
	startManager(t, m)
	done := waitFor(t, h, run.ID, model.RunSucceeded)
	if done.Output == nil || *done.Output != "done" {
		t.Fatalf("run = %+v", done)
	}
	files, err := h.store.ListAttachments(ctx, store.AttachmentFilter{RunID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]model.Attachment{}
	for _, f := range files {
		names[f.Filename] = f
	}
	if names["out/report.md"].Kind != model.AttachmentArtifact || names["transcript.jsonl"].Kind != model.AttachmentTranscript ||
		names["console.log"].Kind != model.AttachmentTranscript || len(files) != 3 {
		t.Fatalf("files = %+v", files)
	}
	body, _ := h.files.Get(ctx, names["console.log"].StorageKey)
	console, _ := io.ReadAll(body)
	if strings.Contains(string(console), "sk-proj-") || !strings.Contains(string(console), "[redacted]") {
		t.Errorf("console log not redacted: %q", console)
	}
	if names["out/report.md"].ContentType != "text/markdown; charset=utf-8" {
		t.Errorf("content type = %q", names["out/report.md"].ContentType)
	}
	types := eventTypes(t, h.store, run.ID)
	if types[len(types)-2] != "artifacts" || types[len(types)-1] != "run_status" {
		t.Errorf("artifacts must be recorded before the final status: %v", types)
	}
	paths := m.pathsFor(run.ID)
	for _, dir := range []string{paths.workspace, paths.private} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s should be removed after upload", dir)
		}
	}
}
