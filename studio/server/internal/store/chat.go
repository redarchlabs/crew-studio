package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// Crew chat: conversations (one person, one crew) and their messages. An assistant reply is stored when its turn
// starts, linked to the chat run that writes it, and filled in from the run's output once the run succeeds.

const (
	// maxReplyChars caps a reply copied from a run's output
	maxReplyChars = 20_000
	// maxRunErrorChars keeps the reason a reply failed short; the run page has the rest
	maxRunErrorChars = 300
	// chatBudgetWindow is the period a conversation's daily budget covers
	chatBudgetWindow = 24 * time.Hour
)

const conversationColumns = `cv.id, cv.crew_id, COALESCE(c.name, ''), cv.agent_id, COALESCE(ag.name, ''), cv.user_id,
	cv.work_order_id, cv.title, cv.created_at, cv.updated_at`

// conversationFrom joins a conversation's crew or (in agent chat) its agent
const conversationFrom = ` FROM conversations cv LEFT JOIN crews c ON c.id = cv.crew_id LEFT JOIN agents ag ON ag.id = cv.agent_id`

func scanConversation(row pgx.Row) (model.Conversation, error) {
	var c model.Conversation
	err := row.Scan(&c.ID, &c.CrewID, &c.CrewName, &c.AgentID, &c.AgentName, &c.UserID, &c.WorkOrderID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// A reply whose run succeeded after the last SettleReplies already shows the run's output, so a reader never sees a
// finished turn without its text
const messageColumns = `m.id, m.conversation_id, m.role,
	CASE WHEN m.content = '' AND r.status = 'succeeded' THEN left(COALESCE(r.output, ''), 20000) ELSE m.content END, m.proposal, m.proposal_state, m.proposed_at, m.proposal_result,
	m.decided_by_name, m.decided_at, m.undone_at, m.run_id, COALESCE(r.kind, ''), r.status, COALESCE(r.error, ''), r.cost_usd, m.created_at,
	h.id, COALESCE(h.title, ''), COALESCE(h.worker_name, ''), COALESCE(h.worker_role, ''), h.deadline_at, COALESCE(h.budget_usd, 0),
	COALESCE(h.timeout_minutes, 0), r.started_at, ` + handoffOpenQuestion + `, ` + handoffProposal

// handoffProposal says a message of a hand-off is one of the cards it proposed (slice 3d), not its result: the result
// is the hand-off's first message, made when it started
const handoffProposal = `(m.handoff_id IS NOT NULL AND EXISTS (SELECT 1 FROM conversation_messages e
	WHERE e.handoff_id = m.handoff_id AND e.seq < m.seq))`

// A hand-off's result message (docs/agent-chat-handoffs-design.md, section 5) carries what was handed off
const messageFrom = ` FROM conversation_messages m LEFT JOIN runs r ON r.id = m.run_id LEFT JOIN chat_handoffs h ON h.id = m.handoff_id`

func scanMessage(row pgx.Row) (model.ChatMessage, error) {
	var m model.ChatMessage
	var handoffID *string
	var hand model.HandoffRef
	var deadline *time.Time
	err := row.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.Proposal, &m.ProposalState, &m.ProposedAt, &m.ProposalResult,
		&m.DecidedByName, &m.DecidedAt, &m.UndoneAt, &m.RunID, &m.RunKind, &m.RunStatus, &m.RunError, &m.CostUSD, &m.CreatedAt,
		&handoffID, &hand.Title, &hand.WorkerName, &hand.WorkerRole, &deadline, &hand.BudgetUSD, &hand.TimeoutMinutes, &hand.StartedAt, &hand.OpenQuestionID,
		&m.HandoffProposal)
	m.RunError = shortError(m.RunError)
	m.Attachments = []model.ChatAttachment{}
	if handoffID != nil && deadline != nil {
		hand.ID, hand.DeadlineAt = *handoffID, *deadline
		m.Handoff = &hand
	}
	return m, err
}

// withAttachments fills in the files sent with each message.
func withAttachments(ctx context.Context, q queryer, msgs []model.ChatMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	ids := make([]string, len(msgs))
	at := make(map[string]int, len(msgs))
	for i, m := range msgs {
		ids[i], at[m.ID] = m.ID, i
	}
	rows, err := q.Query(ctx,
		`SELECT message_id, id, filename, content_type, size_bytes FROM attachments
		 WHERE message_id = ANY($1::uuid[]) ORDER BY created_at, filename`, ids)
	if err != nil {
		return fmt.Errorf("list chat files: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var messageID string
		var a model.ChatAttachment
		if err := rows.Scan(&messageID, &a.ID, &a.Filename, &a.ContentType, &a.SizeBytes); err != nil {
			return fmt.Errorf("read chat files: %w", err)
		}
		if i, ok := at[messageID]; ok {
			msgs[i].Attachments = append(msgs[i].Attachments, a)
		}
	}
	return rows.Err()
}

// shortError is the first line of a run's error, cut to maxRunErrorChars.
func shortError(msg string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(msg), "\n")
	if r := []rune(line); len(r) > maxRunErrorChars {
		line = string(r[:maxRunErrorChars]) + "…"
	}
	return line
}

// CreateConversation starts an empty conversation of userID with a crew.
func (s *Store) CreateConversation(ctx context.Context, crewID, userID, title string) (model.Conversation, error) {
	var id string
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO conversations (crew_id, user_id, title) VALUES ($1, $2, $3) RETURNING id`, crewID, userID, title).Scan(&id); err != nil {
		return model.Conversation{}, mapErr(err, "conversation", "")
	}
	return s.GetConversation(ctx, id)
}

// GetConversation returns one conversation regardless of owner; callers check it is the caller's.
func (s *Store) GetConversation(ctx context.Context, id string) (model.Conversation, error) {
	c, err := scanConversation(s.pool.QueryRow(ctx,
		`SELECT `+conversationColumns+conversationFrom+` WHERE cv.id = $1`, id))
	return c, mapErr(err, "conversation", "")
}

// ConversationFilter picks one person's conversations, optionally with one crew or one agent, among crews and
// agents in scope.
type ConversationFilter struct {
	UserID  string
	CrewID  string
	AgentID string
	Scope   Scope
	Limit   int
}

// ListConversations returns a person's conversations with crews and agents they can still see, most recently used
// first. Conversations about a work order live on the work order, not in this list.
func (s *Store) ListConversations(ctx context.Context, f ConversationFilter) ([]model.Conversation, error) {
	all, areas := f.Scope.areaArgs()
	rows, err := s.pool.Query(ctx,
		`SELECT `+conversationColumns+conversationFrom+`
		 WHERE ((c.id IS NOT NULL AND `+areaFilter("crew_areas", "crew_id", "c.id")+`)
		     OR (ag.id IS NOT NULL AND `+areaFilter("agent_areas", "agent_id", "ag.id")+`))
		   AND cv.user_id = $3 AND ($4::uuid IS NULL OR cv.crew_id = $4) AND ($6::uuid IS NULL OR cv.agent_id = $6)
		   AND cv.work_order_id IS NULL
		 ORDER BY cv.updated_at DESC, cv.id DESC LIMIT $5`, all, areas, f.UserID, nullableUUID(f.CrewID), f.Limit, nullableUUID(f.AgentID))
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Conversation, error) { return scanConversation(r) })
	if err != nil {
		return nil, fmt.Errorf("read conversations: %w", err)
	}
	return orEmpty(items), nil
}

// DeleteConversation removes a conversation, its messages and its hand-offs. Its chat and hand-off runs are kept (and
// still count towards the person's chat budget). It is refused while a turn is being answered or a hand-off runs.
func (s *Store) DeleteConversation(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id FROM conversations WHERE id = $1 FOR UPDATE`, id).Scan(&id); err != nil {
			return mapErr(err, "conversation", "")
		}
		if err := checkNoActiveTurn(ctx, tx, id); err != nil {
			return err
		}
		if err := checkNoActiveHandoff(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM conversations WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete conversation: %w", err)
		}
		return nil
	})
}

