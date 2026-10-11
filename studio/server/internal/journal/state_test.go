package journal

import (
	"testing"
	"time"

	"crewstudio/internal/model"
)

// Calls state tools make are recorded by the run-state API as "state" events and read as one line each.
func TestStateEventsReadAsOneLine(t *testing.T) {
	ev := model.RunEvent{Seq: 1, Type: "state", CreatedAt: time.Now(), Payload: map[string]any{
		"message": "Notes: read weekly-report v12", "detail": "subject project:atlas", "agent_role": "PMO Coordinator"}}
	j := Build(model.Run{ID: "r"}, model.RunSpec{}, []model.RunEvent{ev}, nil)
	if len(j.Entries) != 1 {
		t.Fatalf("entries = %+v", j.Entries)
	}
	e := j.Entries[0]
	if e.Kind != "state" || e.Title != "Notes: read weekly-report v12" || e.Body != "subject project:atlas" || e.AgentRole != "PMO Coordinator" {
		t.Errorf("entry = %+v", e)
	}
	if len(j.Agents) != 1 || j.Agents[0].Entries != 1 {
		t.Errorf("agents = %+v", j.Agents)
	}
}
