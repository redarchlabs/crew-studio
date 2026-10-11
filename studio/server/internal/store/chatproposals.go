package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// A chat reply can carry one proposal: a change the front desk wants. It moves from pending to confirmed (by the
// person who owns the conversation, who then sees it applied), declined or expired. Only the chat run that wrote
// the reply can attach one.

var errAlreadyProposed = &ConflictError{Code: "proposal_exists",
	Message: "You already proposed a change in this reply. Tell the person about it; they can confirm or decline it."}

// ErrProposalDecided means the proposal is no longer pending.
var ErrProposalDecided = &ConflictError{Code: "proposal_decided", Message: "This proposal was already confirmed, declined or has expired."}

// MaxHandoffProposals caps the cards one hand-off may propose (docs/agent-chat-handoffs-design.md, section 10).
const MaxHandoffProposals = 5

var errTooManyProposals = &ConflictError{Code: "too_many_proposals", Message: fmt.Sprintf("This hand-off has already proposed %d "+
	"changes, the most one hand-off may. Put anything else in your result instead.", MaxHandoffProposals)}

// AttachProposal puts a proposal on the reply that the chat run runID is writing. A hand-off writes no reply while it
// works, so its proposals are messages of their own instead (see addHandoffProposal).
func (s *Store) AttachProposal(ctx context.Context, runID string, p model.Proposal) (model.ChatMessage, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return model.ChatMessage{}, fmt.Errorf("encode proposal: %w", err)
	}
	var kind string
	err = s.pool.QueryRow(ctx, `SELECT kind FROM runs WHERE id = $1`, runID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ChatMessage{}, ErrNotFound
	}
	if err != nil {
		return model.ChatMessage{}, fmt.Errorf("read run kind: %w", err)
	}
	if kind == model.RunKindHandoff {
		return s.addHandoffProposal(ctx, runID, p.Summary, data)
	}
	var id string
	// Only while the run is still running: a finished or cancelled turn's token may linger a little
	err = s.pool.QueryRow(ctx,
		`UPDATE conversation_messages SET proposal = $2, proposal_state = 'pending', proposed_at = now()
		 WHERE run_id = $1 AND role = 'assistant' AND proposal IS NULL
		   AND EXISTS (SELECT 1 FROM runs r WHERE r.id = $1 AND r.status = 'running') RETURNING id`, runID, data).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ChatMessage{}, s.whyNoProposal(ctx, runID)
	}
	if err != nil {
		return model.ChatMessage{}, fmt.Errorf("attach proposal: %w", err)
	}
	m, _, err := s.GetMessage(ctx, id)
	return m, err
}

// addHandoffProposal adds a hand-off's proposal as a new assistant message of the hand-off (handoff_id set, no run of
// its own; its text is the card's summary), while the hand-off's run is running and it has proposed fewer than
// MaxHandoffProposals cards. The hand-off's row is locked, so parallel tool calls cannot pass the cap together.
func (s *Store) addHandoffProposal(ctx context.Context, runID, summary string, data []byte) (model.ChatMessage, error) {
	var id string
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var handoffID, conversationID string
		var running bool
		err := tx.QueryRow(ctx,
			`SELECT h.id, h.conversation_id, r.status = 'running' FROM chat_handoffs h JOIN runs r ON r.id = h.run_id
			 WHERE h.run_id = $1 FOR UPDATE OF h`, runID).Scan(&handoffID, &conversationID, &running)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("find hand-off: %w", err)
		}
		if !running {
			return &ConflictError{Code: "turn_finished", Message: "This hand-off has finished, so it can no longer propose changes."}
		}
		var proposed int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM conversation_messages WHERE handoff_id = $1 AND run_id IS NULL AND proposal IS NOT NULL`,
			handoffID).Scan(&proposed); err != nil {
			return fmt.Errorf("count hand-off proposals: %w", err)
		}
		if proposed >= MaxHandoffProposals {
			return errTooManyProposals
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO conversation_messages (conversation_id, role, content, handoff_id, proposal, proposal_state, proposed_at)
			 VALUES ($1, 'assistant', $2, $3, $4, 'pending', now()) RETURNING id`,
			conversationID, summary, handoffID, data).Scan(&id); err != nil {
			return mapErr(err, "message", "")
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at = now() WHERE id = $1`, conversationID); err != nil {
			return fmt.Errorf("touch conversation: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.ChatMessage{}, err
	}
	m, _, err := s.GetMessage(ctx, id)
	return m, err
}

// whyNoProposal explains why a proposal could not be attached to the run's reply.
func (s *Store) whyNoProposal(ctx context.Context, runID string) error {
	var hasReply, proposed bool
	err := s.pool.QueryRow(ctx,
		`SELECT m.id IS NOT NULL, COALESCE(m.proposal IS NOT NULL, false)
		 FROM runs r LEFT JOIN conversation_messages m ON m.run_id = r.id WHERE r.id = $1`, runID).Scan(&hasReply, &proposed)
	switch {
	case errors.Is(err, pgx.ErrNoRows) || (err == nil && !hasReply):
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("find chat reply: %w", err)
	case proposed:
		return errAlreadyProposed
	default:
		return &ConflictError{Code: "turn_finished", Message: "This chat turn has finished, so it can no longer propose changes."}
	}
}

// ClaimProposal moves a pending proposal to confirmed for the person deciding, so it is applied once. A failed
// apply calls ReleaseProposal to put it back.
func (s *Store) ClaimProposal(ctx context.Context, messageID, userID, userName string) error {
	return s.decide(ctx, messageID, model.ProposalConfirmed, userID, userName)
}

// DeclineProposal records that the person declined a pending proposal.
func (s *Store) DeclineProposal(ctx context.Context, messageID, userID, userName string) error {
	return s.decide(ctx, messageID, model.ProposalDeclined, userID, userName)
}

// ExpireProposal marks a pending proposal expired.
func (s *Store) ExpireProposal(ctx context.Context, messageID string) error {
	return s.decide(ctx, messageID, model.ProposalExpired, "", "")
}

func (s *Store) decide(ctx context.Context, messageID, state, userID, userName string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE conversation_messages SET proposal_state = $2, decided_by = $3, decided_by_name = $4, decided_at = now()
		 WHERE id = $1 AND proposal_state = 'pending'`, messageID, state, nullableUUID(userID), userName)
	if err != nil {
		return fmt.Errorf("decide proposal: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProposalDecided
	}
	return nil
}

