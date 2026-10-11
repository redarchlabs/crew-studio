package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
)

// taskCrewDraft returns an unsaved crew that runs one task: the task, the tasks it takes context from (earlier
// tasks first) and their agents, with an input for every placeholder. The crew builder opens with it, and saving
// creates the crew through the usual checks. Work orders, schedules and triggers run crews, so this is how a single
// task gets run.
func (s *Server) taskCrewDraft(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	p := principal(r)
	task, err := s.store.GetTask(r.Context(), id)
	if err == nil && !p.SeesAreas(task.AreaIDs) {
		err = store.ErrNotFound
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	lib, err := s.launcher.Library(r.Context())
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	// Input definitions are borrowed only from crews the caller can see
	visible := slices.DeleteFunc(slices.Clone(lib.Crews), func(c model.Crew) bool { return !p.SeesAreas(c.AreaIDs) })
	crew, problems := runspec.TaskCrew(task.ID, runspec.Library{
		LLMs: lib.LLMs, Agents: lib.Agents, Tasks: lib.Tasks, Tools: lib.Tools, Pricing: lib.Pricing, Settings: lib.Settings, Crews: visible,
	})
	problems = append(problems, hiddenMembers(crew, lib, p.SeesAreas)...)
	if len(problems) > 0 {
		failErr(w, r, s.log, &launch.ProblemsError{Message: "A crew cannot be made from this task.", Details: problems})
		return
	}
	// Every crew counts here: names are unique across areas
	crew.Name = uniqueCrewName(task.Name, lib.Crews)
	crew.Description = fmt.Sprintf("Runs the task %q.", task.Name)
	crew.AreaIDs = slices.DeleteFunc(slices.Clone(task.AreaIDs), func(id string) bool { return !p.HasArea(id) })
	ok(w, crew)
}

// hiddenMembers lists the tasks and agents of a draft crew the principal cannot see, which they could not save.
func hiddenMembers(crew model.Crew, lib runspec.Library, sees func([]string) bool) []string {
	var problems []string
	for _, id := range crew.TaskIDs {
		if t, found := lib.Tasks[id]; found && !sees(t.AreaIDs) {
			problems = append(problems, "It takes context from a task outside your functional areas.")
			break
		}
	}
	for _, id := range crew.AgentIDs {
		if a, found := lib.Agents[id]; found && !sees(a.AreaIDs) {
			problems = append(problems, "One of its tasks uses an agent outside your functional areas.")
			break
		}
	}
	return problems
}

// suffixedNameLen leaves room for " (task N)" within the crew name limit.
const suffixedNameLen = 100

// uniqueCrewName is the task's name, or the first free "<name> (task)", "<name> (task 2)", ... since crew names
// are unique. A suffixed name is cut short first, so it stays within the name limit.
func uniqueCrewName(name string, crews []model.Crew) string {
	taken := make(map[string]bool, len(crews))
	for _, c := range crews {
		taken[c.Name] = true
	}
	if !taken[name] {
		return name
	}
	base := name
	if runes := []rune(name); len(runes) > suffixedNameLen {
		base = strings.TrimSpace(string(runes[:suffixedNameLen]))
	}
	candidate := base + " (task)"
	for n := 2; taken[candidate]; n++ {
		candidate = fmt.Sprintf("%s (task %d)", base, n)
	}
	return candidate
}
