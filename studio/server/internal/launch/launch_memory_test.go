package launch

import (
	"testing"

	"crewstudio/internal/model"
)

type fakeMemory struct {
	ok    bool
	model string
}

func (f fakeMemory) Available() (bool, string) { return f.ok, "off" }
func (f fakeMemory) Model() string             { return f.model }

func TestMemoryIsFrozenIntoRuns(t *testing.T) {
	e := newEnv(t)
	p := e.full("mem")
	req := CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "Acme"}}

	// Memory off (no settings): nothing in the spec, whatever the embedder
	e.l.WithMemory(fakeMemory{ok: true, model: "fake:8"})
	run, _, err := e.l.StartCrewRun(bg(), p, req)
	ok(t, err)
	if spec := mustSpec(t, e, run.ID); spec.Memory != nil {
		t.Errorf("off: %+v", spec.Memory)
	}

	settings := model.DefaultMemorySettings(e.crew.ID)
	settings.Mode = model.MemoryAutomatic
	_, err = e.st.PutMemorySettings(bg(), settings, "")
	ok(t, err)
	run, _, err = e.l.StartCrewRun(bg(), p, req)
	ok(t, err)
	spec := mustSpec(t, e, run.ID)
	if spec.Memory == nil || spec.Memory.Mode != model.MemoryAutomatic || spec.Memory.LLMID != e.llm.ID || spec.Memory.EmbedModel != "fake:8" {
		t.Errorf("automatic: %+v", spec.Memory)
	}

	// Work orders get it too
	wo := e.draftWorkOrder(p)
	run, _, err = e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{})
	ok(t, err)
	if spec := mustSpec(t, e, run.ID); spec.Memory == nil {
		t.Error("work order run has no memory")
	}

	// No embedder: left out, and the run still starts
	e.l.WithMemory(fakeMemory{ok: false})
	run, _, err = e.l.StartCrewRun(bg(), p, req)
	ok(t, err)
	if spec := mustSpec(t, e, run.ID); spec.Memory != nil {
		t.Errorf("unavailable: %+v", spec.Memory)
	}

	// A memory model without a price stops the run
	unpriced, err := e.st.CreateLLM(bg(), model.LLM{Name: "Unpriced", Model: "openai/no-price"})
	ok(t, err)
	settings.LLMID = &unpriced.ID
	_, err = e.st.PutMemorySettings(bg(), settings, "")
	ok(t, err)
	e.l.WithMemory(fakeMemory{ok: true, model: "fake:8"})
	_, _, err = e.l.StartCrewRun(bg(), p, req)
	wantProblems(t, err, "Memory uses \"Unpriced\"")
	_, _, err = e.l.SubmitWorkOrder(bg(), p, e.draftWorkOrder(p).ID, SubmitOptions{})
	wantProblems(t, err, "no price")
}

func mustSpec(t *testing.T, e *env, runID string) model.RunSpec {
	t.Helper()
	spec, err := e.st.GetRunSpec(bg(), runID)
	ok(t, err)
	return spec
}
