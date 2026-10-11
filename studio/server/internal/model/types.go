// Package model holds the Crew Studio domain types shared by the store, API and runner.
// JSON field names match docs/api.md.
package model

import "time"

type LLM struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Model               string   `json:"model"`
	Temperature         *float64 `json:"temperature"`
	MaxCompletionTokens *int     `json:"max_completion_tokens"`
	ReasoningEffort     *string  `json:"reasoning_effort"`
	API                 string   `json:"api"`
	// Vision: the model accepts images and PDFs (work order files are given to it directly)
	Vision    bool      `json:"vision"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ModelPrice struct {
	ID                 string  `json:"id"`
	Model              string  `json:"model"`
	InputPerMTok       float64 `json:"input_per_mtok"`
	CachedInputPerMTok float64 `json:"cached_input_per_mtok"`
	// CacheWritePerMTok prices tokens written to a prompt cache (Anthropic charges extra); nil means the input price
	CacheWritePerMTok *float64  `json:"cache_write_per_mtok"`
	OutputPerMTok     float64   `json:"output_per_mtok"`
	Notes             string    `json:"notes"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Settings struct {
	DefaultBudgetUSD          float64 `json:"default_budget_usd"`
	HardStopMultiplier        float64 `json:"hard_stop_multiplier"`
	SearchModel               string  `json:"search_model"`
	GroundedSearchCostPer1000 float64 `json:"grounded_search_cost_per_1000"`
	GroundedSearchesFree      int     `json:"grounded_searches_free"`
	GroundingFreePeriod       string  `json:"grounding_free_period"`
	MaxConcurrentRuns         int     `json:"max_concurrent_runs"`
	// CompactContextTokens compacts an agent's context during a run once it passes this many tokens; 0 never does
	CompactContextTokens int `json:"compact_context_tokens"`
}

type Tool struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	RequiresEnv []string `json:"requires_env"`
	// PricedModel is a model the tool bills for (such as a video model); crews using the tool need its price
	PricedModel string `json:"priced_model,omitempty"`
	Available   bool   `json:"available"`
	// Effect is what the tool does outside the run: reads, proposes or changes (see tooleffect.go)
	Effect string `json:"effect"`
	// Needs is what it requires besides keys: crew, tracker, km2, schedule
	Needs []string `json:"needs"`
}

type Agent struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Role            string    `json:"role"`
	Goal            string    `json:"goal"`
	Backstory       string    `json:"backstory"`
	LLMID           *string   `json:"llm_id"`
	Tools           []string  `json:"tools"`
	MaxIter         int       `json:"max_iter"`
	AllowDelegation bool      `json:"allow_delegation"`
	Verbose         bool      `json:"verbose"`
	AreaIDs         []string  `json:"area_ids"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Task struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	ExpectedOutput string    `json:"expected_output"`
	AgentID        *string   `json:"agent_id"`
	ContextTaskIDs []string  `json:"context_task_ids"`
	Markdown       bool      `json:"markdown"`
	AreaIDs        []string  `json:"area_ids"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CrewInput struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Default  string `json:"default"`
	Required bool   `json:"required"`
}

const (
	APICompletions = "completions"
	APIResponses   = "responses"

	GroundingPeriodDay   = "day"
	GroundingPeriodMonth = "month"

	ProcessSequential   = "sequential"
	ProcessHierarchical = "hierarchical"
)

