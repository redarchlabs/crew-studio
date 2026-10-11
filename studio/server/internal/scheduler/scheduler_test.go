package scheduler

import (
	"testing"
	"time"
)

func TestNextTimes(t *testing.T) {
	from := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) // a Saturday
	times, err := NextTimes("0 9 * * 1-5", "America/New_York", from, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(times) != 3 || times[0].Weekday() != time.Monday || times[0].Hour() != 9 || times[0].Location().String() != "America/New_York" {
		t.Errorf("times = %v", times)
	}
	for _, bad := range []struct{ expr, tz string }{
		{"* * * * *", "UTC"}, {"*/2 * * * *", "UTC"}, {"0-4 9 * * *", "UTC"}, {"@every 10m", "UTC"},
		{"not cron", "UTC"}, {"0 9 * * *", "Nowhere/Zone"}, {"0 0 30 2 *", "UTC"},
	} {
		if _, err := NextTimes(bad.expr, bad.tz, from, 1); err == nil {
			t.Errorf("%q in %s should be refused", bad.expr, bad.tz)
		}
	}
	if _, err := NextTimes("@daily", "UTC", from, 1); err != nil {
		t.Errorf("@daily: %v", err)
	}
	next, err := Next("*/5 * * * *", "UTC", from)
	if err != nil || !next.Equal(from.Add(5*time.Minute)) {
		t.Errorf("next = %v %v", next, err)
	}
}
