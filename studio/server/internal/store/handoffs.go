package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// Hand-offs (docs/agent-chat-handoffs-design.md, slice 3b): a background run of kind "handoff" started from an agent
// chat when the person confirms the lead's card. Its result is an assistant message created empty with the hand-off,
// linked to its run, and filled from the run's output by SettleReplies like any reply.

const handoffColumns = `h.id, h.conversation_id, h.request_message_id, COALESCE(m.id::text, ''), h.run_id, h.worker_agent_id,
	h.worker_name, h.worker_role, h.title, h.brief, h.budget_usd, h.timeout_minutes, h.deadline_at, h.created_at,
	r.status, COALESCE(r.error, ''), r.cost_usd, r.started_at, r.finished_at, ` + handoffOpenQuestion

// handoffOpenQuestion is the question a hand-off's run (h, r) is waiting on the person to answer, if any: the soonest
// due of those still open (not answered, not past their deadline, the run still going)
const handoffOpenQuestion = `(SELECT q.id::text FROM human_questions q WHERE q.run_id = h.run_id AND q.status = 'open'
	AND q.deadline_at > now() AND r.status IN ('queued', 'running') ORDER BY q.deadline_at LIMIT 1)`

// The result message is the hand-off's first message, made with it; the cards it proposes come after (slice 3d). The
// run may have been deleted since; the hand-off and its result are kept.
const handoffFrom = ` FROM chat_handoffs h LEFT JOIN runs r ON r.id = h.run_id
	LEFT JOIN LATERAL (SELECT e.id FROM conversation_messages e WHERE e.handoff_id = h.id ORDER BY e.seq LIMIT 1) m ON true`

func scanHandoff(row pgx.Row) (model.Handoff, error) {
	var h model.Handoff
	err := row.Scan(&h.ID, &h.ConversationID, &h.RequestMessageID, &h.ResultMessageID, &h.RunID, &h.WorkerAgentID,
		&h.WorkerName, &h.WorkerRole, &h.Title, &h.Brief, &h.BudgetUSD, &h.TimeoutMinutes, &h.DeadlineAt, &h.CreatedAt,
		&h.RunStatus, &h.RunError, &h.CostUSD, &h.StartedAt, &h.FinishedAt, &h.OpenQuestionID)
	h.RunError = shortError(h.RunError)
	return h, err
}

// NewHandoff is a hand-off to start: what it works on, its frozen run, and the limits it must fit in.
type NewHandoff struct {
	ConversationID string
	// Handoff says what to work on and who works; its ID and DeadlineAt are set here
	Handoff model.ChatHandoffSpec
	// Spec is the frozen run (its Chat section set); its budget is cut to what the hand-off may spend
	Spec model.RunSpec
	// Meta says who started it (the person) and for which agent (ChatAgentID)
	Meta RunMeta
	// HandoffBudgetUSD is what one hand-off may spend; DailyBudgetUSD and PersonBudgetUSD are the conversation's and
	// the person's daily chat budgets, which it must also fit in, with at least MinBudgetUSD left
	HandoffBudgetUSD float64
	DailyBudgetUSD   float64
	PersonBudgetUSD  float64
	MinBudgetUSD     float64
	// MaxActive caps hand-offs queued or running at once in the conversation, MaxPerson those of the person with
	// the agent in all their conversations
	MaxActive int
	MaxPerson int
}

// StartedHandoff is what CreateHandoff stored.
type StartedHandoff struct {
	Handoff model.Handoff
	Run     model.Run
	// Message is the empty result message the run's output will fill
	Message model.ChatMessage
}

var (
	errHandoffsBusy = &ConflictError{Code: "handoffs_busy",
		Message: "This conversation already has as many hand-offs running as the agent allows. Wait for one to finish, or cancel one."}
	errPersonHandoffsBusy = &ConflictError{Code: "handoffs_busy",
		Message: "You already have as many hand-offs with this agent running as one person may. Wait for one to finish, or cancel one."}
	errHandoffActive = &ConflictError{Code: "handoff_active",
		Message: "A hand-off of this conversation is still running. Cancel the hand-off first."}
	errHandoffWithoutOwner  = errors.New("store: a hand-off needs the person who started it and the agent")
	errCompactHandoffActive = &ConflictError{Code: "handoff_active",
		Message: "A hand-off of this conversation is still running; compact the conversation once it has finished (or cancel it)."}
)

