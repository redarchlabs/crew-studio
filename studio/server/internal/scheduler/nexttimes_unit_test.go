package scheduler

import (
	"strings"
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestNextTimesAcceptsAndRejects(t *testing.T) {
	from := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		expr    string
		tz      string
		wantErr string
	}{
		{"every 5 minutes is the floor", "*/5 * * * *", "UTC", ""},
		{"every 4 minutes too fast", "*/4 * * * *", "UTC", "more often"},
		{"every minute", "* * * * *", "UTC", "more often"},
		{"uneven gaps: 2 minute gap hides in the pattern", "0,58 * * * *", "UTC", "more often"},
		{"hourly descriptor", "@hourly", "UTC", ""},
		{"daily descriptor", "@daily", "UTC", ""},
		{"midnight descriptor", "@midnight", "UTC", ""},
		{"weekly descriptor", "@weekly", "UTC", ""},
		{"monthly descriptor", "@monthly", "UTC", ""},
		{"yearly descriptor", "@yearly", "UTC", ""},
		{"annually descriptor", "@annually", "UTC", ""},
		{"@every refused", "@every 1h", "UTC", "not @every"},
		{"@every with leading space refused", "  @every 1h", "UTC", "not @every"},
		{"six fields (seconds) refused", "0 0 9 * * *", "UTC", "cron:"},
		{"four fields refused", "0 9 * *", "UTC", "cron:"},
		{"empty expression", "", "UTC", "cron:"},
		{"out of range minute", "60 * * * *", "UTC", "cron:"},
		{"out of range hour", "0 24 * * *", "UTC", "cron:"},
		{"garbage", "every day please", "UTC", "cron:"},
		{"never fires: Feb 31", "0 0 31 2 *", "UTC", "never fires"},
		{"unknown zone", "0 9 * * *", "Mars/Olympus", "timezone:"},
		{"empty zone means UTC", "0 9 * * *", "", ""},
		{"name list", "0 9 * * MON-FRI", "UTC", ""},
		{"month names", "0 9 1 JAN,JUL *", "UTC", ""},
		{"step range", "10-50/20 * * * *", "UTC", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			times, err := NextTimes(tt.expr, tt.tz, from, 2)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(times) != 2 {
					t.Errorf("times = %v", times)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want mention of %q", err, tt.wantErr)
			}
		})
	}
}

func TestNextTimesCountAndOrder(t *testing.T) {
	from := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for _, n := range []int{1, 2, 10, 60, 100} {
		times, err := NextTimes("*/10 * * * *", "UTC", from, n)
		if err != nil {
			t.Fatal(err)
		}
		if len(times) != n {
			t.Errorf("n=%d got %d", n, len(times))
		}
		for i := 1; i < len(times); i++ {
			if times[i].Sub(times[i-1]) != 10*time.Minute {
				t.Fatalf("n=%d gap %v at %d", n, times[i].Sub(times[i-1]), i)
			}
		}
	}
	if times, err := NextTimes("*/10 * * * *", "UTC", from, 0); err != nil || len(times) != 0 {
		t.Errorf("n=0 -> %v %v", times, err)
	}
	// exactly "from" is not included (strictly after)
	times, _ := NextTimes("0 12 * * *", "UTC", from, 1)
	if !times[0].After(from) || times[0].Day() != 4 {
		t.Errorf("first = %v", times[0])
	}
}

