package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// Memory settings follow the chat settings pattern: one row per crew, no revisions, changed by a person.

const memorySettingsColumns = `cm.crew_id, cm.mode, cm.llm_id, cm.recall_depth, cm.recall_limit, cm.saves_per_run, cm.retention_days,
	cm.review_run_saves, cm.recency_weight, cm.semantic_weight, cm.importance_weight, cm.recency_half_life_days,
	COALESCE(u.name, ''), cm.updated_at`

func scanMemorySettings(row pgx.Row) (model.MemorySettings, error) {
	var m model.MemorySettings
	var rec, sem, imp float32
	err := row.Scan(&m.CrewID, &m.Mode, &m.LLMID, &m.RecallDepth, &m.RecallLimit, &m.SavesPerRun, &m.RetentionDays,
		&m.ReviewRunSaves, &rec, &sem, &imp, &m.RecencyHalfLifeDays, &m.UpdatedByName, &m.UpdatedAt)
	m.RecencyWeight, m.SemanticWeight, m.ImportanceWeight = round3(rec), round3(sem), round3(imp)
	return m, err
}

// round3 undoes real's float32 noise (0.3 reads back as 0.30000001192...).
func round3(v float32) float64 {
	return float64(int64(float64(v)*1000+0.5)) / 1000
}

// MemorySettings returns a crew's memory settings; a crew nobody set up has memory off with the defaults.
func (s *Store) MemorySettings(ctx context.Context, crewID string) (model.MemorySettings, error) {
	m, err := scanMemorySettings(s.pool.QueryRow(ctx,
		`SELECT `+memorySettingsColumns+` FROM crew_memory cm LEFT JOIN users u ON u.id = cm.updated_by WHERE cm.crew_id = $1`, crewID))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.DefaultMemorySettings(crewID), nil
	}
	if err != nil {
		return m, fmt.Errorf("read memory settings: %w", err)
	}
	return m, nil
}

// PutMemorySettings saves a crew's memory settings (already validated) as changed by userID.
func (s *Store) PutMemorySettings(ctx context.Context, in model.MemorySettings, userID string) (model.MemorySettings, error) {
	var out model.MemorySettings
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO crew_memory (crew_id, mode, llm_id, recall_depth, recall_limit, saves_per_run, retention_days, review_run_saves,
			   recency_weight, semantic_weight, importance_weight, recency_half_life_days, updated_by, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, now())
			 ON CONFLICT (crew_id) DO UPDATE SET mode = EXCLUDED.mode, llm_id = EXCLUDED.llm_id, recall_depth = EXCLUDED.recall_depth,
			   recall_limit = EXCLUDED.recall_limit, saves_per_run = EXCLUDED.saves_per_run, retention_days = EXCLUDED.retention_days,
			   review_run_saves = EXCLUDED.review_run_saves, recency_weight = EXCLUDED.recency_weight,
			   semantic_weight = EXCLUDED.semantic_weight, importance_weight = EXCLUDED.importance_weight,
			   recency_half_life_days = EXCLUDED.recency_half_life_days, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			in.CrewID, in.Mode, in.LLMID, in.RecallDepth, in.RecallLimit, in.SavesPerRun, in.RetentionDays, in.ReviewRunSaves,
			in.RecencyWeight, in.SemanticWeight, in.ImportanceWeight, in.RecencyHalfLifeDays, nullableUUID(userID)); err != nil {
			return mapErr(err, "memory setting", "")
		}
		var err error
		out, err = scanMemorySettings(tx.QueryRow(ctx,
			`SELECT `+memorySettingsColumns+` FROM crew_memory cm LEFT JOIN users u ON u.id = cm.updated_by WHERE cm.crew_id = $1`, in.CrewID))
		if err != nil {
			return fmt.Errorf("read memory settings: %w", err)
		}
		return nil
	})
	return out, err
}
