package model

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Field length limits. Prompts can be long, labels cannot.
const (
	maxNameLen        = 120
	maxModelLen       = 200
	maxShortTextLen   = 5_000
	maxPromptTextLen  = 50_000
	maxNotesLen       = 2_000
	maxBudgetUSD      = 1_000
	maxPricePerMTok   = 100_000
	maxTokens         = 2_000_000
	maxIterLimit      = 100
	maxConcurrentRuns = 10
	// An agent's context may be compacted automatically above MinCompactContextTokens to MaxCompactContextTokens
	MinCompactContextTokens = 10_000
	MaxCompactContextTokens = 1_000_000
	maxInputs               = 50
)

var (
	uuidPattern     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	inputKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	// Same pattern CrewAI uses to find {placeholders}, so validation matches what the run does
	placeholderPattern = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_\-]*)}`)
	reasoningEfforts   = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
)

// ValidationError lists every problem found, as "field: problem" strings.
type ValidationError struct {
	Details []string
}

func (e *ValidationError) Error() string {
	return "validation failed: " + strings.Join(e.Details, "; ")
}

type checker struct {
	details []string
}

func (c *checker) addf(field, format string, args ...any) {
	c.details = append(c.details, field+": "+fmt.Sprintf(format, args...))
}

func (c *checker) err() error {
	if len(c.details) == 0 {
		return nil
	}
	return &ValidationError{Details: c.details}
}

func (c *checker) text(field, value string, minLen, maxLen int) {
	n := len([]rune(value))
	switch {
	case n < minLen && minLen == 1:
		c.addf(field, "is required")
	case n < minLen:
		c.addf(field, "must be at least %d characters", minLen)
	case n > maxLen:
		c.addf(field, "must be at most %d characters", maxLen)
	}
}

func (c *checker) identifier(field, value string) {
	c.text(field, value, 1, maxModelLen)
	if strings.ContainsAny(value, " \t\r\n") {
		c.addf(field, "must not contain spaces")
	}
}

func (c *checker) floatRange(field string, value, minVal, maxVal float64) {
	if value < minVal || value > maxVal {
		c.addf(field, "must be between %g and %g", minVal, maxVal)
	}
}

func (c *checker) positiveBudget(field string, value float64) {
	if value <= 0 || value > maxBudgetUSD {
		c.addf(field, "must be more than 0 and at most %d", maxBudgetUSD)
	}
}

func (c *checker) optionalUUID(field string, value *string) {
	if value != nil && !uuidPattern.MatchString(*value) {
		c.addf(field, "must be a UUID")
	}
}

// uuidList checks a list of IDs and returns a copy (never nil).
func (c *checker) uuidList(field string, ids []string, exclude string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		switch {
		case !uuidPattern.MatchString(id):
			c.addf(field, "%q is not a UUID", id)
		case exclude != "" && id == exclude:
			c.addf(field, "must not reference itself")
		case slices.Contains(out, id):
			c.addf(field, "lists %s more than once", id)
		default:
			out = append(out, id)
		}
	}
	return out
}

func trimmedPtr(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	return &s
}

// NormalizeLLM trims and validates an LLM config, returning a cleaned copy.
func NormalizeLLM(in LLM) (LLM, error) {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Model = strings.TrimSpace(in.Model)

	var c checker
	c.text("name", out.Name, 1, maxNameLen)
	c.identifier("model", out.Model)
	if out.Temperature != nil {
		c.floatRange("temperature", *out.Temperature, 0, 2)
	}
	if out.MaxCompletionTokens != nil && (*out.MaxCompletionTokens < 1 || *out.MaxCompletionTokens > maxTokens) {
		c.addf("max_completion_tokens", "must be between 1 and %d", maxTokens)
	}
	out.ReasoningEffort = trimmedPtr(in.ReasoningEffort)
	if out.ReasoningEffort != nil && !slices.Contains(reasoningEfforts, *out.ReasoningEffort) {
		c.addf("reasoning_effort", "must be one of %s, or empty", strings.Join(reasoningEfforts, ", "))
	}
	out.API = strings.TrimSpace(in.API)
	switch out.API {
	case "":
		out.API = APICompletions
	case APICompletions:
	case APIResponses:
		if !strings.HasPrefix(out.Model, "openai/") {
			c.addf("api", "the Responses API is only for openai/ models")
		}
	default:
		c.addf("api", "must be %q or %q", APICompletions, APIResponses)
	}
	return out, c.err()
}

// NormalizePrice trims and validates a model price, returning a cleaned copy.
func NormalizePrice(in ModelPrice) (ModelPrice, error) {
	out := in
	out.Model = strings.TrimSpace(in.Model)
	out.Notes = strings.TrimSpace(in.Notes)

	var c checker
	c.identifier("model", out.Model)
	c.floatRange("input_per_mtok", out.InputPerMTok, 0, maxPricePerMTok)
	c.floatRange("cached_input_per_mtok", out.CachedInputPerMTok, 0, maxPricePerMTok)
	if out.CacheWritePerMTok != nil {
		c.floatRange("cache_write_per_mtok", *out.CacheWritePerMTok, 0, maxPricePerMTok)
	}
	c.floatRange("output_per_mtok", out.OutputPerMTok, 0, maxPricePerMTok)
	c.text("notes", out.Notes, 0, maxNotesLen)
	return out, c.err()
}

// NormalizeSettings validates the global settings, returning a cleaned copy.
func NormalizeSettings(in Settings) (Settings, error) {
	out := in
	out.SearchModel = strings.TrimSpace(in.SearchModel)

	var c checker
	c.positiveBudget("default_budget_usd", out.DefaultBudgetUSD)
	c.floatRange("hard_stop_multiplier", out.HardStopMultiplier, 1, 5)
	c.identifier("search_model", out.SearchModel)
	c.floatRange("grounded_search_cost_per_1000", out.GroundedSearchCostPer1000, 0, 10_000)
	if out.GroundedSearchesFree < 0 || out.GroundedSearchesFree > 1_000_000 {
		c.addf("grounded_searches_free", "must be between 0 and 1000000")
	}
	out.GroundingFreePeriod = strings.TrimSpace(in.GroundingFreePeriod)
	if out.GroundingFreePeriod != GroundingPeriodDay && out.GroundingFreePeriod != GroundingPeriodMonth {
		c.addf("grounding_free_period", "must be %q or %q", GroundingPeriodDay, GroundingPeriodMonth)
	}
	if out.MaxConcurrentRuns < 1 || out.MaxConcurrentRuns > maxConcurrentRuns {
		c.addf("max_concurrent_runs", "must be between 1 and %d", maxConcurrentRuns)
	}
	if out.CompactContextTokens != 0 && (out.CompactContextTokens < MinCompactContextTokens || out.CompactContextTokens > MaxCompactContextTokens) {
		c.addf("compact_context_tokens", "must be 0 (never) or between %d and %d", MinCompactContextTokens, MaxCompactContextTokens)
	}
	return out, c.err()
}

// NormalizeAgent validates an agent against the known tool keys, returning a cleaned copy.
func NormalizeAgent(in Agent, knownTools map[string]bool) (Agent, error) {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Role = strings.TrimSpace(in.Role)
	out.Goal = strings.TrimSpace(in.Goal)
	out.Backstory = strings.TrimSpace(in.Backstory)
	out.LLMID = trimmedPtr(in.LLMID)

	var c checker
	c.text("name", out.Name, 1, maxNameLen)
	c.text("role", out.Role, 1, maxShortTextLen)
	c.text("goal", out.Goal, 1, maxPromptTextLen)
	c.text("backstory", out.Backstory, 1, maxPromptTextLen)
	c.optionalUUID("llm_id", out.LLMID)
	if out.MaxIter < 1 || out.MaxIter > maxIterLimit {
		c.addf("max_iter", "must be between 1 and %d", maxIterLimit)
	}

	tools := make([]string, 0, len(in.Tools))
	for _, key := range in.Tools {
		key = strings.TrimSpace(key)
		switch {
		case !knownTools[key]:
			c.addf("tools", "unknown tool %q", key)
		case !slices.Contains(tools, key):
			tools = append(tools, key)
		}
	}
	out.Tools = tools
	out.AreaIDs = c.uuidList("area_ids", in.AreaIDs, "")
	return out, c.err()
}

// NormalizeTask validates a task, returning a cleaned copy.
func NormalizeTask(in Task) (Task, error) {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Description = strings.TrimSpace(in.Description)
	out.ExpectedOutput = strings.TrimSpace(in.ExpectedOutput)
	out.AgentID = trimmedPtr(in.AgentID)

	var c checker
	c.text("name", out.Name, 1, maxNameLen)
	c.text("description", out.Description, 1, maxPromptTextLen)
	c.text("expected_output", out.ExpectedOutput, 1, maxPromptTextLen)
	c.optionalUUID("agent_id", out.AgentID)
	out.ContextTaskIDs = c.uuidList("context_task_ids", in.ContextTaskIDs, in.ID)
	out.AreaIDs = c.uuidList("area_ids", in.AreaIDs, "")
	return out, c.err()
}

// NormalizeCrew validates a crew definition (not whether it can run), returning a cleaned copy.
func NormalizeCrew(in Crew) (Crew, error) {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Description = strings.TrimSpace(in.Description)
	out.Process = strings.TrimSpace(in.Process)
	out.ManagerLLMID = trimmedPtr(in.ManagerLLMID)

	var c checker
	c.text("name", out.Name, 1, maxNameLen)
	c.text("description", out.Description, 0, maxShortTextLen)
	switch out.Process {
	case ProcessSequential:
	case ProcessHierarchical:
		if out.ManagerLLMID == nil {
			c.addf("manager_llm_id", "is required for a hierarchical crew")
		}
	default:
		c.addf("process", "must be %q or %q", ProcessSequential, ProcessHierarchical)
	}
	c.optionalUUID("manager_llm_id", out.ManagerLLMID)
	c.positiveBudget("budget_usd", out.BudgetUSD)
	out.AgentIDs = c.uuidList("agent_ids", in.AgentIDs, "")
	out.TaskIDs = c.uuidList("task_ids", in.TaskIDs, "")
	out.Inputs = normalizeInputs(&c, in.Inputs)
	out.AreaIDs = c.uuidList("area_ids", in.AreaIDs, "")
	return out, c.err()
}

func normalizeInputs(c *checker, inputs []CrewInput) []CrewInput {
	if len(inputs) > maxInputs {
		c.addf("inputs", "at most %d inputs", maxInputs)
	}
	out := make([]CrewInput, 0, len(inputs))
	seen := map[string]bool{}
	for i, in := range inputs {
		field := fmt.Sprintf("inputs[%d]", i)
		item := CrewInput{
			Key:      strings.TrimSpace(in.Key),
			Label:    strings.TrimSpace(in.Label),
			Default:  in.Default,
			Required: in.Required,
		}
		switch {
		case !inputKeyPattern.MatchString(item.Key):
			c.addf(field+".key", "must start with a lowercase letter and use only a-z, 0-9 and _")
		case seen[item.Key]:
			c.addf(field+".key", "%q is used more than once", item.Key)
		}
		seen[item.Key] = true
		if item.Label == "" {
			item.Label = item.Key
		}
		c.text(field+".label", item.Label, 0, maxNameLen*2)
		c.text(field+".default", item.Default, 0, maxShortTextLen)
		out = append(out, item)
	}
	return out
}

// Placeholders returns the distinct {placeholder} names in text, in order of first use.
func Placeholders(text string) []string {
	var out []string
	for _, m := range placeholderPattern.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// Interpolate replaces {key} placeholders that have a value and leaves the rest alone.
func Interpolate(text string, values map[string]string) string {
	return placeholderPattern.ReplaceAllStringFunc(text, func(match string) string {
		if v, ok := values[match[1:len(match)-1]]; ok {
			return v
		}
		return match
	})
}
