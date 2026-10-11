package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

const priceColumns = `id, model, input_per_mtok, cached_input_per_mtok, cache_write_per_mtok, output_per_mtok, notes, created_at, updated_at`

func scanPrice(row pgx.Row) (model.ModelPrice, error) {
	var p model.ModelPrice
	err := row.Scan(&p.ID, &p.Model, &p.InputPerMTok, &p.CachedInputPerMTok, &p.CacheWritePerMTok, &p.OutputPerMTok, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (s *Store) ListPrices(ctx context.Context) ([]model.ModelPrice, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+priceColumns+` FROM model_prices ORDER BY model`)
	if err != nil {
		return nil, fmt.Errorf("list prices: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ModelPrice, error) { return scanPrice(r) })
	if err != nil {
		return nil, fmt.Errorf("read prices: %w", err)
	}
	return orEmpty(items), nil
}

func (s *Store) GetPrice(ctx context.Context, id string) (model.ModelPrice, error) {
	p, err := scanPrice(s.pool.QueryRow(ctx, `SELECT `+priceColumns+` FROM model_prices WHERE id = $1`, id))
	return p, mapErr(err, "price", "")
}

func (s *Store) CreatePrice(ctx context.Context, in model.ModelPrice) (model.ModelPrice, error) {
	var out model.ModelPrice
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanPrice(tx.QueryRow(ctx,
			`INSERT INTO model_prices (model, input_per_mtok, cached_input_per_mtok, cache_write_per_mtok, output_per_mtok, notes)
			 VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+priceColumns,
			in.Model, in.InputPerMTok, in.CachedInputPerMTok, in.CacheWritePerMTok, in.OutputPerMTok, in.Notes))
		if err != nil {
			return mapErr(err, "price for model", in.Model)
		}
		return recordRevision(ctx, tx, "pricing", out.ID, "create", out)
	})
	return out, err
}

func (s *Store) UpdatePrice(ctx context.Context, id string, in model.ModelPrice) (model.ModelPrice, error) {
	var out model.ModelPrice
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanPrice(tx.QueryRow(ctx,
			`UPDATE model_prices SET model = $2, input_per_mtok = $3, cached_input_per_mtok = $4, cache_write_per_mtok = $5,
			 output_per_mtok = $6, notes = $7, updated_at = now() WHERE id = $1 RETURNING `+priceColumns,
			id, in.Model, in.InputPerMTok, in.CachedInputPerMTok, in.CacheWritePerMTok, in.OutputPerMTok, in.Notes))
		if err != nil {
			return mapErr(err, "price for model", in.Model)
		}
		return recordRevision(ctx, tx, "pricing", out.ID, "update", out)
	})
	return out, err
}

func (s *Store) DeletePrice(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		old, err := scanPrice(tx.QueryRow(ctx, `DELETE FROM model_prices WHERE id = $1 RETURNING `+priceColumns, id))
		if err != nil {
			return mapErr(err, "price", "")
		}
		return recordRevision(ctx, tx, "pricing", id, "delete", old)
	})
}

const settingsColumns = `default_budget_usd, hard_stop_multiplier, search_model, grounded_search_cost_per_1000,
	grounded_searches_free, grounding_free_period, max_concurrent_runs, compact_context_tokens`

func scanSettings(row pgx.Row) (model.Settings, error) {
	var st model.Settings
	err := row.Scan(&st.DefaultBudgetUSD, &st.HardStopMultiplier, &st.SearchModel, &st.GroundedSearchCostPer1000,
		&st.GroundedSearchesFree, &st.GroundingFreePeriod, &st.MaxConcurrentRuns, &st.CompactContextTokens)
	return st, err
}

func (s *Store) GetSettings(ctx context.Context) (model.Settings, error) {
	st, err := scanSettings(s.pool.QueryRow(ctx, `SELECT `+settingsColumns+` FROM settings`))
	if err != nil {
		return st, fmt.Errorf("read settings: %w", err)
	}
	return st, nil
}

func (s *Store) UpdateSettings(ctx context.Context, in model.Settings) (model.Settings, error) {
	var out model.Settings
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanSettings(tx.QueryRow(ctx,
			`UPDATE settings SET default_budget_usd = $1, hard_stop_multiplier = $2, search_model = $3,
			 grounded_search_cost_per_1000 = $4, grounded_searches_free = $5, grounding_free_period = $6,
			 max_concurrent_runs = $7, compact_context_tokens = $8, updated_at = now() RETURNING `+settingsColumns,
			in.DefaultBudgetUSD, in.HardStopMultiplier, in.SearchModel, in.GroundedSearchCostPer1000,
			in.GroundedSearchesFree, in.GroundingFreePeriod, in.MaxConcurrentRuns, in.CompactContextTokens))
		if err != nil {
			return fmt.Errorf("update settings: %w", err)
		}
		return recordRevision(ctx, tx, "settings", "settings", "update", out)
	})
	return out, err
}
