package launch

import (
	"testing"

	"crewstudio/internal/model"
)

func TestEscalationIsFrozenIntoRuns(t *testing.T) {
	e := newEnv(t)
	p := e.full("esc")
	req := CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "Acme"}}

	// Escalation off (no settings, or saved but disabled): nothing in the spec
	run, _, err := e.l.StartCrewRun(bg(), p, req)
	ok(t, err)
	if spec := mustSpec(t, e, run.ID); spec.Escalation != nil {
		t.Errorf("off: %+v", spec.Escalation)
	}
	settings := model.DefaultEscalationSettings(e.crew.ID)
	settings.Rules = "Ask before assuming a budget."
	_, err = e.st.PutEscalationSettings(bg(), settings, "")
	ok(t, err)
	run, _, err = e.l.StartCrewRun(bg(), p, req)
	ok(t, err)
	if spec := mustSpec(t, e, run.ID); spec.Escalation != nil {
		t.Errorf("disabled: %+v", spec.Escalation)
	}

	settings.Enabled = true
	settings.OnTimeout = model.EscalationFail
	_, err = e.st.PutEscalationSettings(bg(), settings, "")
	ok(t, err)
	run, _, err = e.l.StartCrewRun(bg(), p, req)
	ok(t, err)
	want := model.EscalationSpec{Rules: "Ask before assuming a budget.", TimeoutMinutes: 30, OnTimeout: model.EscalationFail, MaxQuestions: 5}
	if spec := mustSpec(t, e, run.ID); spec.Escalation == nil || *spec.Escalation != want {
		t.Errorf("on: %+v", spec.Escalation)
	}

	// Work orders get it too
	wo := e.draftWorkOrder(p)
	run, _, err = e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{})
	ok(t, err)
	if spec := mustSpec(t, e, run.ID); spec.Escalation == nil {
		t.Error("work order run has no escalation")
	}
}
