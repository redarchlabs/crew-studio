package model

import (
	"strings"
	"time"
)

// Agent chat (docs/agent-chat-design.md): people talk to one agent directly. In each turn the agent may ask its
// specialists (other agents, in the same chat run) and propose a work order on one of the crews it may start.

const (
	// MaxChatSpecialists caps the agents a chatting agent may ask
	MaxChatSpecialists = 8
	// MaxChatCrews caps the crews a chatting agent may propose a work order on
	MaxChatCrews = 10
	// MaxChatConnections caps the connections a chatting agent may read through
	MaxChatConnections = 10

	// Hand-offs (docs/agent-chat-handoffs-design.md): what one may spend, how long it may run and how many may run at
	// once in one conversation, by default and at most
	DefaultHandoffBudgetUSD      = 0.5
	MaxHandoffBudgetUSD          = 100.0
	DefaultHandoffTimeoutMinutes = 20
	MaxHandoffTimeoutMinutes     = 180
	DefaultMaxActiveHandoffs     = 2
	MaxActiveHandoffsLimit       = 5

	// Questions during a hand-off (slice 3c): how long the lead waits for the person, and how many it may ask in one
	// hand-off, by default and at most. The rules and the timeout's limits are a crew's escalation's.
	DefaultQuestionTimeoutMinutes = 30
	DefaultChatMaxQuestions       = 3
)

// ChatNeedOf is what a connection of this kind meets for a tool's needs in agent chat: "tracker" (Jira, Linear, KM2
// issues), "km2" (KM2) or "mcp" (an MCP server, whose own read tools it brings; slice 3e), and "" for kinds no read
// tool uses. It is the one place that says which connection kinds an agent may list for chat
// (docs/agent-chat-handoffs-design.md, section 7).
func ChatNeedOf(kind string) string {
	switch {
	case IsTrackerKind(kind):
		return NeedTracker
	case kind == KindKM2:
		return NeedKM2
	case kind == KindMCP:
		return NeedMCP
	}
	return ""
}

// AgentChatSettings say whether people can chat with an agent, which agents it may ask, which crews it may start and
// what chat with it may spend.
type AgentChatSettings struct {
	AgentID string `json:"agent_id"`
	Enabled bool   `json:"enabled"`
	// LLMID, when set, replaces the agent's own model in chat; specialists keep their own
	LLMID *string `json:"llm_id"`
	// SpecialistIDs are the agents it may ask, in order
	SpecialistIDs []string `json:"specialist_ids"`
	// CrewIDs are the crews it may propose a work order on, in order
	CrewIDs []string `json:"crew_ids"`
	// ConnectionIDs are the connections its tracker and KM2 read tools may use in chat, in order. A turn uses only
	// those that are enabled and share an area with the person chatting and the agent.
	ConnectionIDs        []string `json:"connection_ids"`
	DailyBudgetUSD       float64  `json:"daily_budget_usd"`
	PersonDailyBudgetUSD float64  `json:"person_daily_budget_usd"`
	// HandoffsEnabled lets the agent propose hand-offs: background runs for work too long for one turn, each started
	// by the person's Confirm. HandoffBudgetUSD caps what one spends (the daily budgets still apply),
	// HandoffTimeoutMinutes how long it may run, and MaxActiveHandoffs how many run at once in one conversation.
	HandoffsEnabled       bool    `json:"handoffs_enabled"`
	HandoffBudgetUSD      float64 `json:"handoff_budget_usd"`
	HandoffTimeoutMinutes int     `json:"handoff_timeout_minutes"`
	MaxActiveHandoffs     int     `json:"max_active_handoffs"`
	// QuestionsEnabled lets the lead bring a specialist's question to the person during a hand-off, when
	// QuestionRules (plain language, required then) say so. The person has QuestionTimeoutMinutes to answer (never
	// past the hand-off's own deadline); without an answer the hand-off carries on with an assumption ("proceed") or
	// stops ("fail"). MaxQuestions caps the questions to the person in one hand-off.
	QuestionsEnabled       bool   `json:"questions_enabled"`
	QuestionRules          string `json:"question_rules"`
	QuestionTimeoutMinutes int    `json:"question_timeout_minutes"`
	QuestionOnTimeout      string `json:"question_on_timeout"`
	MaxQuestions           int    `json:"max_questions"`
	// SpecialistsMayPropose gives the specialists the proposing counterparts of their own changing tools (a tracker
	// comment, suggestion or new issue, a KM2 save), as the lead always gets them: each makes a Confirm card, and
	// nothing changes until the person confirms (docs/agent-chat-handoffs-design.md, section 7)
	SpecialistsMayPropose bool       `json:"specialists_may_propose"`
	UpdatedByName         string     `json:"updated_by_name"`
	UpdatedAt             *time.Time `json:"updated_at"`
	// HiddenSpecialists, HiddenCrews and HiddenConnections count the specialists, crews and connections outside the
	// reader's areas, which are left out of the lists above. Saving keeps them. Read only.
	HiddenSpecialists int `json:"hidden_specialists"`
	HiddenCrews       int `json:"hidden_crews"`
	HiddenConnections int `json:"hidden_connections"`
}

