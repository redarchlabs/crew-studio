package model

import "testing"

func resumeRun(status RunStatus, positions ...int) Run {
	tasks := []Task{{Name: "Research"}, {Name: "Design"}, {Name: "Write"}}
	outputs := make([]TaskOutput, 0, len(positions))
	for _, p := range positions {
		cost := 0.25
		outputs = append(outputs, TaskOutput{Position: p, TaskName: tasks[p].Name, Raw: "out", CostUSD: &cost})
	}
	return Run{ID: "r1", Kind: RunKindCrew, Status: status, Spec: &RunSpec{Tasks: tasks}, TaskOutputs: &outputs}
}

func TestResumePoint(t *testing.T) {
	cases := []struct {
		name  string
		run   Run
		ok    bool
		start int
	}{
		{"failed after the first task", resumeRun(RunFailed, 0), true, 1},
		{"failed after two tasks", resumeRun(RunFailed, 0, 1), true, 2},
		{"cancelled runs resume too", resumeRun(RunCancelled, 0), true, 1},
		{"a gap ends the reusable outputs", resumeRun(RunFailed, 0, 2), true, 1},
		{"nothing finished: run it again instead", resumeRun(RunFailed), false, 0},
		{"first task missing", resumeRun(RunFailed, 1), false, 0},
		{"every task finished", resumeRun(RunFailed, 0, 1, 2), false, 0},
		{"succeeded runs are not resumed", resumeRun(RunSucceeded, 0), false, 0},
		{"running runs are not resumed", resumeRun(RunRunning, 0), false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hint, ok := ResumePoint(c.run)
			if ok != c.ok || hint.StartPosition != c.start {
				t.Fatalf("got %+v %v, want start %d ok %v", hint, ok, c.start, c.ok)
			}
			if ok {
				if hint.TaskName != c.run.Spec.Tasks[c.start].Name || hint.ReusedTasks != c.start {
					t.Fatalf("hint %+v", hint)
				}
				if want := 0.25 * float64(c.start); hint.ReusedCostUSD != want {
					t.Fatalf("reused cost %v, want %v", hint.ReusedCostUSD, want)
				}
			}
		})
	}
}

func TestResumePointNeedsACrewRunDetail(t *testing.T) {
	chat := resumeRun(RunFailed, 0)
	chat.Kind = RunKindChat
	noSpec := resumeRun(RunFailed, 0)
	noSpec.Spec = nil
	noOutputs := resumeRun(RunFailed, 0)
	noOutputs.TaskOutputs = nil
	chatSpec := resumeRun(RunFailed, 0)
	chatSpec.Spec.Chat = &ChatSpec{}
	for name, run := range map[string]Run{"chat": chat, "no spec": noSpec, "no outputs": noOutputs, "chat spec": chatSpec} {
		if _, ok := ResumePoint(run); ok {
			t.Errorf("%s: want not resumable", name)
		}
	}
}

func TestReusedOutputsKeepTheOriginalProducer(t *testing.T) {
	run := resumeRun(RunFailed, 0, 1, 2)
	older := "r0"
	(*run.TaskOutputs)[0].ReusedFromRunID = &older

	got := ReusedOutputs(run, 2)
	if len(got) != 2 {
		t.Fatalf("got %d outputs", len(got))
	}
	if *got[0].ReusedFromRunID != "r0" || *got[1].ReusedFromRunID != "r1" {
		t.Fatalf("producers %q %q", *got[0].ReusedFromRunID, *got[1].ReusedFromRunID)
	}
	if (*run.TaskOutputs)[1].ReusedFromRunID != nil {
		t.Fatal("the source run's outputs must not change")
	}
	if ReusedOutputs(Run{}, 1) != nil {
		t.Fatal("no outputs: nil")
	}
}