// CreateHandoff stores a queued hand-off run, the hand-off and its empty result message, all at once. It runs under
// the conversation's row lock and the same budget lock as turns, so the caps and budgets hold when several start at
// once. The run's budget is the smallest of the hand-off budget and what is left of the two daily budgets.
func (s *Store) CreateHandoff(ctx context.Context, in NewHandoff) (StartedHandoff, error) {
	var out StartedHandoff
	// Fail closed: without them the per-person cap and budget could not count it
	if in.Meta.CreatedBy == "" || in.Meta.ChatAgentID == "" {
		return out, errHandoffWithoutOwner
	}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id FROM conversations WHERE id = $1 FOR UPDATE`, in.ConversationID).Scan(new(string)); err != nil {
			return mapErr(err, "conversation", "")
		}
		budget, err := handoffBudget(ctx, tx, in)
		if err != nil {
			return err
		}
		spec, err := withHandoffIdentity(ctx, tx, in)
		if err != nil {
			return err
		}
		spec.BudgetUSD = budget
		meta := in.Meta
		meta.Kind = model.RunKindHandoff
		if out.Run, err = insertRun(ctx, tx, spec, meta, nil, nil); err != nil {
			return err
		}
		if err := insertHandoff(ctx, tx, in, *spec.Chat.Handoff, out.Run.ID, budget); err != nil {
			return err
		}
		if out.Message, err = insertResultMessage(ctx, tx, in.ConversationID, out.Run.ID, spec.Chat.Handoff.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at = now() WHERE id = $1`, in.ConversationID); err != nil {
			return fmt.Errorf("touch conversation: %w", err)
		}
		out.Handoff, err = scanHandoff(tx.QueryRow(ctx, `SELECT `+handoffColumns+handoffFrom+` WHERE h.id = $1`, spec.Chat.Handoff.ID))
		if err != nil {
			return fmt.Errorf("read new hand-off: %w", err)
		}
		return nil
	})
	return out, err
}

// handoffBudget checks the caps on hand-offs running at once and returns what the new one may spend. It takes the
// same budget lock as a turn (turnBudget), which also serialises the person's hand-offs with the agent. A hand-off
// never takes all that is left of either daily budget: MinBudgetUSD stays free so the person can keep chatting while it
// works, so it needs twice that left to start.
func handoffBudget(ctx context.Context, tx pgx.Tx, in NewHandoff) (float64, error) {
	remaining, err := turnBudget(ctx, tx, ChatTurn{ConversationID: in.ConversationID, Spec: in.Spec, Meta: in.Meta,
		DailyBudgetUSD: in.DailyBudgetUSD, PersonBudgetUSD: in.PersonBudgetUSD, MinBudgetUSD: 2 * in.MinBudgetUSD})
	if err != nil {
		return 0, err
	}
	var inConversation, ofPerson int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE h.conversation_id = $1), count(*) FILTER (WHERE r.chat_agent_id = $2 AND r.created_by = $3)
		 FROM chat_handoffs h JOIN runs r ON r.id = h.run_id WHERE r.status IN ('queued', 'running')`,
		in.ConversationID, in.Meta.ChatAgentID, in.Meta.CreatedBy).Scan(&inConversation, &ofPerson); err != nil {
		return 0, fmt.Errorf("count running hand-offs: %w", err)
	}
	switch {
	case inConversation >= in.MaxActive:
		return 0, errHandoffsBusy
	case ofPerson >= in.MaxPerson:
		return 0, errPersonHandoffsBusy
	}
	return min(in.HandoffBudgetUSD, remaining-in.MinBudgetUSD), nil
}

// withHandoffIdentity is the spec with its hand-off's new id and a first deadline (now plus its time limit) filled in;
// StartRun sets the deadline again from when the run starts.
func withHandoffIdentity(ctx context.Context, tx pgx.Tx, in NewHandoff) (model.RunSpec, error) {
	hand := in.Handoff
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text, now() + make_interval(mins => $1)`, hand.TimeoutMinutes).
		Scan(&hand.ID, &hand.DeadlineAt); err != nil {
		return model.RunSpec{}, fmt.Errorf("make hand-off id: %w", err)
	}
	spec := in.Spec
	chat := *spec.Chat
	chat.Handoff = &hand
	spec.Chat = &chat
	return spec, nil
}

