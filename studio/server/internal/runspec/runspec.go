// Package runspec decides whether a crew can run and freezes everything a run needs into a RunSpec.
package runspec

import (
	"fmt"
	"slices"
	"strings"

	"crewstudio/internal/model"
)

// MaxInputValueLen caps each run input so one value cannot blow up every prompt.
const MaxInputValueLen = 20_000

// MaxWatchInputLen caps the inputs a watch adds (changes, followups_due, dry_run); the runner gives them to one
// task only, unless tasks name them as {placeholders}.
const MaxWatchInputLen = 120_000

const googleSearchTool = "google_search"

// Library is the configuration a crew draws on, keyed by ID.
type Library struct {
	LLMs     map[string]model.LLM
	Agents   map[string]model.Agent
	Tasks    map[string]model.Task
	Tools    map[string]model.Tool
	Pricing  []model.ModelPrice
	Settings model.Settings
	// Crews lets a crew made from one task reuse the input definitions of a crew the task belongs to
	Crews []model.Crew
}

// Check lists everything that would stop the crew from running. An empty result means it can run.
func Check(crew model.Crew, lib Library) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if len(crew.TaskIDs) == 0 {
		add("The crew has no tasks.")
	}
	if len(crew.AgentIDs) == 0 {
		add("The crew has no agents.")
	}
	if crew.Process == model.ProcessHierarchical {
		if crew.ManagerLLMID == nil {
			add("A hierarchical crew needs a manager LLM.")
		} else {
			checkLLMPrice(*crew.ManagerLLMID, "The manager", lib, add)
		}
	}

	declared := map[string]bool{}
	for _, in := range crew.Inputs {
		declared[in.Key] = true
	}
	checkPlaceholders := func(owner, text string) {
		for _, name := range model.Placeholders(text) {
			if !declared[name] {
				add("%s uses {%s}, but the crew has no input with that key.", owner, name)
			}
		}
	}

	usesSearch := false
	var pricedModels []string // models tools bill for, such as the video model
	for _, id := range crew.AgentIDs {
		agent, ok := lib.Agents[id]
		if !ok {
			add("Agent %s no longer exists.", id)
			continue
		}
		owner := fmt.Sprintf("Agent %q", agent.Name)
		if agent.LLMID == nil {
			add("%s has no LLM; pick one so its cost can be tracked.", owner)
		} else {
			checkLLMPrice(*agent.LLMID, owner, lib, add)
		}
		for _, key := range agent.Tools {
			tool, ok := lib.Tools[key]
			switch {
			case !ok:
				add("%s uses unknown tool %q.", owner, key)
			case !tool.Available:
				add("%s uses tool %q, which needs %s set in the provider .env file.", owner, key, strings.Join(tool.RequiresEnv, ", "))
			}
			usesSearch = usesSearch || key == googleSearchTool
			if ok && tool.PricedModel != "" && !slices.Contains(pricedModels, tool.PricedModel) {
				pricedModels = append(pricedModels, tool.PricedModel)
			}
		}
		for _, text := range []string{agent.Role, agent.Goal, agent.Backstory} {
			checkPlaceholders(owner, text)
		}
	}
	if usesSearch && MatchPrice(lib.Settings.SearchModel, lib.Pricing) == nil {
		add("There is no price for the search model %q; add one on the Pricing page.", lib.Settings.SearchModel)
	}
	for _, m := range pricedModels {
		if MatchPrice(m, lib.Pricing) == nil {
			add("There is no price for the model %q that a tool bills for; add one on the Pricing page.", m)
		}
	}

	for position, id := range crew.TaskIDs {
		task, ok := lib.Tasks[id]
		if !ok {
			add("Task %s no longer exists.", id)
			continue
		}
		owner := fmt.Sprintf("Task %q", task.Name)
		checkTaskAgent(crew, task, owner, lib, add)
		for _, ctxID := range task.ContextTaskIDs {
			ctxPos := slices.Index(crew.TaskIDs, ctxID)
			ctxName := ctxID
			if ctxTask, ok := lib.Tasks[ctxID]; ok {
				ctxName = ctxTask.Name
			}
			switch {
			case ctxPos < 0:
				add("%s uses the output of %q, which is not in the crew.", owner, ctxName)
			// The runner builds tasks in order, so a later task cannot be context in either process
			case ctxPos > position:
				add("%s uses the output of %q, so %q must come earlier.", owner, ctxName, ctxName)
			}
		}
		checkPlaceholders(owner, task.Description)
		checkPlaceholders(owner, task.ExpectedOutput)
	}
	return problems
}

