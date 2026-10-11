package runner

import (
	"context"
	"fmt"
	"time"

	"crewstudio/internal/model"
)

// runTokenGrace is how long a run's token keeps working after the run ends, so a state call in flight completes.
const runTokenGrace = 2 * time.Minute

// StateEventType is the run event the run-state API records for every call a state tool makes.
const StateEventType = "state"

// stateAPI mints the run's token for the run-state API and returns what the runner reads from its request.
// It returns nil when the API is off (no StateURL). The token lasts at most the run's time limit plus the grace
// period; finishing the run brings that forward.
func (m *Manager) stateAPI(ctx context.Context, runID string) (map[string]string, error) {
	if m.proc.StateURL == "" {
		return nil, nil
	}
	token, err := m.store.CreateRunToken(ctx, runID, time.Now().Add(m.proc.MaxDuration+runTokenGrace))
	if err != nil {
		return nil, fmt.Errorf("Could not create the run's state token: %v", err)
	}
	return map[string]string{"url": m.proc.StateURL + "/api/run-state", "token": token}, nil
}

// expireRunToken ends the run's token after grace. Failures are logged: the token still expires on its own.
func (m *Manager) expireRunToken(runID string, grace time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), dbWriteTimeout)
	defer cancel()
	if err := m.store.ExpireRunToken(ctx, runID, time.Now().Add(grace)); err != nil {
		m.log.Warn("could not expire the run's state token", "run_id", runID, "err", err)
	}
}

// RecordStateEvent adds a "state" event (such as "Notes: read weekly-report v12") to a run's events, and so to
// its journal and live stream. While the run executes it goes through the run's own recorder so sequence
// numbers stay in order; afterwards (the token's grace period) it is appended after the last event.
func (m *Manager) RecordStateEvent(ctx context.Context, runID string, payload map[string]any) error {
	event := model.RunEvent{Type: StateEventType, CreatedAt: time.Now(), Payload: payload}
	m.mu.Lock()
	entry := m.active[runID]
	m.mu.Unlock()
	if entry != nil {
		if rec := entry.rec.Load(); rec != nil {
			rec.record(event, nil)
			return nil
		}
	}
	seq, err := m.store.AppendEventNext(ctx, runID, event)
	if err != nil {
		return err
	}
	event.Seq = seq
	m.broker.Publish(runID, event)
	return nil
}
