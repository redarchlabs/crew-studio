package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// Agent chat (docs/agent-chat-design.md): an agent's chat settings with the specialists it may ask and the crews it
// may start, conversations with an agent, and what a person spent chatting with one.

const agentChatColumns = `ac.agent_id, ac.enabled, ac.llm_id, ac.daily_budget_usd, ac.person_daily_budget_usd,
	ac.handoffs_enabled, ac.handoff_budget_usd, ac.handoff_timeout_minutes, ac.max_active_handoffs,
	ac.questions_enabled, ac.question_rules, ac.question_timeout_minutes, ac.question_on_timeout, ac.max_questions,
	ac.specialists_may_propose, COALESCE(u.name, ''), ac.updated_at,
	ARRAY(SELECT specialist_id::text FROM agent_chat_specialists s WHERE s.agent_id = ac.agent_id ORDER BY s.position),
	ARRAY(SELECT crew_id::text FROM agent_chat_crews k WHERE k.agent_id = ac.agent_id ORDER BY k.position),
	ARRAY(SELECT connection_id::text FROM agent_chat_connections x WHERE x.agent_id = ac.agent_id ORDER BY x.position)`

const agentChatFrom = ` FROM agent_chat ac LEFT JOIN users u ON u.id = ac.updated_by`

func scanAgentChat(row pgx.Row) (model.AgentChatSettings, error) {
	var c model.AgentChatSettings
	err := row.Scan(&c.AgentID, &c.Enabled, &c.LLMID, &c.DailyBudgetUSD, &c.PersonDailyBudgetUSD,
		&c.HandoffsEnabled, &c.HandoffBudgetUSD, &c.HandoffTimeoutMinutes, &c.MaxActiveHandoffs,
		&c.QuestionsEnabled, &c.QuestionRules, &c.QuestionTimeoutMinutes, &c.QuestionOnTimeout, &c.MaxQuestions,
		&c.SpecialistsMayPropose, &c.UpdatedByName, &c.UpdatedAt,
		&c.SpecialistIDs, &c.CrewIDs, &c.ConnectionIDs)
	c.SpecialistIDs, c.CrewIDs, c.ConnectionIDs = orEmpty(c.SpecialistIDs), orEmpty(c.CrewIDs), orEmpty(c.ConnectionIDs)
	return c, err
}

