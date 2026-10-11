package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

const escalationColumns = `e.crew_id, e.enabled, e.rules, e.timeout_minutes, e.on_timeout, e.max_questions, COALESCE(u.name, ''), e.updated_at`

func scanEscalation(row pgx.Row) (model.EscalationSettings, error) {
	var e model.EscalationSettings
	err := row.Scan(&e.CrewID, &e.Enabled, &e.Rules, &e.TimeoutMinutes, &e.OnTimeout, &e.MaxQuestions, &e.UpdatedByName, &e.UpdatedAt)
	return e, err
}

// EscalationSettings returns a crew's escalation settings; a crew nobody set up has escalation off.
func (s *Store) EscalationSettings(ctx context.Context, crewID string) (model.EscalationSettings, error) {
	e, err := scanEscalation(s.pool.QueryRow(ctx,
		`SELECT `+escalationColumns+` FROM crew_escalation e LEFT JOIN users u ON u.id = e.updated_by WHERE e.crew_id = $1`, crewID))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.DefaultEscalationSettings(crewID), nil
	}
	if err != nil {
		return e, fmt.Errorf("read escalation settings: %w", err)
	}
	return e, nil
}

// PutEscalationSettings saves a crew's escalation settings (already validated) as changed by userID.
func (s *Store) PutEscalationSettings(ctx context.Context, in model.EscalationSettings, userID string) (model.EscalationSettings, error) {
	var out model.EscalationSettings
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO crew_escalation (crew_id, enabled, rules, timeout_minutes, on_timeout, max_questions, updated_by, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, now())
			 ON CONFLICT (crew_id) DO UPDATE SET enabled = EXCLUDED.enabled, rules = EXCLUDED.rules,
			   timeout_minutes = EXCLUDED.timeout_minutes, on_timeout = EXCLUDED.on_timeout,
			   max_questions = EXCLUDED.max_questions, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			in.CrewID, in.Enabled, in.Rules, in.TimeoutMinutes, in.OnTimeout, in.MaxQuestions, nullableUUID(userID)); err != nil {
			return mapErr(err, "escalation setting", "")
		}
		var err error
		out, err = scanEscalation(tx.QueryRow(ctx,
			`SELECT `+escalationColumns+` FROM crew_escalation e LEFT JOIN users u ON u.id = e.updated_by WHERE e.crew_id = $1`, in.CrewID))
		if err != nil {
			return fmt.Errorf("read escalation settings: %w", err)
		}
		return nil
	})
	return out, err
}

// questionStatus is a question's effective status: an open question past its deadline has timed out, and one whose
// run has ended is closed. Stored rows only change when answered, so nothing has to sweep them.
const questionStatus = `CASE WHEN q.status <> 'open' THEN q.status
	WHEN r.status NOT IN ('queued', 'running') THEN 'closed'
	WHEN q.deadline_at <= now() THEN 'timed_out'
	ELSE 'open' END`

const questionColumns = `q.id, q.run_id, r.work_order_id, wo.number, COALESCE(wo.title, ''), r.crew_name, q.assignee_id,
	q.agent_role, q.asked_for, q.task_name, q.question, q.context, ` + questionStatus + `, q.answer, COALESCE(ua.name, ''),
	q.asked_at, q.deadline_at, q.answered_at, h.conversation_id, COALESCE(cv.title, ''), h.id, COALESCE(h.title, '')`

// A question from an agent chat's hand-off is found through its run (docs/agent-chat-handoffs-design.md, section 3)
const questionFrom = ` FROM human_questions q
	JOIN runs r ON r.id = q.run_id
	LEFT JOIN work_orders wo ON wo.id = r.work_order_id
	LEFT JOIN users ua ON ua.id = q.answered_by
	LEFT JOIN chat_handoffs h ON h.run_id = q.run_id
	LEFT JOIN conversations cv ON cv.id = h.conversation_id`

func scanQuestion(row pgx.Row) (model.HumanQuestion, error) {
	var q model.HumanQuestion
	err := row.Scan(&q.ID, &q.RunID, &q.WorkOrderID, &q.WorkOrderNumber, &q.WorkOrderTitle, &q.CrewName, &q.AssigneeID,
		&q.AgentRole, &q.AskedFor, &q.TaskName, &q.Question, &q.Context, &q.Status, &q.Answer, &q.AnsweredByName,
		&q.AskedAt, &q.DeadlineAt, &q.AnsweredAt, &q.ConversationID, &q.ConversationTitle, &q.HandoffID, &q.HandoffTitle)
	return q, err
}

