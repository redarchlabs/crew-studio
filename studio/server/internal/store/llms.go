package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

const llmColumns = `id, name, model, temperature, max_completion_tokens, reasoning_effort, api, vision, created_at, updated_at`

func scanLLM(row pgx.Row) (model.LLM, error) {
	var l model.LLM
	err := row.Scan(&l.ID, &l.Name, &l.Model, &l.Temperature, &l.MaxCompletionTokens, &l.ReasoningEffort, &l.API, &l.Vision, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

func (s *Store) ListLLMs(ctx context.Context) ([]model.LLM, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+llmColumns+` FROM llms ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list llms: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.LLM, error) { return scanLLM(r) })
	if err != nil {
		return nil, fmt.Errorf("read llms: %w", err)
	}
	return orEmpty(items), nil
}

func (s *Store) GetLLM(ctx context.Context, id string) (model.LLM, error) {
	l, err := scanLLM(s.pool.QueryRow(ctx, `SELECT `+llmColumns+` FROM llms WHERE id = $1`, id))
	return l, mapErr(err, "LLM", "")
}

func (s *Store) CreateLLM(ctx context.Context, in model.LLM) (model.LLM, error) {
	var out model.LLM
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanLLM(tx.QueryRow(ctx,
			`INSERT INTO llms (name, model, temperature, max_completion_tokens, reasoning_effort, api, vision)
			 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+llmColumns,
			in.Name, in.Model, in.Temperature, in.MaxCompletionTokens, in.ReasoningEffort, apiOrDefault(in.API), in.Vision))
		if err != nil {
			return mapErr(err, "LLM", in.Name)
		}
		return recordRevision(ctx, tx, "llm", out.ID, "create", out)
	})
	return out, err
}

func (s *Store) UpdateLLM(ctx context.Context, id string, in model.LLM) (model.LLM, error) {
	var out model.LLM
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanLLM(tx.QueryRow(ctx,
			`UPDATE llms SET name = $2, model = $3, temperature = $4, max_completion_tokens = $5, reasoning_effort = $6,
			 api = $7, vision = $8, updated_at = now() WHERE id = $1 RETURNING `+llmColumns,
			id, in.Name, in.Model, in.Temperature, in.MaxCompletionTokens, in.ReasoningEffort, apiOrDefault(in.API), in.Vision))
		if err != nil {
			return mapErr(err, "LLM", in.Name)
		}
		return recordRevision(ctx, tx, "llm", out.ID, "update", out)
	})
	return out, err
}

func apiOrDefault(api string) string {
	if api == "" {
		return model.APICompletions
	}
	return api
}

func (s *Store) DeleteLLM(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		users, err := usedBy(ctx, tx, `
			SELECT 'agent "' || name || '"' FROM agents WHERE llm_id = $1
			UNION ALL
			SELECT 'crew "' || name || '" (manager)' FROM crews WHERE manager_llm_id = $1
			UNION ALL
			SELECT 'crew "' || c.name || '" (chat)' FROM crew_chat cc JOIN crews c ON c.id = cc.crew_id WHERE cc.llm_id = $1
			UNION ALL
			SELECT 'agent "' || a.name || '" (chat)' FROM agent_chat ac JOIN agents a ON a.id = ac.agent_id WHERE ac.llm_id = $1
			UNION ALL
			SELECT 'crew "' || c.name || '" (memory)' FROM crew_memory cm JOIN crews c ON c.id = cm.crew_id WHERE cm.llm_id = $1`, id)
		if err != nil {
			return err
		}
		if len(users) > 0 {
			return inUseError("LLM", users)
		}
		old, err := scanLLM(tx.QueryRow(ctx, `DELETE FROM llms WHERE id = $1 RETURNING `+llmColumns, id))
		if err != nil {
			return mapErr(err, "LLM", "")
		}
		return recordRevision(ctx, tx, "llm", id, "delete", old)
	})
}
