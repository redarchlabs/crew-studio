package duereport

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/tracker"
)

var denver = mustLoc("America/Denver")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// 2026-10-07 03:00 UTC is still 2026-10-06 in Denver
var now = time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC)

func issue(key, due, cat string, assignee string, labels ...string) tracker.Issue {
	i := tracker.Issue{Key: key, Title: "Item " + key, URL: "https://km2.example.com/r?id=" + key, Status: "Todo", StatusCategory: cat,
		DueDate: due, Labels: labels, UpdatedAt: now}
	if assignee != "" {
		i.Assignee = &tracker.Person{AccountID: "acc-" + assignee, Name: assignee}
	}
	if i.Labels == nil {
		i.Labels = []string{}
	}
	return i
}

func project() []Tracked {
	issues := []tracker.Issue{
		issue("ERP-1", "2026-12-01", model.StatusInProgress, "Priya"),
		issue("ERP-2", "2026-10-03", model.StatusToDo, "Sam", "blocked"), // 3 days late in Denver
		issue("ERP-3", "", model.StatusDone, "Sam"),
		issue("ERP-4", "2026-10-06", model.StatusToDo, "Sam"),         // due today in Denver (tomorrow in UTC terms)
		issue("ERP-5", "2026-10-12", model.StatusToDo, ""),            // due in 6 days, unassigned
		issue("ERP-6", "2026-10-12", model.StatusInProgress, "Priya"), // the cliff
		issue("ERP-7", "2026-10-25", model.StatusToDo, ""),            // unassigned, 19 days out: not "soon"
		issue("ERP-8", "", model.StatusCanceled, ""),
	}
	out := make([]Tracked, len(issues))
	for i, is := range issues {
		out[i] = Tracked{Issue: is, Epic: "ERP-1"}
	}
	return out
}

func TestMakeDerivesSectionsInTheScheduleTimeZone(t *testing.T) {
	s := Make(Input{Project: "ERP", Cadence: "daily", Location: denver, Now: now, Issues: project()})
	if s.Today != "2026-10-06" || s.Timezone != "America/Denver" || s.Deltas != nil || s.Changes != nil || s.KPIs.DoneSincePrev != nil {
		t.Fatalf("snapshot = %+v", s)
	}
	if len(s.Sections.Overdue) != 1 || s.Sections.Overdue[0] != (Late{Key: "ERP-2", DaysLate: 3}) {
		t.Errorf("overdue = %+v", s.Sections.Overdue)
	}
	if strings.Join(s.Sections.DueToday, ",") != "ERP-4" || strings.Join(s.Sections.DueNext7, ",") != "ERP-5,ERP-6" {
		t.Errorf("due = %v / %v", s.Sections.DueToday, s.Sections.DueNext7)
	}
	if strings.Join(s.Sections.Blocked, ",") != "ERP-2" || strings.Join(s.Sections.UnassignedSoon, ",") != "ERP-5" {
		t.Errorf("blocked %v unassigned %v", s.Sections.Blocked, s.Sections.UnassignedSoon)
	}
	want := KPIs{Open: 6, Overdue: 1, DueToday: 1, Due7: 3, Blocked: 1, UnassignedSoon: 1}
	if s.KPIs != want {
		t.Errorf("kpis = %+v", s.KPIs)
	}
	if s.StatusMix != (StatusMix{ToDo: 4, InProgress: 2, Done: 1, Canceled: 1}) {
		t.Errorf("mix = %+v", s.StatusMix)
	}
	if len(s.Timeline) != TimelineDays || s.Timeline[0].Date != "2026-10-06" || s.Timeline[0].Count != 1 || s.Timeline[6].Count != 2 {
		t.Errorf("timeline = %+v", s.Timeline)
	}
	if p := s.Sections.ByPerson[0]; p.Name != "Sam" || p.Overdue != 1 || p.Due7 != 1 || p.Open != 2 {
		t.Errorf("by person = %+v", s.Sections.ByPerson)
	}
	if len(s.Trend) != 1 || s.Trend[0].Date != "2026-10-06" {
		t.Errorf("trend = %+v", s.Trend)
	}
	// ERP-1 is the tracked item itself: its epic is left empty; the others sit under it
	byKey := s.ItemsByKey()
	if byKey["ERP-1"].Epic != "" || byKey["ERP-5"].Epic != "ERP-1" || byKey["ERP-2"].URL == "" || len(s.Items) != 6 {
		t.Errorf("items = %+v", s.Items)
	}
	// The same moment in UTC is the next day: ERP-4 is then overdue
	utc := Make(Input{Project: "ERP", Cadence: "daily", Location: time.UTC, Now: now, Issues: project()})
	if utc.Today != "2026-10-07" || utc.KPIs.Overdue != 2 {
		t.Errorf("utc = %s %+v", utc.Today, utc.KPIs)
	}
}

