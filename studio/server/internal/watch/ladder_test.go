package watch

import (
	"encoding/json"
	"testing"
	"time"

	"crewstudio/internal/model"
)

func testLadder() model.FollowupLadder {
	return model.FollowupLadder{
		DateField: "due",
		SkipWhen:  map[string][]string{"status": {"Done", "Canceled"}},
		Steps: []model.FollowupStep{
			{Name: "due_soon", Offset: "-3d", Notify: "owner"},
			{Name: "due_today", Offset: "0d", Notify: "owner"},
			{Name: "overdue_escalate", Offset: "+2d", Notify: "lead"},
			{Name: "overdue_weekly", Offset: "+9d", Notify: "lead", Repeat: "7d"},
		},
		QuietDays:  []string{"Sat", "Sun"},
		MaxPerTick: 25,
	}
}

func item(id, due, status string) Item {
	return Item{ID: id, Fields: map[string]any{"due": due, "status": status, "owner": "sam"}}
}

func dueSteps(due []FollowupDue) map[string]string {
	out := map[string]string{}
	for _, d := range due {
		out[d.Item] = d.Step
	}
	return out
}

func TestDueFollowups(t *testing.T) {
	denver, _ := time.LoadLocation("America/Denver")
	// Tuesday 2026-10-06, 07:00 in Denver
	now := time.Date(2026, 10, 6, 7, 0, 0, 0, denver)
	items := []Item{
		item("SOON", "2026-10-09", "Open"),                 // Friday: due_soon was 10-06
		item("LATER", "2026-10-20", "Open"),                // nothing yet
		item("TODAY", "2026-10-06T09:00:00-06:00", "Open"), // due_today
		item("LATE", "2026-10-02", "Open"),                 // +2d = Sun 10-04 -> Mon 10-05: overdue_escalate only
		item("DONE", "2026-10-06", "done"),                 // skipped (ignoring case)
		item("NODATE", "", "Open"),
		item("BADDATE", "next week", "Open"),
		item("OLD", "2026-09-20", "Open"), // +9d, repeating weekly
	}
	due, waiting := DueFollowups(testLadder(), items, Sent{}, now, denver)
	got := dueSteps(due)
	want := map[string]string{"SOON": "due_soon", "TODAY": "due_today", "LATE": "overdue_escalate", "OLD": "overdue_weekly"}
	if len(got) != len(want) || waiting != 0 {
		t.Fatalf("due = %v (waiting %d)", got, waiting)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
	// Oldest first; quiet-day shift recorded in step_date; notify value from the tracked field
	if due[0].Item != "OLD" || due[1].Item != "LATE" || due[1].StepDate != "2026-10-05" || due[1].DaysLate != 1 {
		t.Errorf("order/dates = %+v", due[:2])
	}
	var soon FollowupDue
	for _, d := range due {
		if d.Item == "SOON" {
			soon = d
		}
	}
	if soon.Action != "followup:due_soon" || soon.Subject != "SOON" || soon.NotifyValue != "sam" || soon.Fields["status"] != "Open" {
		t.Errorf("soon = %+v", soon)
	}
	var lateEntry FollowupDue
	for _, d := range due {
		if d.Item == "LATE" {
			lateEntry = d
		}
	}
	if lateEntry.NotifyValue != nil {
		t.Errorf("lead is not a tracked field: %+v", lateEntry)
	}

	// Sent steps are not due again; repeating steps come back after their period
	sent := Sent{
		{Subject: "SOON", Action: "followup:due_soon"}:      now.Add(-time.Hour),
		{Subject: "OLD", Action: "followup:overdue_weekly"}: now.Add(-3 * 24 * time.Hour),
	}
	got = dueSteps(must(DueFollowups(testLadder(), items, sent, now, denver)))
	if _, ok := got["SOON"]; ok {
		t.Errorf("sent step still due: %v", got)
	}
	if _, ok := got["OLD"]; ok {
		t.Errorf("repeat within period: %v", got)
	}
	sent[SentKey{Subject: "OLD", Action: "followup:overdue_weekly"}] = now.Add(-8 * 24 * time.Hour)
	again, _ := DueFollowups(testLadder(), items, sent, now, denver)
	for _, d := range again {
		if d.Item == "OLD" && (d.LastSentAt == nil || d.Repeat != "7d") {
			t.Errorf("repeat entry = %+v", d)
		}
	}
	if dueSteps(again)["OLD"] != "overdue_weekly" {
		t.Errorf("repeat after period not due: %v", dueSteps(again))
	}

	// Nothing is due on a quiet day
	saturday := time.Date(2026, 10, 10, 9, 0, 0, 0, denver)
	if d, w := DueFollowups(testLadder(), items, Sent{}, saturday, denver); len(d) != 0 || w != 0 {
		t.Errorf("quiet day: %v", d)
	}

	// The cap holds the rest back
	capped := testLadder()
	capped.MaxPerTick = 2
	d, w := DueFollowups(capped, items, Sent{}, now, denver)
	if len(d) != 2 || w != 2 {
		t.Errorf("cap: %d due, %d waiting", len(d), w)
	}
	capped.MaxPerTick = 0
	if d, _ := DueFollowups(capped, items, Sent{}, now, denver); len(d) != 4 {
		t.Errorf("default cap: %d", len(d))
	}
}

func must[T any](v T, _ int) T { return v }

func TestDecideAndInputs(t *testing.T) {
	none := Changes{Added: []AddedItem{}, Changed: []ChangedItem{}, Removed: []RemovedItem{}}
	some := Changes{Added: []AddedItem{{ID: "A"}}}
	due := &Followups{Due: []FollowupDue{{Item: "A"}}}
	tests := []struct {
		name    string
		runWhen string
		ch      Changes
		f       *Followups
		force   bool
		want    bool
		reason  string
	}{
		{"nothing", model.WatchRunChanged, none, nil, false, false, "checked, no changes"},
		{"changed", model.WatchRunChanged, some, nil, false, true, "1 added"},
		{"always", model.WatchRunAlways, none, nil, false, true, "runs every time"},
		{"followups only, changed", model.WatchRunChanged, none, due, false, false, "only runs when something changed"},
		{"followups", model.WatchRunChangedOrFollowups, none, due, false, true, "1 follow-ups due"},
		{"forced", model.WatchRunChanged, none, nil, true, true, "started by hand"},
	}
	for _, tt := range tests {
		run, reason := decide(tt.runWhen, tt.ch, tt.f, tt.force)
		if run != tt.want || !contains(reason, tt.reason) {
			t.Errorf("%s: %v %q", tt.name, run, reason)
		}
	}

	ch, in, err := inputs(some, due, nil, true)
	if err != nil || in[model.WatchInputDryRun] != "true" || in[model.WatchInputFollowups] == "" || ch.AddedFieldsOmitted {
		t.Fatalf("inputs = %v %v", in, err)
	}
	var back Changes
	if err := json.Unmarshal([]byte(in[model.WatchInputChanges]), &back); err != nil || back.Added[0].ID != "A" {
		t.Errorf("changes input = %s", in[model.WatchInputChanges])
	}
	_, in, _ = inputs(none, nil, nil, false)
	if _, ok := in[model.WatchInputFollowups]; ok || in[model.WatchInputDryRun] != "false" {
		t.Errorf("no ladder inputs = %v", in)
	}

	big := Changes{}
	for i := 0; i < 400; i++ {
		big.Added = append(big.Added, AddedItem{ID: string(rune('a'+i%26)) + string(rune('0'+i%10)) + time.Duration(i).String(),
			Fields: map[string]any{"text": string(make([]byte, 280))}})
	}
	slim, in, err := inputs(big, nil, nil, true)
	if err != nil || !slim.AddedFieldsOmitted || len(in[model.WatchInputChanges]) > MaxInputChars {
		t.Errorf("big changes: omitted=%v len=%d err=%v", slim.AddedFieldsOmitted, len(in[model.WatchInputChanges]), err)
	}
	huge := Changes{}
	for i := 0; i < 6000; i++ {
		huge.Changed = append(huge.Changed, ChangedItem{ID: time.Duration(i).String(), Fields: map[string]FieldChange{"f": {Old: "a", New: "b"}}})
	}
	if _, _, err := inputs(huge, nil, nil, true); err == nil || !contains(err.Error(), "too long") {
		t.Errorf("huge changes err = %v", err)
	}
	bigDue := &Followups{}
	for i := 0; i < 2000; i++ {
		bigDue.Due = append(bigDue.Due, FollowupDue{Item: time.Duration(i).String(), Fields: map[string]any{"x": string(make([]byte, 100))}})
	}
	if _, _, err := inputs(none, bigDue, nil, true); err == nil || !contains(err.Error(), "max_per_tick") {
		t.Errorf("huge follow-ups err = %v", err)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestLocation(t *testing.T) {
	if location(nil, "Asia/Tokyo").String() != "Asia/Tokyo" {
		t.Error("schedule zone")
	}
	if location(&model.FollowupLadder{Timezone: "Europe/London"}, "Asia/Tokyo").String() != "Europe/London" {
		t.Error("ladder zone wins")
	}
	if location(nil, "Nowhere/Land") != time.UTC {
		t.Error("bad zone falls back to UTC")
	}
	if _, ok := parseItemDate(json.Number("20261006"), time.UTC); ok {
		t.Error("a number is not a date")
	}
}

func TestLadderRepeatAndMovedDates(t *testing.T) {
	utc := time.UTC
	now := time.Date(2026, 10, 13, 7, 0, 0, 0, utc) // Tuesday, 07:00, the schedule's tick
	weekly := model.FollowupLadder{DateField: "due", Steps: []model.FollowupStep{{Name: "overdue_weekly", Offset: "+1d", Notify: "lead", Repeat: "7d"}}}
	items := []Item{item("A", "2026-09-01", "Open")}
	key := SentKey{Subject: "A", Action: "followup:overdue_weekly"}

	// Recorded seconds after last week's tick: due again at this week's tick, not a week later
	sent := Sent{key: now.Add(-7*24*time.Hour + 5*time.Second)}
	if d, _ := DueFollowups(weekly, items, sent, now, utc); len(d) != 1 {
		t.Errorf("a weekly step slipped a period: %+v", d)
	}
	sent[key] = now.Add(-6 * 24 * time.Hour)
	if d, _ := DueFollowups(weekly, items, sent, now, utc); len(d) != 0 {
		t.Errorf("due again within the period: %+v", d)
	}

	// A one-off step recorded for the old date comes back when the date moves later
	once := model.FollowupLadder{DateField: "due", Steps: []model.FollowupStep{{Name: "due_soon", Offset: "-3d", Notify: "owner"}}}
	soon := SentKey{Subject: "B", Action: "followup:due_soon"}
	sentEarlier := Sent{soon: time.Date(2026, 10, 2, 7, 1, 0, 0, utc)} // for a date of 10-05
	moved := []Item{item("B", "2026-10-15", "Open")}                   // due_soon is now 10-12
	if d, _ := DueFollowups(once, moved, sentEarlier, now, utc); len(d) != 1 {
		t.Errorf("a moved date did not bring the step back: %+v", d)
	}
	sentEarlier[soon] = time.Date(2026, 10, 12, 7, 1, 0, 0, utc)
	if d, _ := DueFollowups(once, moved, sentEarlier, now, utc); len(d) != 0 {
		t.Errorf("a step recorded for the current date is due again: %+v", d)
	}

	// Items the ladder cannot place are counted
	l := testLadder()
	undated := []Item{item("X", "", "Open"), item("Y", "03/04/2026", "Open"), item("Z", "", "Done"), item("W", "2026-10-01", "Open")}
	if n := UndatedItems(l, undated, utc); n != 2 {
		t.Errorf("undated = %d", n)
	}
}
