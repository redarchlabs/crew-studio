package runspec

import (
	"fmt"
	"slices"
	"strings"

	"crewstudio/internal/model"
)

// maxRunTasksLen bounds the run_tasks input.
const maxRunTasksLen = 2_000

// SelectTasks narrows a crew to the tasks named in the run_tasks input: task ids or names (ignoring case),
// separated by commas or new lines, kept in the crew's order. "" keeps every task. It lets one crew with several
// jobs, such as the PMO's daily sweep and weekly report, run only one job per schedule. A selected task that takes
// context from a task left out simply runs without it.
func SelectTasks(crew model.Crew, lib Library, runTasks string) (model.Crew, []string) {
	runTasks = strings.TrimSpace(runTasks)
	if runTasks == "" {
		return crew, nil
	}
	if len(runTasks) > maxRunTasksLen {
		return crew, []string{fmt.Sprintf("inputs.%s: must be at most %d characters", model.InputRunTasks, maxRunTasksLen)}
	}
	var problems []string
	keep := map[string]bool{}
	for _, want := range strings.FieldsFunc(runTasks, func(r rune) bool { return r == ',' || r == '\n' }) {
		want = strings.TrimSpace(want)
		if want == "" {
			continue
		}
		i := slices.IndexFunc(crew.TaskIDs, func(id string) bool {
			return id == want || (lib.Tasks[id].Name != "" && strings.EqualFold(lib.Tasks[id].Name, want))
		})
		if i < 0 {
			problems = append(problems, fmt.Sprintf("inputs.%s: the crew has no task %q", model.InputRunTasks, want))
			continue
		}
		keep[crew.TaskIDs[i]] = true
	}
	if len(problems) > 0 {
		return crew, problems
	}
	if len(keep) == 0 {
		return crew, []string{fmt.Sprintf("inputs.%s: name at least one of the crew's tasks", model.InputRunTasks)}
	}
	out := crew
	out.TaskIDs = slices.DeleteFunc(slices.Clone(crew.TaskIDs), func(id string) bool { return !keep[id] })
	return out, nil
}