// SettleReplies copies the output of every finished, successful chat run into its still-empty reply, and expires
// proposals that waited longer than proposalTTL. Both only touch rows that still need it.
func (s *Store) SettleReplies(ctx context.Context, conversationID string, proposalTTL time.Duration) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE conversation_messages SET proposal_state = 'expired'
		 WHERE conversation_id = $1 AND proposal_state = 'pending' AND proposed_at < $2`,
		conversationID, time.Now().Add(-proposalTTL)); err != nil {
		return fmt.Errorf("expire chat proposals: %w", err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE conversation_messages m SET content = left(r.output, $2)
		 FROM runs r
		 WHERE m.conversation_id = $1 AND m.role IN ('assistant', 'summary') AND m.content = '' AND r.id = m.run_id
		   AND r.status = 'succeeded' AND COALESCE(r.output, '') <> ''`, conversationID, maxReplyChars); err != nil {
		return fmt.Errorf("settle chat replies: %w", err)
	}
	return nil
}

// ListMessages returns a conversation's latest limit messages, oldest first.
func (s *Store) ListMessages(ctx context.Context, conversationID string, limit int) ([]model.ChatMessage, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+messageColumns+messageFrom+` WHERE m.id IN (SELECT id FROM conversation_messages
		   WHERE conversation_id = $1 ORDER BY seq DESC LIMIT $2) ORDER BY m.seq`, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ChatMessage, error) { return scanMessage(r) })
	if err != nil {
		return nil, fmt.Errorf("read chat messages: %w", err)
	}
	if err := withAttachments(ctx, s.pool, items); err != nil {
		return nil, err
	}
	return orEmpty(items), nil
}

