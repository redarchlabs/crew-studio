package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// MaxEventsPerRead caps one page of events.
const MaxEventsPerRead = 1000

// maxInteractionEvents bounds how many events feed one interactions graph.
const maxInteractionEvents = 200_000

func (s *Store) AppendEvent(ctx context.Context, runID string, event model.RunEvent) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("encode %s event: %w", event.Type, err)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, payload, created_at) VALUES ($1, $2, $3, $4, $5)`,
		runID, event.Seq, event.Type, payload, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("save %s event: %w", event.Type, err)
	}
	return nil
}

func (s *Store) AppendLLMCall(ctx context.Context, runID string, call model.LLMCall) error {
	messages, err := json.Marshal(call.Messages)
	if err != nil {
		return fmt.Errorf("encode llm call messages: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO llm_calls (run_id, seq, agent_role, task_name, model, prompt_tokens, completion_tokens,
		 cached_prompt_tokens, cache_write_tokens, cost_usd, messages, response, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		runID, call.Seq, call.AgentRole, call.TaskName, call.Model, call.Usage.PromptTokens, call.Usage.CompletionTokens,
		call.Usage.CachedPromptTokens, call.Usage.CacheWriteTokens, call.CostUSD, messages, call.Response, call.CreatedAt)
	if err != nil {
		return fmt.Errorf("save llm call: %w", err)
	}
	return nil
}

func scanEvents(rows pgx.Rows) ([]model.RunEvent, error) {
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.RunEvent, error) {
		var e model.RunEvent
		err := r.Scan(&e.Seq, &e.Type, &e.Payload, &e.CreatedAt)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	return orEmpty(items), nil
}

// ListEvents returns up to limit events with seq greater than after, in order.
func (s *Store) ListEvents(ctx context.Context, runID string, after, limit int) ([]model.RunEvent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT seq, type, payload, created_at FROM run_events WHERE run_id = $1 AND seq > $2 ORDER BY seq LIMIT $3`,
		runID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return scanEvents(rows)
}

// InteractionEvents returns the events that shape the interactions graph (everything except raw logs).
func (s *Store) InteractionEvents(ctx context.Context, runID string) ([]model.RunEvent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT seq, type, payload, created_at FROM run_events WHERE run_id = $1 AND type <> 'log' ORDER BY seq LIMIT $2`,
		runID, maxInteractionEvents)
	if err != nil {
		return nil, fmt.Errorf("list interaction events: %w", err)
	}
	return scanEvents(rows)
}

func (s *Store) MaxEventSeq(ctx context.Context, runID string) (int, error) {
	var seq int
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(max(seq), 0) FROM run_events WHERE run_id = $1`, runID).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("read last event seq: %w", err)
	}
	return seq, nil
}

func (s *Store) ListLLMCalls(ctx context.Context, runID string) ([]model.LLMCall, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT seq, agent_role, task_name, model, prompt_tokens, completion_tokens, cached_prompt_tokens, cache_write_tokens, cost_usd,
		 messages, response, created_at FROM llm_calls WHERE run_id = $1 ORDER BY seq`, runID)
	if err != nil {
		return nil, fmt.Errorf("list llm calls: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.LLMCall, error) {
		var c model.LLMCall
		err := r.Scan(&c.Seq, &c.AgentRole, &c.TaskName, &c.Model, &c.Usage.PromptTokens, &c.Usage.CompletionTokens,
			&c.Usage.CachedPromptTokens, &c.Usage.CacheWriteTokens, &c.CostUSD, &c.Messages, &c.Response, &c.CreatedAt)
		c.Usage.SuccessfulRequests = 1
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("read llm calls: %w", err)
	}
	return orEmpty(items), nil
}

// GroundedSearchesInPeriod counts billable grounded search queries across all runs since the start of the
// current day or month, so the provider's free allowance is shared between runs. Gemini 3 bills each query a
// prompt triggers; older events without a count are counted once.
func (s *Store) GroundedSearchesInPeriod(ctx context.Context, period string) (int, error) {
	if period != model.GroundingPeriodDay && period != model.GroundingPeriodMonth {
		return 0, fmt.Errorf("unknown grounding period %q", period)
	}
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(sum(COALESCE((payload->>'billable_searches')::int, 1)), 0)::int FROM run_events
		 WHERE type = 'search' AND created_at >= date_trunc($1, now()) AND (payload->>'grounded')::boolean`,
		period).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count grounded searches: %w", err)
	}
	return n, nil
}