// DefaultAgentChatSettings is an agent's chat before anyone sets it up: off.
func DefaultAgentChatSettings(agentID string) AgentChatSettings {
	return AgentChatSettings{AgentID: agentID, SpecialistIDs: []string{}, CrewIDs: []string{}, ConnectionIDs: []string{},
		DailyBudgetUSD: DefaultChatDailyBudgetUSD, PersonDailyBudgetUSD: DefaultChatPersonBudgetUSD,
		HandoffBudgetUSD: DefaultHandoffBudgetUSD, HandoffTimeoutMinutes: DefaultHandoffTimeoutMinutes, MaxActiveHandoffs: DefaultMaxActiveHandoffs,
		QuestionTimeoutMinutes: DefaultQuestionTimeoutMinutes, QuestionOnTimeout: EscalationProceed, MaxQuestions: DefaultChatMaxQuestions}
}

// QuestionEscalation is what a hand-off of the agent freezes into its run (RunSpec.Escalation) so the lead may ask the
// person, in the shape the runner's Ask a human tool reads; nil when questions are off.
func (s AgentChatSettings) QuestionEscalation() *EscalationSpec {
	if !s.QuestionsEnabled {
		return nil
	}
	return &EscalationSpec{Rules: s.QuestionRules, TimeoutMinutes: s.QuestionTimeoutMinutes, OnTimeout: s.QuestionOnTimeout,
		MaxQuestions: s.MaxQuestions}
}

// NormalizeAgentChatSettings validates an agent's chat settings (not whether the agents and crews exist) and
// returns a cleaned copy.
func NormalizeAgentChatSettings(in AgentChatSettings) (AgentChatSettings, error) {
	out := in
	out.LLMID = trimmedPtr(in.LLMID)
	var c checker
	c.optionalUUID("llm_id", out.LLMID)
	out.SpecialistIDs = c.uuidList("specialist_ids", in.SpecialistIDs, in.AgentID)
	out.CrewIDs = c.uuidList("crew_ids", in.CrewIDs, "")
	out.ConnectionIDs = c.uuidList("connection_ids", in.ConnectionIDs, "")
	if len(out.SpecialistIDs) > MaxChatSpecialists {
		c.addf("specialist_ids", "at most %d", MaxChatSpecialists)
	}
	if len(out.CrewIDs) > MaxChatCrews {
		c.addf("crew_ids", "at most %d", MaxChatCrews)
	}
	if len(out.ConnectionIDs) > MaxChatConnections {
		c.addf("connection_ids", "at most %d", MaxChatConnections)
	}
	if out.DailyBudgetUSD <= 0 || out.DailyBudgetUSD > MaxChatDailyBudgetUSD {
		c.addf("daily_budget_usd", "must be more than 0 and at most %g", MaxChatDailyBudgetUSD)
	}
	if out.PersonDailyBudgetUSD == 0 {
		out.PersonDailyBudgetUSD = DefaultChatPersonBudgetUSD
	}
	if out.PersonDailyBudgetUSD < 0 || out.PersonDailyBudgetUSD > MaxChatPersonBudgetUSD {
		c.addf("person_daily_budget_usd", "must be more than 0 and at most %g", MaxChatPersonBudgetUSD)
	}
	out = normalizeHandoffSettings(out, &c)
	out = normalizeQuestionSettings(out, &c)
	return out, c.err()
}

