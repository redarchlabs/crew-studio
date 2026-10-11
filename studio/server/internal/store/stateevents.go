package store

import (
	"context"
	"encoding/json"
	"fmt"

	"crewstudio/internal/model"
)

// appendNextAttempts bounds retries when two writers pick the same next sequence number.
const appendNextAttempts = 5

// AppendEventNext stores an event after the run's last one, choosing the sequence number in the database. It is
// for runs that are no longer executing (their recorder owns the numbering while they are). event.Seq is ignored;
// the sequence number used is returned.
func (s *Store) AppendEventNext(ctx context.Context, runID string, event model.RunEvent) (int, error) {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return 0, fmt.Errorf("encode %s event: %w", event.Type, err)
	}
	var seq int
	for attempt := 1; ; attempt++ {
		err = s.pool.QueryRow(ctx,
			`INSERT INTO run_events (run_id, seq, type, payload, created_at)
			 SELECT $1, COALESCE(max(seq), 0) + 1, $2, $3, $4 FROM run_events WHERE run_id = $1 RETURNING seq`,
			runID, event.Type, payload, event.CreatedAt).Scan(&seq)
		if err == nil || !isUniqueViolation(err) || attempt == appendNextAttempts {
			break
		}
	}
	if err != nil {
		return 0, mapErr(err, "run event", "")
	}
	return seq, nil
}