// ReleaseProposal returns a claimed proposal whose apply failed to pending.
func (s *Store) ReleaseProposal(ctx context.Context, messageID string) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE conversation_messages SET proposal_state = 'pending', decided_by = NULL, decided_by_name = '', decided_at = NULL
		 WHERE id = $1 AND proposal_state = 'confirmed' AND proposal_result IS NULL`, messageID); err != nil {
		return fmt.Errorf("release proposal: %w", err)
	}
	return nil
}

// SetProposalResult records what applying a confirmed proposal did.
func (s *Store) SetProposalResult(ctx context.Context, messageID string, result map[string]any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode proposal result: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE conversation_messages SET proposal_result = $2 WHERE id = $1`, messageID, data); err != nil {
		return fmt.Errorf("save proposal result: %w", err)
	}
	return nil
}

// UnmarkUndone takes back MarkUndone when the undo itself failed.
func (s *Store) UnmarkUndone(ctx context.Context, messageID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE conversation_messages SET undone_at = NULL WHERE id = $1`, messageID); err != nil {
		return fmt.Errorf("unmark proposal undone: %w", err)
	}
	return nil
}

// MarkUndone claims the undo of a confirmed proposal (so it runs once); it fails when it already was undone.
func (s *Store) MarkUndone(ctx context.Context, messageID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE conversation_messages SET undone_at = now() WHERE id = $1 AND proposal_state = 'confirmed' AND undone_at IS NULL`, messageID)
	if err != nil {
		return fmt.Errorf("mark proposal undone: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return &ConflictError{Code: "already_undone", Message: "This change was already undone."}
	}
	return nil
}

// UndoNoteWrite reverses a note write that made version: the note goes back to the version before it (saved as a
// new version by by), or is deleted when version created it. It is refused when the note changed since.
func (s *Store) UndoNoteWrite(ctx context.Context, key NoteKey, version int, by NoteWriter) (*model.NoteVersion, error) {
	var out *model.NoteVersion
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || '/' || COALESCE($2::text, '') || '/' || $3 || '/' || $4, 0))`,
			key.CrewID, key.ScheduleID, key.Subject, key.Name); err != nil {
			return fmt.Errorf("lock note: %w", err)
		}
		var current int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version), 0) FROM notes WHERE `+noteKeyWhere,
			key.CrewID, key.ScheduleID, key.Subject, key.Name).Scan(&current); err != nil {
			return fmt.Errorf("read note version: %w", err)
		}
		if current != version {
			return &ConflictError{Code: "version_conflict", Message: fmt.Sprintf(
				"The note %q changed since (it is at version %d, not %d), so this cannot be undone here; use the crew's State tab.",
				key.Name, current, version)}
		}
		if version == 1 {
			_, err := tx.Exec(ctx, `DELETE FROM notes WHERE `+noteKeyWhere, key.CrewID, key.ScheduleID, key.Subject, key.Name)
			return mapErr(err, "note", key.Name)
		}
		var content string
		if err := tx.QueryRow(ctx, `SELECT content FROM notes WHERE `+noteKeyWhere+` AND version = $5`,
			key.CrewID, key.ScheduleID, key.Subject, key.Name, version-1).Scan(&content); err != nil {
			return mapErr(err, "note", key.Name)
		}
		n, err := scanNoteVersion(tx.QueryRow(ctx,
			`INSERT INTO notes (crew_id, schedule_id, subject, name, version, content, run_id, written_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+noteVersionColumns,
			key.CrewID, key.ScheduleID, key.Subject, key.Name, current+1, content, by.RunID, by.Name))
		if err != nil {
			return mapErr(err, "note", key.Name)
		}
		out = &n
		return nil
	})
	return out, err
}
