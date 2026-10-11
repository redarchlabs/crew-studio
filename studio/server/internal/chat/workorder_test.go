package chat

import (
	"testing"

	"crewstudio/internal/model"
)

func TestFinalAgentIsTheLastTasksAgentInTheCrew(t *testing.T) {
	ptr := func(s string) *string { return &s }
	tasks := map[string]model.Task{
		"t1": {ID: "t1", AgentID: ptr("researcher")},
		"t2": {ID: "t2", AgentID: ptr("writer")},
		"t3": {ID: "t3", AgentID: ptr("gone")},
		"t4": {ID: "t4"},
	}
	cases := []struct {
		name string
		crew model.Crew
		want string
	}{
		{"last task's agent", model.Crew{AgentIDs: []string{"researcher", "writer"}, TaskIDs: []string{"t1", "t2"}}, "writer"},
		{"skips agents no longer in the crew and tasks without one", model.Crew{AgentIDs: []string{"researcher"}, TaskIDs: []string{"t1", "t3", "t4"}}, "researcher"},
		{"falls back to the first agent", model.Crew{AgentIDs: []string{"writer"}, TaskIDs: []string{"missing"}}, "writer"},
		{"no agents", model.Crew{TaskIDs: []string{"t1"}}, ""},
	}
	for _, c := range cases {
		if got := finalAgent(c.crew, tasks); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