// AgentChatSettings returns an agent's chat settings; an agent nobody set up has chat off with the default budgets.
func (s *Store) AgentChatSettings(ctx context.Context, agentID string) (model.AgentChatSettings, error) {
	c, err := scanAgentChat(s.pool.QueryRow(ctx, `SELECT `+agentChatColumns+agentChatFrom+` WHERE ac.agent_id = $1`, agentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.DefaultAgentChatSettings(agentID), nil
	}
	if err != nil {
		return c, fmt.Errorf("read agent chat settings: %w", err)
	}
	return c, nil
}

// PutAgentChatSettings saves an agent's chat settings (already validated) as changed by userID. Specialists, crews
// and connections that do not exist are refused with a ConflictError (the foreign keys).
func (s *Store) PutAgentChatSettings(ctx context.Context, in model.AgentChatSettings, userID string) (model.AgentChatSettings, error) {
	var out model.AgentChatSettings
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO agent_chat (agent_id, enabled, llm_id, daily_budget_usd, person_daily_budget_usd, handoffs_enabled,
			   handoff_budget_usd, handoff_timeout_minutes, max_active_handoffs, questions_enabled, question_rules,
			   question_timeout_minutes, question_on_timeout, max_questions, specialists_may_propose, updated_by, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $16, $15, now())
			 ON CONFLICT (agent_id) DO UPDATE SET enabled = EXCLUDED.enabled, llm_id = EXCLUDED.llm_id,
			   daily_budget_usd = EXCLUDED.daily_budget_usd, person_daily_budget_usd = EXCLUDED.person_daily_budget_usd,
			   handoffs_enabled = EXCLUDED.handoffs_enabled, handoff_budget_usd = EXCLUDED.handoff_budget_usd,
			   handoff_timeout_minutes = EXCLUDED.handoff_timeout_minutes, max_active_handoffs = EXCLUDED.max_active_handoffs,
			   questions_enabled = EXCLUDED.questions_enabled, question_rules = EXCLUDED.question_rules,
			   question_timeout_minutes = EXCLUDED.question_timeout_minutes, question_on_timeout = EXCLUDED.question_on_timeout,
			   max_questions = EXCLUDED.max_questions, specialists_may_propose = EXCLUDED.specialists_may_propose, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			in.AgentID, in.Enabled, in.LLMID, in.DailyBudgetUSD, personBudget(in.PersonDailyBudgetUSD), in.HandoffsEnabled,
			orDefault(in.HandoffBudgetUSD, model.DefaultHandoffBudgetUSD), orDefault(in.HandoffTimeoutMinutes, model.DefaultHandoffTimeoutMinutes),
			orDefault(in.MaxActiveHandoffs, model.DefaultMaxActiveHandoffs), in.QuestionsEnabled, in.QuestionRules,
			orDefault(in.QuestionTimeoutMinutes, model.DefaultQuestionTimeoutMinutes), orDefault(in.QuestionOnTimeout, model.EscalationProceed),
			orDefault(in.MaxQuestions, model.DefaultChatMaxQuestions), nullableUUID(userID), in.SpecialistsMayPropose); err != nil {
			return mapErr(err, "chat setting", "")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM agent_chat_specialists WHERE agent_id = $1`, in.AgentID); err != nil {
			return fmt.Errorf("clear chat specialists: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO agent_chat_specialists (agent_id, specialist_id, position)
			 SELECT $1, id::uuid, pos FROM unnest($2::text[]) WITH ORDINALITY AS x(id, pos)`, in.AgentID, orEmpty(in.SpecialistIDs)); err != nil {
			return mapErr(err, "specialist", "")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM agent_chat_crews WHERE agent_id = $1`, in.AgentID); err != nil {
			return fmt.Errorf("clear chat crews: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO agent_chat_crews (agent_id, crew_id, position)
			 SELECT $1, id::uuid, pos FROM unnest($2::text[]) WITH ORDINALITY AS x(id, pos)`, in.AgentID, orEmpty(in.CrewIDs)); err != nil {
			return mapErr(err, "crew", "")
		}
		if err := replaceChatList(ctx, tx, "agent_chat_connections", "connection_id", in.AgentID, in.ConnectionIDs); err != nil {
			return mapErr(err, "connection", "")
		}
		var err error
		if out, err = scanAgentChat(tx.QueryRow(ctx, `SELECT `+agentChatColumns+agentChatFrom+` WHERE ac.agent_id = $1`, in.AgentID)); err != nil {
			return fmt.Errorf("read agent chat settings: %w", err)
		}
		return nil
	})
	return out, err
}