// GetMessage returns one message with its conversation (whose owner callers check).
func (s *Store) GetMessage(ctx context.Context, id string) (model.ChatMessage, model.Conversation, error) {
	m, err := scanMessage(s.pool.QueryRow(ctx, `SELECT `+messageColumns+messageFrom+` WHERE m.id = $1`, id))
	if err != nil {
		return m, model.Conversation{}, mapErr(err, "message", "")
	}
	if err := withAttachments(ctx, s.pool, []model.ChatMessage{m}); err != nil {
		return m, model.Conversation{}, err
	}
	c, err := s.GetConversation(ctx, m.ConversationID)
	return m, c, err
}

// RecentTurns returns the text of a conversation's latest messages for the front desk, oldest first: the latest
// summary (when the conversation was compacted) and up to limit messages after it. Replies that never arrived are
// left out. A hand-off's result, and each card it proposed, is labelled with the hand-off's title and worker.
func (s *Store) RecentTurns(ctx context.Context, conversationID string, limit int) ([]model.ChatTurn, error) {
	rows, err := s.pool.Query(ctx,
		`WITH summary AS (
		   SELECT seq FROM conversation_messages
		   WHERE conversation_id = $1 AND role = 'summary' AND content <> '' ORDER BY seq DESC LIMIT 1)
		 SELECT latest.role, latest.content, latest.created_at, COALESCE(h.title, ''), COALESCE(h.worker_name, ''),
		   h.id IS NOT NULL, EXISTS (SELECT 1 FROM conversation_messages e WHERE e.handoff_id = latest.handoff_id AND e.seq < latest.seq)
		 FROM (
		   (SELECT m.role, m.content, m.created_at, m.seq, m.handoff_id, m.run_id FROM conversation_messages m JOIN summary ON m.seq = summary.seq)
		   UNION ALL
		   (SELECT role, content, created_at, seq, handoff_id, run_id FROM conversation_messages
		    WHERE conversation_id = $1 AND content <> '' AND role <> 'summary'
		      AND seq > COALESCE((SELECT seq FROM summary), 0)
		    ORDER BY seq DESC LIMIT $2)) latest
		 LEFT JOIN chat_handoffs h ON h.id = latest.handoff_id
		 ORDER BY latest.seq`, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("list chat turns: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ChatTurn, error) {
		var t model.ChatTurn
		var title, worker string
		var ofHandoff, proposal bool
		err := r.Scan(&t.Role, &t.Content, &t.At, &title, &worker, &ofHandoff, &proposal)
		switch {
		case ofHandoff && proposal:
			t.Kind, t.Title, t.Worker = model.ChatTurnHandoffProposal, title, worker
		case ofHandoff:
			t.Kind, t.Title, t.Worker = model.ChatTurnHandoffResult, title, worker
		}
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("read chat turns: %w", err)
	}
	return orEmpty(items), nil
}

// ChatSpent is what a conversation's turns cost in the last 24 hours.
func (s *Store) ChatSpent(ctx context.Context, conversationID string) (float64, error) {
	return chatSpent(ctx, s.pool, conversationID)
}

// PersonChatSpent is what one person's chat turns with a crew cost in the last 24 hours, in every conversation
// (deleted ones included: it reads the runs). A turn still queued or running counts at its whole budget, so turns
// started at once in several conversations cannot each spend the person's whole cap.
func (s *Store) PersonChatSpent(ctx context.Context, crewID, userID string) (float64, error) {
	return personChatSpent(ctx, s.pool, crewID, userID)
}

func personChatSpent(ctx context.Context, q queryer, crewID, userID string) (float64, error) {
	var spent float64
	err := q.QueryRow(ctx,
		`SELECT COALESCE(sum(COALESCE(cost_usd, CASE WHEN status IN ('queued', 'running') THEN budget_usd ELSE 0 END)), 0) FROM runs
		 WHERE kind = 'chat' AND crew_id = $1 AND created_by = $2 AND created_at >= $3`,
		crewID, userID, time.Now().Add(-chatBudgetWindow)).Scan(&spent)
	if err != nil {
		return 0, fmt.Errorf("sum chat spend: %w", err)
	}
	return spent, nil
}

func chatSpent(ctx context.Context, q queryer, conversationID string) (float64, error) {
	var spent float64
	err := q.QueryRow(ctx,
		`SELECT COALESCE(sum(COALESCE(r.cost_usd, CASE WHEN r.status IN ('queued', 'running') THEN r.budget_usd ELSE 0 END)), 0)
		 FROM conversation_messages m JOIN runs r ON r.id = m.run_id
		 WHERE m.conversation_id = $1 AND r.created_at >= $2`, conversationID, time.Now().Add(-chatBudgetWindow)).Scan(&spent)
	if err != nil {
		return 0, fmt.Errorf("sum chat spend: %w", err)
	}
	return spent, nil
}

// ChatTurn is a turn to start: the person's message and the frozen run that answers it. A compaction has no
// message (Content is empty) and its reply is a summary (Compact).
type ChatTurn struct {
	ConversationID string
	Content        string
	Compact        bool
	// HistoryThrough is, for a compaction, the conversation's latest message (LatestMessageSeq) when its history was
	// read; a message added since would be left out of the summary, so the compaction is refused
	HistoryThrough int64
	// Files were sent with the message and are already in object storage (ID and StorageKey set); they are recorded
	// with the message
	Files []model.Attachment
	Spec  model.RunSpec
	Meta  RunMeta
	// DailyBudgetUSD is what the conversation may spend in 24 hours, PersonBudgetUSD what the person (Meta.CreatedBy)
	// may spend on the crew's chat (or, with Meta.ChatAgentID, the agent's) in 24 hours; the run gets what is left of
	// the smaller
	DailyBudgetUSD  float64
	PersonBudgetUSD float64
	// MinBudgetUSD is the least a turn may start with
	MinBudgetUSD float64
}

// StartedTurn is what CreateChatTurn stored.
type StartedTurn struct {
	// Title is the conversation's title after the turn (the first message names it)
	Title string
	// User is the person's message; empty (no ID) for a compaction
	User      model.ChatMessage
	Assistant model.ChatMessage
	Run       model.Run
}

var (
	errTurnActive = &ConflictError{Code: "chat_turn_active", Message: "The crew is still answering your last message; wait for its reply."}
	errBudgetUsed = &ConflictError{Code: "chat_budget_used",
		Message: "This conversation has used its daily chat budget. Try again later, or ask someone who can edit the crew to raise it."}
	errPersonBudgetUsed = &ConflictError{Code: "chat_person_budget_used",
		Message: "You have used your daily chat budget for this crew across your conversations. Try again later."}
)

// CreateChatTurn stores the person's message, a queued chat run and the empty reply it will fill, all at once.
// One turn runs at a time per conversation, and the run's budget is what is left of the daily budget.
func (s *Store) CreateChatTurn(ctx context.Context, t ChatTurn) (StartedTurn, error) {
	var out StartedTurn
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		// Serialises turns of one conversation, so two messages cannot both pass the checks below
		var title string
		if err := tx.QueryRow(ctx, `SELECT title FROM conversations WHERE id = $1 FOR UPDATE`, t.ConversationID).Scan(&title); err != nil {
			return mapErr(err, "conversation", "")
		}
		if err := checkNoActiveTurn(ctx, tx, t.ConversationID); err != nil {
			return err
		}
		if t.Compact {
			if err := checkUnchanged(ctx, tx, t.ConversationID, t.HistoryThrough); err != nil {
				return err
			}
			// A hand-off's result fills a message from before the summary, which later turns would never see
			if err := checkNoActiveHandoff(ctx, tx, t.ConversationID); errors.Is(err, errHandoffActive) {
				return errCompactHandoffActive
			} else if err != nil {
				return err
			}
		}
		remaining, err := turnBudget(ctx, tx, t)
		if err != nil {
			return err
		}
		spec := t.Spec
		spec.BudgetUSD = min(spec.BudgetUSD, remaining)
		meta := t.Meta
		meta.Kind = model.RunKindChat
		reply := model.ChatRoleAssistant
		if t.Compact {
			reply = model.ChatRoleSummary
		} else if out.User, err = insertMessage(ctx, tx, t.ConversationID, model.ChatRoleUser, t.Content, nil); err != nil {
			return err
		} else if out.User.Attachments, err = insertChatFiles(ctx, tx, out.User.ID, t.Files); err != nil {
			return err
		}
		if out.Run, err = insertRun(ctx, tx, spec, meta, nil, nil); err != nil {
			return err
		}
		if out.Assistant, err = insertMessage(ctx, tx, t.ConversationID, reply, "", &out.Run.ID); err != nil {
			return err
		}
		if title == "" && t.Content != "" {
			title = model.ChatTitle(t.Content)
		}
		out.Title = title
		if _, err := tx.Exec(ctx, `UPDATE conversations SET title = $2, updated_at = now() WHERE id = $1`, t.ConversationID, title); err != nil {
			return fmt.Errorf("touch conversation: %w", err)
		}
		return nil
	})
	return out, err
}