func checkTaskAgent(crew model.Crew, task model.Task, owner string, lib Library, add func(string, ...any)) {
	if task.AgentID == nil {
		if crew.Process == model.ProcessSequential {
			add("%s has no agent; a sequential crew needs one on every task.", owner)
		}
		return
	}
	if slices.Contains(crew.AgentIDs, *task.AgentID) {
		return
	}
	agentName := *task.AgentID
	if agent, ok := lib.Agents[*task.AgentID]; ok {
		agentName = agent.Name
	}
	add("%s is assigned to %q, which is not in the crew.", owner, agentName)
}

func checkLLMPrice(llmID, owner string, lib Library, add func(string, ...any)) {
	llm, ok := lib.LLMs[llmID]
	if !ok {
		add("%s uses an LLM that no longer exists.", owner)
		return
	}
	if MatchPrice(llm.Model, lib.Pricing) == nil {
		add("%s uses %q, but there is no price for model %q; add one on the Pricing page.", owner, llm.Name, llm.Model)
	}
}

// ResolveInputs fills in defaults, trims values, and reports missing, unknown or oversized inputs.
func ResolveInputs(crew model.Crew, provided map[string]string) (map[string]string, []string) {
	return resolveInputs(crew, provided, false)
}

// ResolveWatchedInputs is ResolveInputs for a work order a watched schedule created: the inputs the watch adds
// (changes, followups_due, dry_run) are accepted without being declared, up to MaxWatchInputLen. Only the
// scheduler's work orders may carry them; everyone else goes through ResolveInputs.
func ResolveWatchedInputs(crew model.Crew, provided map[string]string) (map[string]string, []string) {
	return resolveInputs(crew, provided, true)
}

func resolveInputs(crew model.Crew, provided map[string]string, watched bool) (map[string]string, []string) {
	declared := map[string]bool{}
	var problems []string
	out := make(map[string]string, len(crew.Inputs))
	for _, in := range crew.Inputs {
		declared[in.Key] = true
		value, ok := provided[in.Key]
		if !ok {
			value = in.Default
		}
		value = strings.TrimSpace(value)
		if in.Required && value == "" {
			problems = append(problems, fmt.Sprintf("inputs.%s: is required", in.Key))
		}
		if len(value) > MaxInputValueLen && !(watched && slices.Contains(model.WatchInputKeys, in.Key)) {
			problems = append(problems, fmt.Sprintf("inputs.%s: must be at most %d characters", in.Key, MaxInputValueLen))
		}
		out[in.Key] = value
	}
	for key, value := range provided {
		switch {
		case watched && slices.Contains(model.WatchInputKeys, key):
			// A watched schedule adds these whether or not the crew declares them
			value = strings.TrimSpace(value)
			if len(value) > MaxWatchInputLen {
				problems = append(problems, fmt.Sprintf("inputs.%s: must be at most %d characters", key, MaxWatchInputLen))
			}
			out[key] = value
		case key == model.InputRunTasks && !declared[key]:
			// Which of the crew's tasks to run (see SelectTasks); any work order may narrow its crew this way
			out[key] = strings.TrimSpace(value)
		case !declared[key]:
			problems = append(problems, fmt.Sprintf("inputs.%s: the crew has no input with this key", key))
		}
	}
	slices.Sort(problems)
	return out, problems
}

