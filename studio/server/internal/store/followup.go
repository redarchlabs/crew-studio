package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// WorkOrderConversation returns a person's conversation about a work order; ErrNotFound when they have none yet.
func (s *Store) WorkOrderConversation(ctx context.Context, workOrderID, userID string) (model.Conversation, error) {
	c, err := scanConversation(s.pool.QueryRow(ctx,
		`SELECT `+conversationColumns+conversationFrom+`
		 WHERE cv.work_order_id = $1 AND cv.user_id = $2`, workOrderID, userID))
	return c, mapErr(err, "conversation", "")
}

// EnsureWorkOrderConversation returns the person's conversation about a work order, creating it with the crew.
func (s *Store) EnsureWorkOrderConversation(ctx context.Context, crewID, userID, workOrderID, title string) (model.Conversation, error) {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO conversations (crew_id, user_id, work_order_id, title) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (work_order_id, user_id) WHERE work_order_id IS NOT NULL DO NOTHING`,
		crewID, userID, workOrderID, title); err != nil {
		return model.Conversation{}, mapErr(err, "conversation", "")
	}
	return s.WorkOrderConversation(ctx, workOrderID, userID)
}

const feedbackColumns = `id, work_order_id, run_id, revision_run_id, feedback, with_conversation, created_by_name, created_at`

func scanFeedback(row pgx.Row) (model.WorkOrderFeedback, error) {
	var f model.WorkOrderFeedback
	err := row.Scan(&f.ID, &f.WorkOrderID, &f.RunID, &f.RevisionRunID, &f.Feedback, &f.WithConversation, &f.CreatedByName, &f.CreatedAt)
	return f, err
}

// NewFeedback is feedback on an attempt of a work order.
type NewFeedback struct {
	WorkOrderID      string
	RunID            string
	Feedback         string
	WithConversation bool
	CreatedBy        string
	CreatedByName    string
}

// AddFeedback stores feedback on an attempt.
func (s *Store) AddFeedback(ctx context.Context, in NewFeedback) (model.WorkOrderFeedback, error) {
	f, err := scanFeedback(s.pool.QueryRow(ctx,
		`INSERT INTO work_order_feedback (work_order_id, run_id, feedback, with_conversation, created_by, created_by_name)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+feedbackColumns,
		in.WorkOrderID, nullableUUID(in.RunID), in.Feedback, in.WithConversation, nullableUUID(in.CreatedBy), in.CreatedByName))
	return f, mapErr(err, "feedback", "")
}

// SetFeedbackRevision records the attempt started to act on feedback.
func (s *Store) SetFeedbackRevision(ctx context.Context, feedbackID, runID string) (model.WorkOrderFeedback, error) {
	f, err := scanFeedback(s.pool.QueryRow(ctx,
		`UPDATE work_order_feedback SET revision_run_id = $2 WHERE id = $1 RETURNING `+feedbackColumns, feedbackID, runID))
	return f, mapErr(err, "feedback", "")
}

// DeleteFeedback removes feedback whose revision could not be started, so it does not look acted on.
func (s *Store) DeleteFeedback(ctx context.Context, feedbackID string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM work_order_feedback WHERE id = $1`, feedbackID); err != nil {
		return fmt.Errorf("delete feedback: %w", err)
	}
	return nil
}

// ListFeedback returns a work order's feedback, oldest first.
func (s *Store) ListFeedback(ctx context.Context, workOrderID string) ([]model.WorkOrderFeedback, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+feedbackColumns+` FROM work_order_feedback WHERE work_order_id = $1 ORDER BY created_at, id LIMIT 200`, workOrderID)
	if err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.WorkOrderFeedback, error) { return scanFeedback(r) })
	if err != nil {
		return nil, fmt.Errorf("read feedback: %w", err)
	}
	return orEmpty(items), nil
}