// turnBudget is what a turn may spend: what is left of the conversation's and of the person's daily budget.
// Turns of one person with one crew (or one agent) take turns here, so parallel conversations cannot both spend the
// last of it.
func turnBudget(ctx context.Context, tx pgx.Tx, t ChatTurn) (float64, error) {
	lockKey := "chat/" + t.Spec.Crew.ID + "/" + t.Meta.CreatedBy
	if t.Meta.ChatAgentID != "" {
		lockKey = "agent-chat/" + t.Meta.ChatAgentID + "/" + t.Meta.CreatedBy
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return 0, fmt.Errorf("lock chat budget: %w", err)
	}
	spent, err := chatSpent(ctx, tx, t.ConversationID)
	if err != nil {
		return 0, err
	}
	if t.DailyBudgetUSD-spent < t.MinBudgetUSD {
		return 0, errBudgetUsed
	}
	var personSpent float64
	if t.Meta.ChatAgentID != "" {
		personSpent, err = personAgentChatSpent(ctx, tx, t.Meta.ChatAgentID, t.Meta.CreatedBy)
	} else {
		personSpent, err = personChatSpent(ctx, tx, t.Spec.Crew.ID, t.Meta.CreatedBy)
	}
	if err != nil {
		return 0, err
	}
	if t.PersonBudgetUSD-personSpent < t.MinBudgetUSD {
		return 0, errPersonBudgetUsed
	}
	return min(t.DailyBudgetUSD-spent, t.PersonBudgetUSD-personSpent), nil
}