// Build freezes the crew and everything it references. Call Check first.
func Build(crew model.Crew, lib Library, inputs map[string]string, budgetUSD float64) model.RunSpec {
	spec := model.RunSpec{
		Crew:      crew,
		Agents:    make([]model.Agent, 0, len(crew.AgentIDs)),
		Tasks:     make([]model.Task, 0, len(crew.TaskIDs)),
		LLMs:      []model.LLM{},
		Pricing:   slices.Clone(lib.Pricing),
		Settings:  lib.Settings,
		Inputs:    inputs,
		BudgetUSD: budgetUSD,
	}
	usedLLMs := map[string]bool{}
	if crew.ManagerLLMID != nil {
		usedLLMs[*crew.ManagerLLMID] = true
	}
	for _, id := range crew.AgentIDs {
		if agent, ok := lib.Agents[id]; ok {
			spec.Agents = append(spec.Agents, agent)
			if agent.LLMID != nil {
				usedLLMs[*agent.LLMID] = true
			}
		}
	}
	for _, id := range crew.TaskIDs {
		if task, ok := lib.Tasks[id]; ok {
			spec.Tasks = append(spec.Tasks, task)
		}
	}
	for id := range usedLLMs {
		if llm, ok := lib.LLMs[id]; ok {
			spec.LLMs = append(spec.LLMs, llm)
		}
	}
	slices.SortFunc(spec.LLMs, func(a, b model.LLM) int { return strings.Compare(a.Name, b.Name) })
	return spec
}

// MatchPrice finds the price for a model id: an exact match first, then the part after the provider
// prefix ("openai/gpt-6-luna" matches a "gpt-6-luna" price).
func MatchPrice(modelID string, prices []model.ModelPrice) *model.ModelPrice {
	for i := range prices {
		if prices[i].Model == modelID {
			return &prices[i]
		}
	}
	if _, bare, ok := strings.Cut(modelID, "/"); ok {
		for i := range prices {
			if prices[i].Model == bare {
				return &prices[i]
			}
		}
	}
	return nil
}

// TaskCrew builds a crew that runs one task: the task, every task it takes context from (directly or indirectly,
// earlier tasks first), and their agents, as a sequential crew named after the task. Inputs are the placeholders
// those tasks and agents use.
func TaskCrew(taskID string, lib Library) (model.Crew, []string) {
	var problems []string
	var order []string
	visiting := map[string]bool{}
	done := map[string]bool{}
	var visit func(id string)
	visit = func(id string) {
		if done[id] {
			return
		}
		if visiting[id] {
			problems = append(problems, "The tasks' context forms a loop, so there is no order to run them in.")
			return
		}
		task, ok := lib.Tasks[id]
		if !ok {
			problems = append(problems, fmt.Sprintf("Task %s no longer exists.", id))
			return
		}
		visiting[id] = true
		for _, ctxID := range task.ContextTaskIDs {
			visit(ctxID)
		}
		visiting[id] = false
		done[id] = true
		order = append(order, id)
	}
	visit(taskID)

	root := lib.Tasks[taskID]
	crew := model.Crew{
		Name:      root.Name,
		Process:   model.ProcessSequential,
		TaskIDs:   order,
		AgentIDs:  []string{},
		Inputs:    []model.CrewInput{},
		BudgetUSD: lib.Settings.DefaultBudgetUSD,
		AreaIDs:   root.AreaIDs,
	}
	// Inputs a crew containing this task already defines keep their label, default and required flag
	defined := map[string]model.CrewInput{}
	for _, c := range lib.Crews {
		if !slices.Contains(c.TaskIDs, taskID) {
			continue
		}
		for _, in := range c.Inputs {
			if _, seen := defined[in.Key]; !seen {
				defined[in.Key] = in
			}
		}
	}
	seenInput := map[string]bool{}
	addInputs := func(texts ...string) {
		for _, text := range texts {
			for _, key := range model.Placeholders(text) {
				if seenInput[key] {
					continue
				}
				seenInput[key] = true
				in, ok := defined[key]
				if !ok {
					in = model.CrewInput{Key: key, Label: key, Required: true}
				}
				crew.Inputs = append(crew.Inputs, in)
			}
		}
	}
	for _, id := range order {
		task := lib.Tasks[id]
		addInputs(task.Description, task.ExpectedOutput)
		if task.AgentID != nil && !slices.Contains(crew.AgentIDs, *task.AgentID) {
			crew.AgentIDs = append(crew.AgentIDs, *task.AgentID)
			if agent, ok := lib.Agents[*task.AgentID]; ok {
				addInputs(agent.Role, agent.Goal, agent.Backstory)
			}
		}
	}
	return crew, problems
}
