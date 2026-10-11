package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// The resumed run's fake runner checks the earlier run's file is back in its workspace, finishes the second task, then
// crashes without a final result (as a killed runner would).
const resumeScript = `
import json, sys
req = json.load(sys.stdin)
assert req["spec"]["resume"]["start_position"] == 1
assert open("notes/draft.md").read() == "draft from the first run"
def emit(t, p): print(json.dumps({"type": t, "payload": p}), flush=True)
emit("task_started", {"task_name": "Write", "agent_role": "Writer"})
emit("llm_call", {"agent_role": "Writer", "task_name": "Write", "model": "openai/gpt-6-luna",
  "usage": {"prompt_tokens": 100, "completion_tokens": 10}, "cost_usd": 0.05, "spent_usd": 0.05, "messages": [], "response": "memo"})
emit("task_completed", {"task_name": "Write", "agent_role": "Writer", "output": "the memo", "position": 1})
emit("task_completed", {"task_name": "Forged", "output": "not a task of this run", "position": 7})
raise SystemExit(9)
`

func TestResumedRunRestoresFilesAndKeepsFinishedTasks(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	spec := model.RunSpec{Crew: h.crew, Inputs: map[string]string{"company": "Acme"}, BudgetUSD: 1,
		Tasks: []model.Task{{ID: "t1", Name: "Research"}, {ID: "t2", Name: "Write"}, {ID: "t3", Name: "Review"}}}
	src, _, err := h.store.CreateRun(ctx, spec, store.RunMeta{AreaIDs: h.crew.AreaIDs})
	if err != nil {
		t.Fatal(err)
	}
	msg := "out of credits"
	first := model.TaskOutput{Position: 0, TaskName: "Research", AgentRole: "Analyst", Raw: "notes"}
	if _, err := h.store.FinishRun(ctx, src.ID, store.RunResult{Status: model.RunFailed, Error: &msg, TaskOutputs: []model.TaskOutput{first}}); err != nil {
		t.Fatal(err)
	}
	if err := h.files.Put(ctx, "runs/src/draft", strings.NewReader("draft from the first run"), 24, "text/markdown"); err != nil {
		t.Fatal(err)
	}
	for _, a := range []model.Attachment{
		{ID: "0b6f6a7e-1f6e-4c5c-9a7e-3f1d2c4b5a70", Filename: "notes/draft.md", StorageKey: "runs/src/draft", Kind: model.AttachmentArtifact},
		// Not restored: the transcript is not something agents wrote, and nothing may land outside the workspace
		{ID: "0b6f6a7e-1f6e-4c5c-9a7e-3f1d2c4b5a71", Filename: "transcript.jsonl", StorageKey: "runs/src/transcript", Kind: model.AttachmentTranscript},
		{ID: "0b6f6a7e-1f6e-4c5c-9a7e-3f1d2c4b5a72", Filename: "../escape.md", StorageKey: "runs/src/escape", Kind: model.AttachmentArtifact},
	} {
		a.RunID, a.ContentType, a.SizeBytes, a.SHA256 = &src.ID, "text/markdown", 24, "x"
		if _, err := h.store.AddAttachment(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	reused := model.ReusedOutputs(model.Run{ID: src.ID, TaskOutputs: &[]model.TaskOutput{first}}, 1)
	spec.Resume = &model.ResumeSpec{FromRunID: src.ID, StartPosition: 1, Outputs: reused}
	run, _, err := h.store.CreateRun(ctx, spec, store.RunMeta{AreaIDs: h.crew.AreaIDs,
		Resume: &store.RunResume{FromRunID: src.ID, StartPosition: 1, Outputs: reused}})
	if err != nil {
		t.Fatal(err)
	}
	startManager(t, h.manager(t, resumeScript))
	done := waitFor(t, h, run.ID, model.RunFailed)

	// The runner died before reporting a result, yet the task it finished is kept, with its cost (summed from its LLM
	// calls); a position outside the run's tasks is ignored
	outs := *done.TaskOutputs
	if len(outs) != 2 || outs[0].ReusedFromRunID == nil || *outs[0].ReusedFromRunID != src.ID || outs[0].Raw != "notes" {
		t.Fatalf("outputs = %+v", outs)
	}
	if outs[1].Raw != "the memo" || outs[1].ReusedFromRunID != nil || outs[1].CostUSD == nil || *outs[1].CostUSD != 0.05 {
		t.Fatalf("new output = %+v", outs[1])
	}
	if hint, ok := model.ResumePoint(done); !ok || hint.StartPosition != 2 || hint.TaskName != "Review" {
		t.Fatalf("resume point = %+v %v", hint, ok)
	}

	events, err := h.store.ListEvents(ctx, run.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var resumed map[string]any
	for _, e := range events {
		if e.Type == "run_resumed" {
			resumed = e.Payload
		}
	}
	if resumed == nil || resumed["from_run_id"] != src.ID || resumed["restored_files"] != float64(1) || resumed["error"] != nil {
		t.Fatalf("run_resumed = %v", resumed)
	}
}

func TestFinishedTaskNeedsAPosition(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		ok      bool
	}{
		{"with position", map[string]any{"position": float64(2), "output": "x", "task_name": "T", "agent_role": "R"}, true},
		{"first task", map[string]any{"position": float64(0), "output": "x"}, true},
		{"older runners send no position", map[string]any{"output": "x"}, false},
		{"negative", map[string]any{"position": float64(-1)}, false},
		{"fractional", map[string]any{"position": 1.5}, false},
		{"past the run's tasks", map[string]any{"position": float64(3)}, false},
	}
	for _, c := range cases {
		o, ok := finishedTask(model.RunEvent{Type: "task_completed", Payload: c.payload}, 3)
		if ok != c.ok {
			t.Errorf("%s: ok = %v", c.name, ok)
		}
		if ok && c.payload["task_name"] != nil && (o.TaskName != "T" || o.AgentRole != "R" || o.Raw != "x" || o.Position != 2) {
			t.Errorf("%s: %+v", c.name, o)
		}
	}
	if _, ok := finishedTask(model.RunEvent{Type: "task_started", Payload: map[string]any{"position": float64(1)}}, 3); ok {
		t.Error("only task_completed events carry outputs")
	}
}

func TestRestoreSaysWhatCouldNotComeBack(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := h.manager(t, succeedScript)

	// The earlier run was deleted
	n, notes := m.restoreArtifacts(ctx, "new", missingRunID, t.TempDir())
	if n != 0 || len(notes) != 1 || !strings.Contains(notes[0], "was deleted") {
		t.Fatalf("deleted source: %d %v", n, notes)
	}

	// It could not keep everything, one file is over the limits, and one is bigger in storage than recorded
	src := h.queue(t)
	if err := h.store.AppendEvent(ctx, src.ID, model.RunEvent{Seq: 1, Type: "artifacts",
		Payload: map[string]any{"files": 1.0, "error": "3 files were not kept"}, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_ = h.files.Put(ctx, "runs/s/big", strings.NewReader("x"), 1, "text/plain")
	_ = h.files.Put(ctx, "runs/s/liar", strings.NewReader("much longer than recorded"), 25, "text/plain")
	for _, a := range []model.Attachment{
		{ID: "0b6f6a7e-1f6e-4c5c-9a7e-3f1d2c4b5a80", Filename: "big.bin", StorageKey: "runs/s/big", SizeBytes: maxArtifactFileBytes + 1},
		{ID: "0b6f6a7e-1f6e-4c5c-9a7e-3f1d2c4b5a81", Filename: "liar.txt", StorageKey: "runs/s/liar", SizeBytes: 4},
	} {
		a.RunID, a.Kind, a.ContentType, a.SHA256 = &src.ID, model.AttachmentArtifact, "text/plain", "x"
		if _, err := h.store.AddAttachment(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	workspace := t.TempDir()
	n, notes = m.restoreArtifacts(ctx, "new", src.ID, workspace)
	joined := strings.Join(notes, " ")
	if n != 0 || !strings.Contains(joined, "could not keep all") || !strings.Contains(joined, "over the limits") ||
		!strings.Contains(joined, "big.bin") || !strings.Contains(joined, "1 files could not be restored: liar.txt") {
		t.Fatalf("restored %d, notes %v", n, notes)
	}
	if strings.Contains(joined, workspace) {
		t.Error("notes must not show server paths")
	}
	if _, err := os.Stat(filepath.Join(workspace, "liar.txt")); !os.IsNotExist(err) {
		t.Error("a file larger than recorded must not be left behind")
	}
}

const missingRunID = "99999999-0000-4000-8000-000000000000"

func TestSomeNamesIsCapped(t *testing.T) {
	if got := someNames([]string{"a", "b"}); got != "a, b" {
		t.Error(got)
	}
	many := make([]string, 13)
	for i := range many {
		many[i] = string(rune('a' + i))
	}
	if got := someNames(many); got != "a, b, c, d, e, f, g, h, i, j and 3 more" {
		t.Error(got)
	}
}