// normalizeQuestionSettings fills in the question settings left out (as an older client sends them) and checks them as
// a crew's escalation is checked: rules are required when questions are on.
func normalizeQuestionSettings(in AgentChatSettings, c *checker) AgentChatSettings {
	out := in
	out.QuestionRules = strings.TrimSpace(in.QuestionRules)
	if out.QuestionTimeoutMinutes == 0 {
		out.QuestionTimeoutMinutes = DefaultQuestionTimeoutMinutes
	}
	if out.QuestionOnTimeout == "" {
		out.QuestionOnTimeout = EscalationProceed
	}
	if out.MaxQuestions == 0 {
		out.MaxQuestions = DefaultChatMaxQuestions
	}
	if out.QuestionsEnabled && out.QuestionRules == "" {
		c.addf("question_rules", "say when the lead should ask the person")
	}
	if len([]rune(out.QuestionRules)) > MaxEscalationRulesLen {
		c.addf("question_rules", "must be at most %d characters", MaxEscalationRulesLen)
	}
	if out.QuestionTimeoutMinutes < 1 || out.QuestionTimeoutMinutes > MaxEscalationTimeoutMinutes {
		c.addf("question_timeout_minutes", "must be between 1 and %d", MaxEscalationTimeoutMinutes)
	}
	if out.QuestionOnTimeout != EscalationProceed && out.QuestionOnTimeout != EscalationFail {
		c.addf("question_on_timeout", "must be %q or %q", EscalationProceed, EscalationFail)
	}
	if out.MaxQuestions < 1 || out.MaxQuestions > MaxEscalationMaxQuestions {
		c.addf("max_questions", "must be between 1 and %d", MaxEscalationMaxQuestions)
	}
	return out
}

// normalizeHandoffSettings fills in the hand-off settings left out (0, as an older client sends them) and checks the
// ranges of those given.
func normalizeHandoffSettings(in AgentChatSettings, c *checker) AgentChatSettings {
	out := in
	if out.HandoffBudgetUSD == 0 {
		out.HandoffBudgetUSD = DefaultHandoffBudgetUSD
	}
	if out.HandoffTimeoutMinutes == 0 {
		out.HandoffTimeoutMinutes = DefaultHandoffTimeoutMinutes
	}
	if out.MaxActiveHandoffs == 0 {
		out.MaxActiveHandoffs = DefaultMaxActiveHandoffs
	}
	if out.HandoffBudgetUSD < 0 || out.HandoffBudgetUSD > MaxHandoffBudgetUSD {
		c.addf("handoff_budget_usd", "must be more than 0 and at most %g", MaxHandoffBudgetUSD)
	}
	if out.HandoffTimeoutMinutes < 1 || out.HandoffTimeoutMinutes > MaxHandoffTimeoutMinutes {
		c.addf("handoff_timeout_minutes", "must be between 1 and %d", MaxHandoffTimeoutMinutes)
	}
	if out.MaxActiveHandoffs < 1 || out.MaxActiveHandoffs > MaxActiveHandoffsLimit {
		c.addf("max_active_handoffs", "must be between 1 and %d", MaxActiveHandoffsLimit)
	}
	return out
}

// ChatRoleKey is a role as CrewAI matches coworkers: whitespace collapsed, quotes dropped, case folded. Two agents
// in one agent-chat turn need different keys.
func ChatRoleKey(role string) string {
	return strings.ToLower(strings.ReplaceAll(strings.Join(strings.Fields(role), " "), `"`, ""))
}

// ChatAgent is an agent someone can chat with, with the specialists and crews it may use that they can see.
type ChatAgent struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Role        string        `json:"role"`
	Goal        string        `json:"goal"`
	Specialists []ChatNameRef `json:"specialists"`
	Crews       []ChatNameRef `json:"crews"`
}

// ChatNameRef names an agent or crew.
type ChatNameRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Role is set for agents
	Role string `json:"role,omitempty"`
}

// ChatAgentSpec is what an agent-chat turn may use besides the lead agent (RunSpec.Agents[0]): its specialists
// (RunSpec.Agents[1:], in this order) and the crews it may propose a work order on.
type ChatAgentSpec struct {
	AgentID     string           `json:"agent_id"`
	Specialists []ChatSpecialist `json:"specialists"`
	Crews       []ChatCrewOption `json:"crews"`
}

// ChatSpecialist is one agent the lead may ask in a turn.
type ChatSpecialist struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
}

// ChatCrewOption is one crew the lead may propose a work order on in a turn.
type ChatCrewOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Inputs are the crew's run inputs, for the lead to fill in
	Inputs []ChatInput `json:"inputs"`
}

// ChatInput is one of a crew's run inputs as a chat turn describes it to the agent proposing a work order, which
// must fill in the required ones. Defaults are left out: the agent should take values from the conversation.
type ChatInput struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

// ChatInputs describes a crew's run inputs for a chat turn.
func ChatInputs(inputs []CrewInput) []ChatInput {
	out := make([]ChatInput, 0, len(inputs))
	for _, in := range inputs {
		out = append(out, ChatInput{Key: in.Key, Label: in.Label, Required: in.Required})
	}
	return out
}