func collectQuestions(rows pgx.Rows, err error) ([]model.HumanQuestion, error) {
	if err != nil {
		return nil, fmt.Errorf("list questions: %w", err)
	}
	defer rows.Close()
	out := []model.HumanQuestion{}
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, fmt.Errorf("read question: %w", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// NewQuestion is what an agent asks.
type NewQuestion struct {
	AgentRole string
	// AskedFor is the agent the question is asked for (a hand-off's lead asks for a specialist); "" for itself
	AskedFor string
	TaskName string
	Question string
	Context  string
}

// ErrQuestionLimit means the run has already asked as many questions as its crew allows.
var ErrQuestionLimit = errors.New("question limit reached")

// ErrQuestionNoTime means a hand-off has too little time left for anyone to answer before it must stop.
var ErrQuestionNoTime = errors.New("too little time left to ask")

// ErrQuestionNobody means a hand-off's question has nobody to go to: the conversation's owner is gone or disabled.
var ErrQuestionNobody = errors.New("nobody to ask")

// minQuestionWait is the least time a question from a hand-off must leave the person to answer
const minQuestionWait = time.Minute

// AskQuestion records a question from a run, addressed to the human who started it, due within timeout. It refuses
// with ErrQuestionLimit once the run has asked maxQuestions. A hand-off's question is due a minute before the
// hand-off's own deadline at the latest, so waiting on the person never outlives it (the deadline does not pause while
// it waits); with less than minQuestionWait left before then it refuses with ErrQuestionNoTime, and when its owner is no
// longer an active person (nobody could answer) with ErrQuestionNobody.
func (s *Store) AskQuestion(ctx context.Context, runID string, in NewQuestion, timeout time.Duration, maxQuestions int) (model.HumanQuestion, error) {
	var out model.HumanQuestion
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		// Lock the run so two agents asking at once cannot both slip under the limit
		var asked int
		var handoffLeft *float64
		var owner *string
		if err := tx.QueryRow(ctx,
			`SELECT (SELECT count(*) FROM human_questions WHERE run_id = r.id),
			   (SELECT extract(epoch FROM h.deadline_at - interval '1 minute' - now())::float8 FROM chat_handoffs h WHERE h.run_id = r.id),
			   (SELECT u.id::text FROM users u WHERE u.id = r.created_by AND u.kind = 'human' AND u.status = 'active')
			 FROM runs r WHERE r.id = $1 FOR UPDATE OF r`, runID).
			Scan(&asked, &handoffLeft, &owner); err != nil {
			return mapErr(err, "run", "")
		}
		if asked >= maxQuestions {
			return ErrQuestionLimit
		}
		if handoffLeft != nil && owner == nil {
			return ErrQuestionNobody
		}
		if handoffLeft != nil && *handoffLeft < minQuestionWait.Seconds() {
			return ErrQuestionNoTime
		}
		var id string
		if err := tx.QueryRow(ctx,
			`INSERT INTO human_questions (run_id, assignee_id, agent_role, asked_for, task_name, question, context, deadline_at)
			 SELECT r.id, u.id, $2, left($7, 200), $3, $4, $5,
			   LEAST(now() + make_interval(secs => $6), (SELECT h.deadline_at - interval '1 minute' FROM chat_handoffs h WHERE h.run_id = r.id))
			 FROM runs r LEFT JOIN users u ON u.id = r.created_by AND u.kind = 'human'
			 WHERE r.id = $1
			 RETURNING id`,
			runID, in.AgentRole, in.TaskName, in.Question, in.Context, timeout.Seconds(), in.AskedFor).Scan(&id); err != nil {
			return mapErr(err, "question", "")
		}
		var err error
		out, err = scanQuestion(tx.QueryRow(ctx, `SELECT `+questionColumns+questionFrom+` WHERE q.id = $1`, id))
		return err
	})
	return out, err
}

// RunQuestion returns one of a run's own questions.
func (s *Store) RunQuestion(ctx context.Context, runID, id string) (model.HumanQuestion, error) {
	q, err := scanQuestion(s.pool.QueryRow(ctx, `SELECT `+questionColumns+questionFrom+` WHERE q.id = $1 AND q.run_id = $2`, id, runID))
	return q, mapErr(err, "question", "")
}

