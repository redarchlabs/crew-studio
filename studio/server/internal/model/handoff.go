package model

import "time"

// Hand-offs (docs/agent-chat-handoffs-design.md, slice 3b): in agent chat, work too long for one turn goes to a
// background run of kind "handoff" once the person confirms the lead's card. Its result is an assistant message
// created empty with the hand-off and filled from the run's output, as replies are.

// Handoff is one hand-off with the state of its run.
type Handoff struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	// RequestMessageID is the reply whose card started it (null once that message is gone)
	RequestMessageID *string `json:"request_message_id"`
	// ResultMessageID is the message its result is written into
	ResultMessageID string `json:"result_message_id"`
	// RunID and RunStatus are null once the run was deleted (after a day, from the Runs page); the result is kept
	RunID          *string `json:"run_id"`
	WorkerAgentID  *string `json:"worker_agent_id"`
	WorkerName     string  `json:"worker_name"`
	WorkerRole     string  `json:"worker_role"`
	Title          string  `json:"title"`
	Brief          string  `json:"brief"`
	BudgetUSD      float64 `json:"budget_usd"`
	TimeoutMinutes int     `json:"timeout_minutes"`
	// DeadlineAt is when it must stop: TimeoutMinutes after its run started (after it was made, while queued)
	DeadlineAt time.Time  `json:"deadline_at"`
	CreatedAt  time.Time  `json:"created_at"`
	RunStatus  *RunStatus `json:"run_status"`
	RunError   string     `json:"run_error,omitempty"`
	CostUSD    *float64   `json:"cost_usd"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	// OpenQuestionID is the question its run is waiting on the person to answer, if any (slice 3c)
	OpenQuestionID *string `json:"open_question_id"`
}

// HandoffRef is what a hand-off's result message says about it.
type HandoffRef struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	WorkerName     string     `json:"worker_name"`
	WorkerRole     string     `json:"worker_role"`
	DeadlineAt     time.Time  `json:"deadline_at"`
	BudgetUSD      float64    `json:"budget_usd"`
	TimeoutMinutes int        `json:"timeout_minutes"`
	StartedAt      *time.Time `json:"started_at"`
	// OpenQuestionID is the question its run is waiting on the person to answer, if any (slice 3c)
	OpenQuestionID *string `json:"open_question_id"`
}

// ChatHandoffSpec is what a hand-off run works on: the brief, and the worker (the lead, RunSpec.Agents[0], or one of
// its specialists).
type ChatHandoffSpec struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Brief            string    `json:"brief"`
	WorkerAgentID    string    `json:"worker_agent_id"`
	WorkerName       string    `json:"worker_name"`
	WorkerRole       string    `json:"worker_role"`
	DeadlineAt       time.Time `json:"deadline_at"`
	TimeoutMinutes   int       `json:"timeout_minutes"`
	RequestMessageID string    `json:"request_message_id"`
}

// ChatHandoffRef is a hand-off still running, as a turn tells the lead about it.
type ChatHandoffRef struct {
	Title      string    `json:"title"`
	WorkerName string    `json:"worker_name"`
	WorkerRole string    `json:"worker_role"`
	StartedAt  time.Time `json:"started_at"`
	DeadlineAt time.Time `json:"deadline_at"`
}
