package runspec

import (
	"fmt"
	"slices"
	"strings"

	"crewstudio/internal/model"
)

// CheapestAgentLLM is the id of the priced LLM of the crew's agents with the lowest input plus output price (by
// name on a tie), or "" when none is priced. Memory uses it when its settings name no model.
func CheapestAgentLLM(crew model.Crew, lib Library) string {
	best, bestCost, bestName := "", 0.0, ""
	for _, id := range crew.AgentIDs {
		agent, ok := lib.Agents[id]
		if !ok || agent.LLMID == nil {
			continue
		}
		llm, ok := lib.LLMs[*agent.LLMID]
		if !ok {
			continue
		}
		price := MatchPrice(llm.Model, lib.Pricing)
		if price == nil {
			continue
		}
		cost := price.InputPerMTok + price.OutputPerMTok
		if best == "" || cost < bestCost || (cost == bestCost && llm.Name < bestName) {
			best, bestCost, bestName = llm.ID, cost, llm.Name
		}
	}
	return best
}

// FreezeMemory adds the crew's memory settings to spec (nothing when memory is off). When memory calls a model
// (automatic mode or deep recall) that model is added to spec.LLMs and must have a price; problems say what is wrong.
func FreezeMemory(spec *model.RunSpec, settings model.MemorySettings, lib Library, embedModel string) []string {
	if settings.Mode == model.MemoryOff || settings.Mode == "" {
		return nil
	}
	mem := &model.MemorySpec{Mode: settings.Mode, RecallDepth: settings.RecallDepth, RecallLimit: settings.RecallLimit,
		SavesPerRun: settings.SavesPerRun, ReviewRunSaves: settings.ReviewRunSaves, RecencyWeight: settings.RecencyWeight,
		SemanticWeight: settings.SemanticWeight, ImportanceWeight: settings.ImportanceWeight,
		RecencyHalfLifeDays: settings.RecencyHalfLifeDays, EmbedModel: embedModel}
	if settings.NeedsLLM() {
		id := CheapestAgentLLM(spec.Crew, lib)
		if settings.LLMID != nil {
			id = *settings.LLMID
		}
		var problems []string
		add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
		if id == "" {
			add("Memory needs a model to extract or recall memories; pick one in the crew's memory settings.")
			return problems
		}
		checkLLMPrice(id, "Memory", lib, add)
		if len(problems) > 0 {
			return problems
		}
		mem.LLMID = id
		if !slices.ContainsFunc(spec.LLMs, func(l model.LLM) bool { return l.ID == id }) {
			spec.LLMs = append(spec.LLMs, lib.LLMs[id])
			slices.SortFunc(spec.LLMs, func(a, b model.LLM) int { return strings.Compare(a.Name, b.Name) })
		}
	}
	spec.Memory = mem
	return nil
}
