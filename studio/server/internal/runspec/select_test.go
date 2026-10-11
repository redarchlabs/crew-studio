package runspec

import (
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/model"
)

func TestSelectTasks(t *testing.T) {
	lib, crew := fixture(), crewFixture()
	for in, want := range map[string][]string{
		"":             {"t1", "t2"},
		"write":        {"t2"},
		" t1 ":         {"t1"},
		"Write,\nt1":   {"t1", "t2"},
		"Research, , ": {"t1"},
	} {
		got, problems := SelectTasks(crew, lib, in)
		if len(problems) > 0 || !slices.Equal(got.TaskIDs, want) {
			t.Errorf("%q -> %v %v", in, got.TaskIDs, problems)
		}
	}
	if got, _ := SelectTasks(crew, lib, "write"); len(crew.TaskIDs) != 2 || len(got.AgentIDs) != 2 {
		t.Error("the crew was changed, or agents dropped")
	}
	for in, want := range map[string]string{
		"Publish":                 `the crew has no task "Publish"`,
		" , ":                     "name at least one",
		strings.Repeat("x", 2001): "at most 2000",
	} {
		if _, problems := SelectTasks(crew, lib, in); !containsProblem(problems, want) {
			t.Errorf("%.20q: %v", in, problems)
		}
	}
}

func TestRunTasksIsAcceptedWithoutDeclaring(t *testing.T) {
	out, problems := ResolveInputs(crewFixture(), map[string]string{"topic": "AI", model.InputRunTasks: " Write "})
	if len(problems) > 0 || out[model.InputRunTasks] != "Write" {
		t.Errorf("out = %v, problems = %v", out, problems)
	}
	spec := Build(crewFixture(), fixture(), out, 1)
	if len(spec.Tasks) != 2 {
		t.Errorf("Build alone keeps every task: %d", len(spec.Tasks))
	}
}