// checkNoActiveTurn refuses while a turn of the conversation (a reply or a compaction) is queued or running. A running
// hand-off does not count: the person may keep chatting while it works.
func checkNoActiveTurn(ctx context.Context, tx pgx.Tx, conversationID string) error {
	var active bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM conversation_messages m JOIN runs r ON r.id = m.run_id
		 WHERE m.conversation_id = $1 AND r.kind = 'chat' AND r.status IN ('queued', 'running'))`, conversationID).Scan(&active); err != nil {
		return fmt.Errorf("check active chat turn: %w", err)
	}
	if active {
		return errTurnActive
	}
	return nil
}

func insertMessage(ctx context.Context, tx pgx.Tx, conversationID, role, content string, runID *string) (model.ChatMessage, error) {
	var id string
	if err := tx.QueryRow(ctx,
		`INSERT INTO conversation_messages (conversation_id, role, content, run_id) VALUES ($1, $2, $3, $4)
		 RETURNING id`, conversationID, role, content, runID).Scan(&id); err != nil {
		return model.ChatMessage{}, mapErr(err, "message", "")
	}
	m, err := scanMessage(tx.QueryRow(ctx, `SELECT `+messageColumns+messageFrom+` WHERE m.id = $1`, id))
	if err != nil {
		return m, fmt.Errorf("read new message: %w", err)
	}
	return m, nil
}

var errConversationChanged = &ConflictError{Code: "conversation_changed",
	Message: "The conversation changed while it was being compacted; compact it again."}

// LatestMessageSeq is the position of the conversation's latest message (0 when it has none).
func (s *Store) LatestMessageSeq(ctx context.Context, conversationID string) (int64, error) {
	return latestMessageSeq(ctx, s.pool, conversationID)
}

func latestMessageSeq(ctx context.Context, q queryer, conversationID string) (int64, error) {
	var seq int64
	if err := q.QueryRow(ctx, `SELECT COALESCE(max(seq), 0) FROM conversation_messages WHERE conversation_id = $1`,
		conversationID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("read latest chat message: %w", err)
	}
	return seq, nil
}

func checkUnchanged(ctx context.Context, tx pgx.Tx, conversationID string, through int64) error {
	seq, err := latestMessageSeq(ctx, tx, conversationID)
	if err != nil {
		return err
	}
	if seq != through {
		return errConversationChanged
	}
	return nil
}

const (
	// A conversation holds at most this many files sent with its messages, of at most this size in all
	maxConversationFiles      = 40
	maxConversationFilesBytes = 200 << 20
)

var errConversationFilesFull = &ConflictError{Code: "chat_files_full", Message: fmt.Sprintf(
	"This conversation already holds the most files it may (%d files, %d MB). Start a new conversation to send more.",
	maxConversationFiles, maxConversationFilesBytes>>20)}

// insertChatFiles records the files sent with a message, within the conversation's file limits. The caller holds
// the conversation's row lock, so two messages cannot both pass the limits.
func insertChatFiles(ctx context.Context, tx pgx.Tx, messageID string, files []model.Attachment) ([]model.ChatAttachment, error) {
	if len(files) > 0 {
		var count int
		var total int64
		if err := tx.QueryRow(ctx,
			`SELECT count(*), COALESCE(sum(a.size_bytes), 0) FROM attachments a
			 JOIN conversation_messages m ON m.id = a.message_id
			 WHERE m.conversation_id = (SELECT conversation_id FROM conversation_messages WHERE id = $1)`, messageID).Scan(&count, &total); err != nil {
			return nil, fmt.Errorf("count chat files: %w", err)
		}
		for _, f := range files {
			count, total = count+1, total+f.SizeBytes
		}
		if count > maxConversationFiles || total > maxConversationFilesBytes {
			return nil, errConversationFilesFull
		}
	}
	actor := actorFrom(ctx)
	out := make([]model.ChatAttachment, 0, len(files))
	for _, f := range files {
		if _, err := tx.Exec(ctx,
			`INSERT INTO attachments (id, message_id, kind, filename, content_type, size_bytes, sha256, storage_key,
			 created_by, created_by_name) VALUES ($1, $2, 'chat', $3, $4, $5, $6, $7, $8, $9)`,
			f.ID, messageID, f.Filename, f.ContentType, f.SizeBytes, f.SHA256, f.StorageKey, nullableUUID(actor.ID), actor.Name); err != nil {
			return nil, mapErr(err, "attachment", f.Filename)
		}
		out = append(out, model.ChatAttachment{ID: f.ID, Filename: f.Filename, ContentType: f.ContentType, SizeBytes: f.SizeBytes})
	}
	return out, nil
}

// IsTurnActive reports whether err means a turn of the conversation is still running.
func IsTurnActive(err error) bool { return errors.Is(err, errTurnActive) }