func TestMakeComparesWithThePreviousReport(t *testing.T) {
	yesterday := Make(Input{Project: "ERP", Cadence: "daily", Location: denver, Now: now.Add(-24 * time.Hour), Issues: project()})
	issues := project()
	issues[0].Issue.StatusCategory = model.StatusDone                        // ERP-1 finished
	issues[3].Issue.DueDate = "2026-10-09"                                   // ERP-4 slipped from 10-06
	issues[4].Issue.Assignee = &tracker.Person{AccountID: "x", Name: "Dana"} // ERP-5 picked up
	issues = append(issues, Tracked{Issue: issue("ERP-9", "2026-10-01", model.StatusToDo, "Dana"), Epic: "ERP-1"})
	s := Make(Input{Project: "ERP", Cadence: "daily", Location: denver, Now: now, Issues: issues,
		History: []Previous{{ReportID: "r-prev", Snapshot: yesterday}}})
	c := s.Changes
	if s.PreviousReportID != "r-prev" || c == nil {
		t.Fatalf("no comparison: %+v", s)
	}
	if len(c.Finished) != 1 || c.Finished[0].Key != "ERP-1" || *s.KPIs.DoneSincePrev != 1 {
		t.Errorf("finished = %+v", c.Finished)
	}
	if len(c.Slipped) != 1 || c.Slipped[0] != (Move{Key: "ERP-4", Title: "Item ERP-4", From: "2026-10-06", To: "2026-10-09"}) {
		t.Errorf("slipped = %+v", c.Slipped)
	}
	if len(c.Reassigned) != 1 || c.Reassigned[0].To != "Dana" || c.Reassigned[0].From != "" {
		t.Errorf("reassigned = %+v", c.Reassigned)
	}
	if strings.Join(c.New, ",") != "ERP-9" || strings.Join(c.NewlyOverdue, ",") != "ERP-9" {
		t.Errorf("new %v newly overdue %v", c.New, c.NewlyOverdue)
	}
	// ERP-2 was already overdue yesterday, so it is not "newly" overdue
	if s.Deltas.Overdue != s.KPIs.Overdue-yesterday.KPIs.Overdue || s.Deltas.Open != -1+1 {
		t.Errorf("deltas = %+v (now %+v, before %+v)", s.Deltas, s.KPIs, yesterday.KPIs)
	}
	if len(s.Trend) != 2 || s.Trend[0].Date != "2026-10-05" || s.Trend[1].Date != "2026-10-06" {
		t.Errorf("trend = %+v", s.Trend)
	}
}

func TestSnapshotsStayWithinTheirLimits(t *testing.T) {
	var issues []Tracked
	for i := 0; i < MaxItems+150; i++ {
		due := "2026-11-20"
		if i%5 == 0 {
			due = "2026-10-01" // overdue: named in a section, so kept first
		}
		long := strings.Repeat("x", 2000)
		is := issue(fmt.Sprintf("BIG-%d", i), due, model.StatusToDo, "P"+fmt.Sprint(i%80))
		is.Title = long
		issues = append(issues, Tracked{Issue: is})
	}
	s := Make(Input{Project: "Big", Cadence: "daily", Location: time.UTC, Now: now, Issues: issues})
	if !s.Truncated || len(s.Items) != MaxItems || s.KPIs.Open != MaxItems+150 || len(s.Sections.ByPerson) != MaxPeople {
		t.Fatalf("items %d open %d people %d truncated %v", len(s.Items), s.KPIs.Open, len(s.Sections.ByPerson), s.Truncated)
	}
	if len(s.Sections.Overdue) != MaxSectionRows {
		t.Errorf("overdue rows = %d", len(s.Sections.Overdue))
	}
	if len([]rune(s.Items[0].Title)) > maxText+1 {
		t.Errorf("title not cut: %d", len(s.Items[0].Title))
	}
	raw, err := Encode(s)
	if err != nil || len(raw) > MaxBytes {
		t.Fatalf("encoded %d bytes, %v", len(raw), err)
	}
	back, err := Parse(raw)
	if err != nil || back.KPIs.Open != s.KPIs.Open {
		t.Errorf("parse = %+v %v", back, err)
	}
	if none, err := Parse(nil); none != nil || err != nil {
		t.Error("an empty snapshot parsed")
	}
	if _, err := Parse(json.RawMessage(`{bad`)); err == nil {
		t.Error("bad JSON parsed")
	}
}

func TestSafeURLAllowsOnlyHTTP(t *testing.T) {
	for raw, want := range map[string]string{
		"https://km2.example.com/r?id=1": "https://km2.example.com/r?id=1", "http://x.example/a": "http://x.example/a",
		"javascript:alert(1)": "", "data:text/html,<script>": "", "//evil.example/x": "", "/relative": "", " JAVASCRIPT:alert(1)": "",
	} {
		if got := SafeURL(raw); got != want {
			t.Errorf("%q = %q, want %q", raw, got, want)
		}
	}
}

// Among people with as much due, whoever has more overdue comes first, before whoever merely has more open work.
func TestByPersonPutsOverdueFirstOnATie(t *testing.T) {
	issues := []Tracked{
		{Issue: issue("A-1", "2026-10-03", model.StatusToDo, "Sam")},
		{Issue: issue("A-2", "2026-10-08", model.StatusToDo, "")},
		{Issue: issue("A-3", "2026-12-08", model.StatusToDo, "")},
	}
	s := Make(Input{Project: "A", Cadence: "daily", Location: denver, Now: now, Issues: issues})
	if len(s.Sections.ByPerson) != 2 || s.Sections.ByPerson[0].Name != "Sam" {
		t.Errorf("by person = %+v", s.Sections.ByPerson)
	}
}
