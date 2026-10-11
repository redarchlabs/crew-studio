package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Compacting a running attempt's context: a person asks, the count of requests goes up, and the runner (which reads
// the count through the run-state API) compacts each agent's context once on its next LLM call.

var errNotRunning = &ConflictError{Code: "run_not_running", Message: "Only a running attempt can have its context compacted."}

// RequestCompaction counts one more request to compact a running run's context and returns the new count.
func (s *Store) RequestCompaction(ctx context.Context, runID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`UPDATE runs SET compact_requests = compact_requests + 1 WHERE id = $1 AND status = 'running'
		 RETURNING compact_requests`, runID).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gerr := s.GetRun(ctx, runID); gerr != nil {
			return 0, gerr
		}
		return 0, errNotRunning
	}
	if err != nil {
		return 0, fmt.Errorf("request compaction: %w", err)
	}
	return n, nil
}

// CompactRequests is how many times people asked to compact the run's context.
func (s *Store) CompactRequests(ctx context.Context, runID string) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT compact_requests FROM runs WHERE id = $1`, runID).Scan(&n); err != nil {
		return 0, mapErr(err, "run", "")
	}
	return n, nil
}
