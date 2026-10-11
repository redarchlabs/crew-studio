package model

import (
	"fmt"
	"strings"
	"time"
)

const (
	MaxFeedbackLen = 8_000
	// MaxRevisionOutputChars cuts the previous attempt's output given to a revision (and to a work order chat)
	MaxRevisionOutputChars = 20_000
)

// RevisionSpec is what an attempt started from feedback gets: the attempt it revises, its output and the feedback.
type RevisionSpec struct {
	// Attempt is the number of the attempt being revised (1 for the first)
	Attempt        int    `json:"attempt"`
	PreviousOutput string `json:"previous_output"`
	Feedback       string `json:"feedback"`
	FeedbackBy     string `json:"feedback_by"`
	// Conversation is the person's latest messages with the crew about the work order, when they chose to include it
	Conversation []ChatTurn `json:"conversation,omitempty"`
}

// ChatWorkOrder is the work order a chat turn is about: its brief and its latest attempt's output.
type ChatWorkOrder struct {
	Number int64  `json:"number"`
	Title  string `json:"title"`
	Brief  string `json:"brief"`
	// Attempt is the number of the latest attempt, Status its status and Output its output (cut), if any
	Attempt int    `json:"attempt"`
	Status  string `json:"status"`
	Output  string `json:"output"`
}

// WorkOrderFeedback is feedback someone gave on an attempt, and the attempt it started.
type WorkOrderFeedback struct {
	ID               string    `json:"id"`
	WorkOrderID      string    `json:"work_order_id"`
	RunID            *string   `json:"run_id"`
	RevisionRunID    *string   `json:"revision_run_id"`
	Feedback         string    `json:"feedback"`
	WithConversation bool      `json:"with_conversation"`
	CreatedByName    string    `json:"created_by_name"`
	CreatedAt        time.Time `json:"created_at"`
}

// NormalizeFeedback trims feedback and checks its length.
func NormalizeFeedback(text string) (string, error) {
	clean := strings.TrimSpace(text)
	if clean == "" || len([]rune(clean)) > MaxFeedbackLen {
		return "", &ValidationError{Details: []string{fmt.Sprintf("feedback: say what should change (at most %d characters)", MaxFeedbackLen)}}
	}
	return clean, nil
}

// CutText keeps the first n characters of s, marking the cut.
func CutText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n[… cut]"
}
