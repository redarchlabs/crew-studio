package model

import (
	"strings"
	"time"
)

const (
	EscalationProceed = "proceed"
	EscalationFail    = "fail"

	DefaultEscalationTimeoutMinutes = 30
	MaxEscalationTimeoutMinutes     = 180
	DefaultEscalationMaxQuestions   = 5
	MaxEscalationMaxQuestions       = 20
	MaxEscalationRulesLen           = 4000
	MaxQuestionLen                  = 2000
	MaxQuestionContextLen           = 4000
	MaxAnswerLen                    = 4000

	QuestionOpen     = "open"
	QuestionAnswered = "answered"
	QuestionTimedOut = "timed_out"
	// QuestionClosed is a question whose run ended before anyone answered it
	QuestionClosed = "closed"
)

// EscalationSettings say when a crew's agents stop and ask a person, and what happens when nobody answers.
type EscalationSettings struct {
	CrewID  string `json:"crew_id"`
	Enabled bool   `json:"enabled"`
	// Rules is plain language ("ask before assuming a budget"); it goes into every agent's instructions
	Rules          string     `json:"rules"`
	TimeoutMinutes int        `json:"timeout_minutes"`
	OnTimeout      string     `json:"on_timeout"`
	MaxQuestions   int        `json:"max_questions"`
	UpdatedByName  string     `json:"updated_by_name"`
	UpdatedAt      *time.Time `json:"updated_at"`
}

// DefaultEscalationSettings is a crew's escalation before anyone sets it up: off.
func DefaultEscalationSettings(crewID string) EscalationSettings {
	return EscalationSettings{
		CrewID: crewID, TimeoutMinutes: DefaultEscalationTimeoutMinutes, OnTimeout: EscalationProceed,
		MaxQuestions: DefaultEscalationMaxQuestions,
	}
}

// NormalizeEscalationSettings validates escalation settings and returns a cleaned copy.
func NormalizeEscalationSettings(in EscalationSettings) (EscalationSettings, error) {
	out := in
	out.Rules = strings.TrimSpace(in.Rules)
	if out.OnTimeout == "" {
		out.OnTimeout = EscalationProceed
	}
	var c checker
	if out.Enabled && out.Rules == "" {
		c.addf("rules", "say when the agents should ask a person")
	}
	if len([]rune(out.Rules)) > MaxEscalationRulesLen {
		c.addf("rules", "must be at most %d characters", MaxEscalationRulesLen)
	}
	if out.TimeoutMinutes < 1 || out.TimeoutMinutes > MaxEscalationTimeoutMinutes {
		c.addf("timeout_minutes", "must be between 1 and %d", MaxEscalationTimeoutMinutes)
	}
	if out.OnTimeout != EscalationProceed && out.OnTimeout != EscalationFail {
		c.addf("on_timeout", "must be %q or %q", EscalationProceed, EscalationFail)
	}
	if out.MaxQuestions < 1 || out.MaxQuestions > MaxEscalationMaxQuestions {
		c.addf("max_questions", "must be between 1 and %d", MaxEscalationMaxQuestions)
	}
	return out, c.err()
}

// EscalationSpec is the escalation frozen into a run's spec when it is on.
type EscalationSpec struct {
	Rules          string `json:"rules"`
	TimeoutMinutes int    `json:"timeout_minutes"`
	OnTimeout      string `json:"on_timeout"`
	MaxQuestions   int    `json:"max_questions"`
}

// HumanQuestion is one question an agent asked a person during a run.
type HumanQuestion struct {
	ID              string  `json:"id"`
	RunID           string  `json:"run_id"`
	WorkOrderID     *string `json:"work_order_id"`
	WorkOrderNumber *int    `json:"work_order_number"`
	WorkOrderTitle  string  `json:"work_order_title"`
	CrewName        string  `json:"crew_name"`
	AssigneeID      *string `json:"assignee_id"`
	AgentRole       string  `json:"agent_role"`
	// AskedFor is the agent the question was asked for: in a hand-off the lead asks on behalf of a specialist
	// ("asked by AskedFor through AgentRole"); "" when the asking agent asked for itself
	AskedFor       string     `json:"asked_for"`
	TaskName       string     `json:"task_name"`
	Question       string     `json:"question"`
	Context        string     `json:"context"`
	Status         string     `json:"status"`
	Answer         string     `json:"answer"`
	AnsweredByName string     `json:"answered_by_name"`
	AskedAt        time.Time  `json:"asked_at"`
	DeadlineAt     time.Time  `json:"deadline_at"`
	AnsweredAt     *time.Time `json:"answered_at"`
	// ConversationID and HandoffID are set for a question from an agent chat's hand-off, so the person can answer it
	// in the conversation; null for a crew's question
	ConversationID    *string `json:"conversation_id"`
	ConversationTitle string  `json:"conversation_title"`
	HandoffID         *string `json:"handoff_id"`
	HandoffTitle      string  `json:"handoff_title"`
}