func insertHandoff(ctx context.Context, tx pgx.Tx, in NewHandoff, hand model.ChatHandoffSpec, runID string, budget float64) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO chat_handoffs (id, conversation_id, request_message_id, run_id, worker_agent_id, worker_name, worker_role, title,
		 brief, budget_usd, timeout_minutes, deadline_at, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, left($7, 200), $8, $9, $10, $11, $12, $13)`,
		hand.ID, in.ConversationID, nullableUUID(hand.RequestMessageID), runID, nullableUUID(hand.WorkerAgentID), hand.WorkerName,
		hand.WorkerRole, hand.Title, hand.Brief, budget, hand.TimeoutMinutes, hand.DeadlineAt, nullableUUID(in.Meta.CreatedBy)); err != nil {
		return mapErr(err, "hand-off", hand.Title)
	}
	return nil
}

func insertResultMessage(ctx context.Context, tx pgx.Tx, conversationID, runID, handoffID string) (model.ChatMessage, error) {
	var id string
	if err := tx.QueryRow(ctx,
		`INSERT INTO conversation_messages (conversation_id, role, content, run_id, handoff_id) VALUES ($1, 'assistant', '', $2, $3)
		 RETURNING id`, conversationID, runID, handoffID).Scan(&id); err != nil {
		return model.ChatMessage{}, mapErr(err, "message", "")
	}
	m, err := scanMessage(tx.QueryRow(ctx, `SELECT `+messageColumns+messageFrom+` WHERE m.id = $1`, id))
	if err != nil {
		return m, fmt.Errorf("read result message: %w", err)
	}
	return m, nil
}

// GetHandoff returns one hand-off regardless of owner; callers check the conversation is the caller's.
func (s *Store) GetHandoff(ctx context.Context, id string) (model.Handoff, error) {
	h, err := scanHandoff(s.pool.QueryRow(ctx, `SELECT `+handoffColumns+handoffFrom+` WHERE h.id = $1`, id))
	return h, mapErr(err, "hand-off", "")
}

// ListHandoffs returns a conversation's latest limit hand-offs, newest first.
func (s *Store) ListHandoffs(ctx context.Context, conversationID string, limit int) ([]model.Handoff, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+handoffColumns+handoffFrom+` WHERE h.conversation_id = $1
		ORDER BY h.created_at DESC, h.id DESC LIMIT $2`, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("list hand-offs: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Handoff, error) { return scanHandoff(r) })
	if err != nil {
		return nil, fmt.Errorf("read hand-offs: %w", err)
	}
	return orEmpty(items), nil
}

// PendingHandoffs are a conversation's hand-offs still queued or running, oldest first, as a turn tells the lead.
func (s *Store) PendingHandoffs(ctx context.Context, conversationID string) ([]model.ChatHandoffRef, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT h.title, h.worker_name, h.worker_role, COALESCE(r.started_at, h.created_at), h.deadline_at
		 FROM chat_handoffs h JOIN runs r ON r.id = h.run_id
		 WHERE h.conversation_id = $1 AND r.status IN ('queued', 'running') ORDER BY h.created_at`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list pending hand-offs: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ChatHandoffRef, error) {
		var h model.ChatHandoffRef
		err := r.Scan(&h.Title, &h.WorkerName, &h.WorkerRole, &h.StartedAt, &h.DeadlineAt)
		return h, err
	})
	if err != nil {
		return nil, fmt.Errorf("read pending hand-offs: %w", err)
	}
	return orEmpty(items), nil
}

// checkNoActiveHandoff refuses while a hand-off of the conversation is queued or running.
func checkNoActiveHandoff(ctx context.Context, tx pgx.Tx, conversationID string) error {
	var active bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM chat_handoffs h JOIN runs r ON r.id = h.run_id
		 WHERE h.conversation_id = $1 AND r.status IN ('queued', 'running'))`, conversationID).Scan(&active); err != nil {
		return fmt.Errorf("check active hand-off: %w", err)
	}
	if active {
		return errHandoffActive
	}
	return nil
}

// startHandoffClock sets a hand-off's deadline from now, as its run starts (inside StartRun's transaction): its time
// limit counts from the start, so waiting for a free slot is free. The frozen spec gets the same deadline, which the
// runner and the run manager enforce. Other runs are left as they are.
func startHandoffClock(ctx context.Context, tx pgx.Tx, runID string) error {
	if _, err := tx.Exec(ctx,
		`UPDATE chat_handoffs SET deadline_at = now() + make_interval(mins => timeout_minutes) WHERE run_id = $1`, runID); err != nil {
		return fmt.Errorf("start hand-off clock: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE runs r SET spec = jsonb_set(r.spec, '{chat,handoff,deadline_at}', to_jsonb(h.deadline_at))
		 FROM chat_handoffs h WHERE h.run_id = r.id AND r.id = $1 AND r.spec->'chat'->'handoff' IS NOT NULL`, runID); err != nil {
		return fmt.Errorf("freeze hand-off deadline: %w", err)
	}
	return nil
}