// GetQuestion returns a question with the areas of its run, for access checks.
func (s *Store) GetQuestion(ctx context.Context, id string) (model.HumanQuestion, []string, error) {
	var areas []string
	q, err := scanQuestion(s.pool.QueryRow(ctx, `SELECT `+questionColumns+questionFrom+` WHERE q.id = $1`, id))
	if err == nil {
		err = s.pool.QueryRow(ctx, `SELECT area_ids::text[] FROM runs WHERE id = $1`, q.RunID).Scan(&areas)
	}
	return q, orEmpty(areas), mapErr(err, "question", "")
}

// AnswerQuestion stores the answer to a question that is still open (not timed out, run still going).
func (s *Store) AnswerQuestion(ctx context.Context, id, answer, userID string) (model.HumanQuestion, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE human_questions q SET status = 'answered', answer = $2, answered_by = $3, answered_at = now()
		 FROM runs r
		 WHERE q.id = $1 AND r.id = q.run_id AND q.status = 'open' AND q.deadline_at > now() AND r.status IN ('queued', 'running')`,
		id, answer, nullableUUID(userID))
	if err != nil {
		return model.HumanQuestion{}, fmt.Errorf("answer question: %w", err)
	}
	q, getErr := scanQuestion(s.pool.QueryRow(ctx, `SELECT `+questionColumns+questionFrom+` WHERE q.id = $1`, id))
	if getErr != nil {
		return q, mapErr(getErr, "question", "")
	}
	if tag.RowsAffected() == 0 {
		return q, &ConflictError{Code: "question_" + q.Status, Message: questionClosedMessage(q.Status)}
	}
	return q, nil
}

func questionClosedMessage(status string) string {
	switch status {
	case model.QuestionAnswered:
		return "This question has already been answered."
	case model.QuestionTimedOut:
		return "This question timed out; the agent has moved on."
	default:
		return "The run that asked this question has ended."
	}
}

// WorkOrderQuestions lists the questions asked by every attempt of a work order, newest first.
func (s *Store) WorkOrderQuestions(ctx context.Context, workOrderID string) ([]model.HumanQuestion, error) {
	return collectQuestions(s.pool.Query(ctx,
		`SELECT `+questionColumns+questionFrom+` WHERE r.work_order_id = $1 ORDER BY q.asked_at DESC LIMIT 200`, workOrderID))
}

// OpenQuestionsFor lists the open questions waiting on a person: those addressed to them, plus unaddressed ones
// (runs started by a service account, trigger or schedule) in their areas when they may cancel work orders. A hand-off's
// question is only ever its conversation owner's, so an unaddressed one is nobody's.
func (s *Store) OpenQuestionsFor(ctx context.Context, userID string, scope Scope, canCancel bool) ([]model.HumanQuestion, error) {
	all, areas := scope.areaArgs()
	return collectQuestions(s.pool.Query(ctx,
		`SELECT `+questionColumns+questionFrom+`
		 WHERE q.status = 'open' AND q.deadline_at > now() AND r.status IN ('queued', 'running')
		   AND (q.assignee_id = $1 OR (q.assignee_id IS NULL AND h.id IS NULL AND $2 AND ($3 OR r.area_ids && $4::uuid[])))
		 ORDER BY q.deadline_at LIMIT 100`, nullableUUID(userID), canCancel, all, areas))
}

// ConversationQuestions lists the questions asked by the hand-offs of a conversation: open ones first (soonest due
// first), then the others newest first.
func (s *Store) ConversationQuestions(ctx context.Context, conversationID string) ([]model.HumanQuestion, error) {
	return collectQuestions(s.pool.Query(ctx,
		`SELECT `+questionColumns+questionFrom+` WHERE h.conversation_id = $1
		 ORDER BY (`+questionStatus+`) = 'open' DESC,
		   CASE WHEN (`+questionStatus+`) = 'open' THEN q.deadline_at END, q.asked_at DESC LIMIT 200`, conversationID))
}

// OpenConversationQuestions lists the open questions of a conversation's hand-offs, soonest due first.
func (s *Store) OpenConversationQuestions(ctx context.Context, conversationID string) ([]model.HumanQuestion, error) {
	return collectQuestions(s.pool.Query(ctx,
		`SELECT `+questionColumns+questionFrom+` WHERE h.conversation_id = $1
		   AND q.status = 'open' AND q.deadline_at > now() AND r.status IN ('queued', 'running')
		 ORDER BY q.deadline_at LIMIT 100`, conversationID))
}