// Overview counts what the scope can see. LLMs and prices are shared, so they are always counted.
func (s *Store) Overview(ctx context.Context, scope Scope) (model.Overview, error) {
	all, areas := scope.areaArgs()
	var o model.Overview
	err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM llms), (SELECT count(*) FROM model_prices),
		(SELECT count(*) FROM agents a WHERE `+areaFilter("agent_areas", "agent_id", "a.id")+`),
		(SELECT count(*) FROM tasks t WHERE `+areaFilter("task_areas", "task_id", "t.id")+`),
		(SELECT count(*) FROM crews c WHERE `+areaFilter("crew_areas", "crew_id", "c.id")+`),
		(SELECT count(*) FROM runs WHERE $1 OR area_ids && $2::uuid[]),
		(SELECT count(*) FROM work_orders WHERE $1 OR area_ids && $2::uuid[]),
		(SELECT count(*) FROM schedules sc WHERE $1 OR EXISTS (SELECT 1 FROM crew_areas x WHERE x.crew_id = sc.crew_id AND x.area_id = ANY($2::uuid[]))),
		(SELECT COALESCE(sum(cost_usd), 0) FROM runs WHERE created_at >= now() - interval '30 days' AND ($1 OR area_ids && $2::uuid[])),
		(SELECT COALESCE(sum(cost_usd), 0) FROM runs WHERE kind IN ('chat', 'handoff') AND created_at >= now() - interval '30 days'
		   AND ($1 OR area_ids && $2::uuid[]))`,
		all, areas).
		Scan(&o.Counts.LLMs, &o.Counts.Pricing, &o.Counts.Agents, &o.Counts.Tasks, &o.Counts.Crews, &o.Counts.Runs,
			&o.Counts.WorkOrders, &o.Counts.Schedules, &o.SpendLast30dUSD, &o.ChatSpendLast30dUSD)
	if err != nil {
		return o, fmt.Errorf("read overview counts: %w", err)
	}
	if o.RecentRuns, _, err = s.ListRuns(ctx, RunFilter{Scope: scope, Kind: model.RunKindCrew, Limit: 10}); err != nil {
		return o, err
	}
	o.RecentWorkOrders, _, err = s.ListWorkOrders(ctx, WorkOrderFilter{Scope: scope, Limit: 10})
	return o, err
}

func (s *Store) ListRevisions(ctx context.Context, f RevisionFilter) ([]model.Revision, int, error) {
	var entityType, entityID *string
	if f.EntityType != "" {
		entityType = &f.EntityType
	}
	if f.EntityID != "" {
		entityID = &f.EntityID
	}
	all, areas := f.Scope.areaArgs()
	// Items without areas (LLMs, prices, settings, admin records) are limited by type alone
	const where = `WHERE ($1::text IS NULL OR entity_type = $1) AND ($2::text IS NULL OR entity_id = $2)
		AND entity_type = ANY($3::text[])
		AND ($4::boolean OR cardinality(area_ids) = 0 OR area_ids && $5::uuid[])`
	args := []any{entityType, entityID, orEmpty(f.Types), all, areas}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM revisions `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count revisions: %w", err)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, entity_type, entity_id, action, snapshot, actor_id, actor_name, created_at FROM revisions `+where+
			` ORDER BY id DESC LIMIT $6 OFFSET $7`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list revisions: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Revision, error) {
		var rev model.Revision
		err := r.Scan(&rev.ID, &rev.EntityType, &rev.EntityID, &rev.Action, &rev.Snapshot, &rev.ActorID, &rev.ActorName, &rev.CreatedAt)
		return rev, err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("read revisions: %w", err)
	}
	return orEmpty(items), total, nil
}

// LLMResponses returns the start of each LLM call's reply, keyed by seq, for the journal.
func (s *Store) LLMResponses(ctx context.Context, runID string, maxChars int) (map[int]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT seq, left(response, $2) FROM llm_calls WHERE run_id = $1`, runID, maxChars)
	if err != nil {
		return nil, fmt.Errorf("list llm responses: %w", err)
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var seq int
		var text string
		if err := rows.Scan(&seq, &text); err != nil {
			return nil, fmt.Errorf("read llm responses: %w", err)
		}
		out[seq] = text
	}
	return out, rows.Err()
}
