package runner

import (
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
)

// A hand-off's run is stopped two minutes after its own deadline (the runner stops it first), never later than the
// server's maximum; other runs get the maximum.
func TestRunLimit(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	handoff := func(deadline time.Time) model.RunSpec {
		return model.RunSpec{Chat: &model.ChatSpec{Handoff: &model.ChatHandoffSpec{DeadlineAt: deadline, TimeoutMinutes: 20}}}
	}
	cases := []struct {
		name    string
		spec    model.RunSpec
		want    time.Duration
		message string
	}{
		{"crew run", model.RunSpec{}, time.Hour, "The run took longer than 1h0m0s"},
		{"chat turn", model.RunSpec{Chat: &model.ChatSpec{}}, time.Hour, "The run took longer than 1h0m0s"},
		{"hand-off", handoff(now.Add(20 * time.Minute)), 22 * time.Minute, "The hand-off ran past its 20-minute limit"},
		{"hand-off past its deadline already", handoff(now.Add(-5 * time.Minute)), handoffGrace, "The hand-off ran past its 20-minute limit"},
		{"hand-off longer than the maximum", handoff(now.Add(3 * time.Hour)), time.Hour, "The run took longer than 1h0m0s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, message := runLimit(tc.spec, time.Hour, now)
			if got != tc.want || !strings.HasPrefix(message, tc.message) {
				t.Errorf("runLimit = %v, %q; want %v, %q", got, message, tc.want, tc.message)
			}
		})
	}
}