// replaceChatList makes ids, in order, an agent's list in table (one of the agent_chat_* lists, keyed by column).
func replaceChatList(ctx context.Context, tx pgx.Tx, table, column, agentID string, ids []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE agent_id = $1`, agentID); err != nil {
		return fmt.Errorf("clear %s: %w", table, err)
	}
	_, err := tx.Exec(ctx, `INSERT INTO `+table+` (agent_id, `+column+`, position)
		SELECT $1, id::uuid, pos FROM unnest($2::text[]) WITH ORDINALITY AS x(id, pos)`, agentID, orEmpty(ids))
	return err
}

// ChatAgents lists the agents in scope that people can chat with, each with the specialists and crews it may use
// that are also in scope.
func (s *Store) ChatAgents(ctx context.Context, scope Scope) ([]model.ChatAgent, error) {
	all, areas := scope.areaArgs()
	rows, err := s.pool.Query(ctx,
		`SELECT a.id, a.name, a.role, a.goal,
		   COALESCE((SELECT json_agg(json_build_object('id', sa.id, 'name', sa.name, 'role', sa.role) ORDER BY sp.position)
		     FROM agent_chat_specialists sp JOIN agents sa ON sa.id = sp.specialist_id
		     WHERE sp.agent_id = a.id AND `+areaFilter("agent_areas", "agent_id", "sa.id")+`), '[]'),
		   COALESCE((SELECT json_agg(json_build_object('id', c.id, 'name', c.name) ORDER BY k.position)
		     FROM agent_chat_crews k JOIN crews c ON c.id = k.crew_id
		     WHERE k.agent_id = a.id AND `+areaFilter("crew_areas", "crew_id", "c.id")+`), '[]')
		 FROM agent_chat ac JOIN agents a ON a.id = ac.agent_id
		 WHERE ac.enabled AND `+areaFilter("agent_areas", "agent_id", "a.id")+`
		 ORDER BY a.name`, all, areas)
	if err != nil {
		return nil, fmt.Errorf("list chat agents: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ChatAgent, error) {
		var a model.ChatAgent
		err := r.Scan(&a.ID, &a.Name, &a.Role, &a.Goal, &a.Specialists, &a.Crews)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("read chat agents: %w", err)
	}
	for i := range items {
		items[i].Specialists = distinctRoles(items[i].Role, items[i].Specialists)
	}
	return orEmpty(items), nil
}

// distinctRoles drops specialists whose role repeats the agent's or an earlier specialist's, as a chat turn does
// (CrewAI could not tell them apart), so the list shows only those a turn can ask.
func distinctRoles(leadRole string, specialists []model.ChatNameRef) []model.ChatNameRef {
	seen := []string{model.ChatRoleKey(leadRole)}
	out := make([]model.ChatNameRef, 0, len(specialists))
	for _, s := range specialists {
		key := model.ChatRoleKey(s.Role)
		if !slices.Contains(seen, key) {
			seen = append(seen, key)
			out = append(out, s)
		}
	}
	return out
}

// CreateAgentConversation starts an empty conversation of userID with an agent.
func (s *Store) CreateAgentConversation(ctx context.Context, agentID, userID, title string) (model.Conversation, error) {
	var id string
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO conversations (agent_id, user_id, title) VALUES ($1, $2, $3) RETURNING id`, agentID, userID, title).Scan(&id); err != nil {
		return model.Conversation{}, mapErr(err, "conversation", "")
	}
	return s.GetConversation(ctx, id)
}

// PersonAgentChatSpent is what one person's chat turns and hand-offs with an agent cost in the last 24 hours, in every
// conversation (deleted ones included: it reads the runs). A turn still queued or running counts at its whole budget,
// so turns started at once in several conversations cannot each spend the person's whole cap.
func (s *Store) PersonAgentChatSpent(ctx context.Context, agentID, userID string) (float64, error) {
	return personAgentChatSpent(ctx, s.pool, agentID, userID)
}

func personAgentChatSpent(ctx context.Context, q queryer, agentID, userID string) (float64, error) {
	var spent float64
	err := q.QueryRow(ctx,
		`SELECT COALESCE(sum(COALESCE(cost_usd, CASE WHEN status IN ('queued', 'running') THEN budget_usd ELSE 0 END)), 0) FROM runs
		 WHERE kind IN ('chat', 'handoff') AND chat_agent_id = $1 AND created_by = $2 AND created_at >= $3`,
		agentID, userID, time.Now().Add(-chatBudgetWindow)).Scan(&spent)
	if err != nil {
		return 0, fmt.Errorf("sum agent chat spend: %w", err)
	}
	return spent, nil
}

// orDefault is v, or def when v is left out (zero), for settings saved without going through their normalizer.
func orDefault[T int | float64 | string](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}