func TestNextTimesTimeZones(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	// Before spring-forward (2026-03-08 02:00 local): 09:00 NY is 14:00Z (EST); after, 13:00Z (EDT).
	from := time.Date(2026, 3, 6, 0, 0, 0, 0, time.UTC)
	times, err := NextTimes("0 9 * * *", "America/New_York", from, 5)
	if err != nil {
		t.Fatal(err)
	}
	wantUTC := []int{14, 14, 13, 13, 13} // Mar 6, 7 (EST), then Mar 8, 9, 10 (EDT)
	for i, tm := range times {
		if tm.Hour() != 9 || tm.Location().String() != "America/New_York" {
			t.Errorf("time %d = %v, want 09:00 local", i, tm)
		}
		if tm.UTC().Hour() != wantUTC[i] {
			t.Errorf("time %d = %v (UTC hour %d, want %d)", i, tm.UTC(), tm.UTC().Hour(), wantUTC[i])
		}
	}
	// the gap across the change is 23 hours
	if d := times[2].Sub(times[1]); d != 23*time.Hour {
		t.Errorf("spring-forward day gap = %v, want 23h", d)
	}

	// Fall back (2026-11-01): the day has 25 hours
	from = time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	times, err = NextTimes("0 9 * * *", "America/New_York", from, 4)
	if err != nil {
		t.Fatal(err)
	}
	if d := times[2].Sub(times[1]); d != 25*time.Hour {
		t.Errorf("fall-back day gap = %v, want 25h", d)
	}

	// A time that does not exist locally (02:30 on spring-forward day) must still produce a strictly increasing, valid series
	from = time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC)
	times, err = NextTimes("30 2 * * *", "America/New_York", from, 5)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(times); i++ {
		if !times[i].After(times[i-1]) {
			t.Errorf("not increasing at %d: %v then %v", i, times[i-1], times[i])
		}
	}
	// An ambiguous time (01:30 happens twice on fall-back day) must not repeat
	from = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	times, err = NextTimes("30 1 * * *", "America/New_York", from, 5)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[time.Time]bool{}
	for _, tm := range times {
		if seen[tm.UTC()] {
			t.Errorf("duplicate fire time %v", tm)
		}
		seen[tm.UTC()] = true
	}
	_ = ny

	// Zones with half-hour offsets and no DST
	from = time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	times, err = NextTimes("0 9 * * *", "Asia/Kolkata", from, 1)
	if err != nil || times[0].UTC().Hour() != 3 || times[0].UTC().Minute() != 30 {
		t.Errorf("kolkata = %v %v", times, err)
	}
	// The same expression in different zones fires at different instants
	utc, _ := NextTimes("0 9 * * *", "UTC", from, 1)
	tokyo, _ := NextTimes("0 9 * * *", "Asia/Tokyo", from, 1)
	if utc[0].Equal(tokyo[0]) {
		t.Error("time zone ignored")
	}
	// `from` given in another zone is the same instant
	fromNY := time.Date(2026, 10, 2, 20, 0, 0, 0, ny) // = 2026-10-03 00:00Z
	a, _ := NextTimes("0 9 * * *", "UTC", fromNY, 1)
	b, _ := NextTimes("0 9 * * *", "UTC", from, 1)
	if !a[0].Equal(b[0]) {
		t.Errorf("%v vs %v", a[0], b[0])
	}
}

func TestNextTimesDayOfMonthAndWeek(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	times, err := NextTimes("0 0 29 2 *", "UTC", from, 1)
	if err != nil {
		t.Fatal(err)
	}
	if times[0].Year() != 2028 || times[0].Month() != time.February || times[0].Day() != 29 {
		t.Errorf("leap day = %v", times[0])
	}
	times, err = NextTimes("0 9 * * 1-5", "UTC", time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 5) // Saturday
	if err != nil {
		t.Fatal(err)
	}
	for _, tm := range times {
		if tm.Weekday() == time.Saturday || tm.Weekday() == time.Sunday {
			t.Errorf("weekend fire %v", tm)
		}
	}
	if times[0].Weekday() != time.Monday {
		t.Errorf("first = %v", times[0].Weekday())
	}
}

func TestNextReturnsUTCPointer(t *testing.T) {
	from := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	n, err := Next("0 9 * * *", "Asia/Tokyo", from)
	if err != nil {
		t.Fatal(err)
	}
	if n.Location() != time.UTC {
		t.Errorf("location = %v", n.Location())
	}
	if !n.Equal(time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)) { // 09:00 JST next day = 00:00Z
		t.Errorf("next = %v", n)
	}
	if n, err := Next("bogus", "UTC", from); err == nil || n != nil {
		t.Errorf("Next(bogus) = %v %v", n, err)
	}
}