type Crew struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Description  string      `json:"description"`
	Process      string      `json:"process"`
	ManagerLLMID *string     `json:"manager_llm_id"`
	AgentIDs     []string    `json:"agent_ids"`
	TaskIDs      []string    `json:"task_ids"`
	Inputs       []CrewInput `json:"inputs"`
	BudgetUSD    float64     `json:"budget_usd"`
	AreaIDs      []string    `json:"area_ids"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// Terminal reports whether a run in this status will never change again.
func (s RunStatus) Terminal() bool {
	return s == RunSucceeded || s == RunFailed || s == RunCancelled
}

type TokenUsage struct {
	PromptTokens       int `json:"prompt_tokens"`
	CompletionTokens   int `json:"completion_tokens"`
	CachedPromptTokens int `json:"cached_prompt_tokens"`
	// CacheWriteTokens are prompt tokens written to the provider's prompt cache (part of PromptTokens)
	CacheWriteTokens   int `json:"cache_write_tokens"`
	SuccessfulRequests int `json:"successful_requests"`
}

type TaskOutput struct {
	Position  int    `json:"position"`
	TaskName  string `json:"task_name"`
	AgentRole string `json:"agent_role"`
	Raw       string `json:"raw"`
	// CostUSD is what the task cost when it ran (unknown for runs from before it was recorded)
	CostUSD *float64 `json:"cost_usd,omitempty"`
	// ReusedFromRunID is the run that produced this output when a resumed run copied it instead of running the task;
	// ReusedFromUnavailable says that run has since been deleted or is not one the reader may open
	ReusedFromRunID       *string `json:"reused_from_run_id,omitempty"`
	ReusedFromUnavailable bool    `json:"reused_from_unavailable,omitempty"`
}

// RunSpec is everything a run started with, frozen so later edits do not rewrite history.
type RunSpec struct {
	Crew      Crew              `json:"crew"`
	Agents    []Agent           `json:"agents"`
	Tasks     []Task            `json:"tasks"`
	LLMs      []LLM             `json:"llms"`
	Pricing   []ModelPrice      `json:"pricing"`
	Settings  Settings          `json:"settings"`
	Inputs    map[string]string `json:"inputs"`
	BudgetUSD float64           `json:"budget_usd"`
	// WorkOrder is set when the run executes a work order: its brief and input files go to the agents
	WorkOrder *WorkOrderSpec `json:"work_order,omitempty"`
	// Chat is set for a chat turn (kind "chat"): one agent answers one message; Tasks is empty
	Chat *ChatSpec `json:"chat,omitempty"`
	// Memory is the crew's memory settings when memory is on (and the server can embed); never for chat turns
	Memory *MemorySpec `json:"memory,omitempty"`
	// Escalation is the crew's rules for asking a person, when escalation is on; for a hand-off, the agent's question
	// settings when its questions are on (docs/agent-chat-handoffs-design.md, section 6); never for chat turns
	Escalation *EscalationSpec `json:"escalation,omitempty"`
	// Resume is set when the run resumes a failed or cancelled run: the tasks before StartPosition are not run again
	Resume *ResumeSpec `json:"resume,omitempty"`
}

type Run struct {
	ID            string            `json:"id"`
	CrewID        *string           `json:"crew_id"`
	CrewName      string            `json:"crew_name"`
	Status        RunStatus         `json:"status"`
	Inputs        map[string]string `json:"inputs"`
	BudgetUSD     float64           `json:"budget_usd"`
	Output        *string           `json:"output"`
	Error         *string           `json:"error"`
	CostUSD       *float64          `json:"cost_usd"`
	Usage         *TokenUsage       `json:"usage"`
	AreaIDs       []string          `json:"area_ids"`
	CreatedBy     *string           `json:"created_by"`
	CreatedByName string            `json:"created_by_name"`
	TriggerType   string            `json:"trigger_type"`
	// Kind is "crew" for crew runs and "chat" for one turn of a crew chat
	Kind        string  `json:"kind"`
	WorkOrderID *string `json:"work_order_id"`
	// APIKeyID is the key that started the run, whose limits still apply while it runs
	APIKeyID   *string    `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	// A pointer so run detail always includes the list (even empty) while run lists leave it out
	TaskOutputs *[]TaskOutput `json:"task_outputs,omitempty"`
	Spec        *RunSpec      `json:"spec,omitempty"`
	// Run detail only: the run this one resumed and the first task it ran, the runs that resumed this one (newest
	// first), and where this one could be resumed from now
	ResumedFromRunID *string     `json:"resumed_from_run_id,omitempty"`
	ResumePosition   *int        `json:"resume_position,omitempty"`
	ResumedBy        *[]string   `json:"resumed_by,omitempty"`
	Resumable        *ResumeHint `json:"resumable,omitempty"`
	// TaskCosts is what the run spent on each task it worked on, by task name, finished or not (run detail only)
	TaskCosts map[string]float64 `json:"task_costs,omitempty"`
}

type RunEvent struct {
	Seq       int            `json:"seq"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

type LLMCall struct {
	Seq       int        `json:"seq"`
	AgentRole string     `json:"agent_role"`
	TaskName  string     `json:"task_name"`
	Model     string     `json:"model"`
	Usage     TokenUsage `json:"usage"`
	CostUSD   float64    `json:"cost_usd"`
	Messages  any        `json:"messages"`
	Response  string     `json:"response"`
	CreatedAt time.Time  `json:"created_at"`
}

type Revision struct {
	ID         int64          `json:"id"`
	EntityType string         `json:"entity_type"`
	EntityID   string         `json:"entity_id"`
	Action     string         `json:"action"`
	Snapshot   map[string]any `json:"snapshot"`
	ActorID    *string        `json:"actor_id"`
	ActorName  string         `json:"actor_name"`
	CreatedAt  time.Time      `json:"created_at"`
}

type Counts struct {
	WorkOrders int `json:"work_orders"`
	Schedules  int `json:"schedules"`
	LLMs       int `json:"llms"`
	Pricing    int `json:"pricing"`
	Agents     int `json:"agents"`
	Tasks      int `json:"tasks"`
	Crews      int `json:"crews"`
	Runs       int `json:"runs"`
}

type Overview struct {
	Counts Counts `json:"counts"`
	// RecentRuns are crew runs only; chat turns would crowd them out
	RecentRuns       []Run       `json:"recent_runs"`
	RecentWorkOrders []WorkOrder `json:"recent_work_orders"`
	// SpendLast30dUSD includes chat turns; ChatSpendLast30dUSD is their share
	SpendLast30dUSD     float64 `json:"spend_last_30d_usd"`
	ChatSpendLast30dUSD float64 `json:"chat_spend_last_30d_usd"`
}

// Page describes one slice of a paginated list.
type Page struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}
