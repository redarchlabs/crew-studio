package reports

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// A daily report covers the local date in the schedule's time zone, is titled and filed as daily, and keeps its own
// note: the weekly report and its note are untouched.
func TestDailyReportsAreSeparateFromWeekly(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()
	// 2026-10-06 03:30 UTC is still 2026-10-05 in Denver
	f.svc.now = func() time.Time { return time.Date(2026, 10, 6, 3, 30, 0, 0, time.UTC) }
	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	submit := func(cadence, md string) Outcome {
		t.Helper()
		out, err := f.svc.Submit(ctx, Submission{Scope: f.scope, WorkOrderID: &f.wo.ID, WorkOrderNumber: f.wo.Number, Location: denver,
			Report: model.ReportRequest{Project: "Atlas", Markdown: md, Cadence: cadence}})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	weekly := submit("", "# Atlas weekly")
	daily := submit("daily", "# Atlas today")
	rep := daily.Report
	if rep.Cadence != model.CadenceDaily || rep.Period != "2026-10-05" || rep.Title != "Atlas daily report, 2026-10-05" || rep.Week != "2026-W41" {
		t.Fatalf("daily = %+v", rep)
	}
	if weekly.Report.Cadence != model.CadenceWeekly || weekly.Report.Period != "2026-W41" || weekly.Report.Title != "Atlas status, 2026-W41" {
		t.Errorf("weekly = %+v", weekly.Report)
	}
	if got := statuses(rep); got != "work_order=sent slack=sent teams=sent email=sent km2=sent" {
		t.Errorf("statuses = %s", got)
	}
	if len(f.saved) != 2 || f.saved[1].ExternalRef != "atlas-daily-2026-10-05" || f.saved[1].Metadata["kind"] != "daily-report" ||
		f.saved[1].Metadata["date"] != "2026-10-05" || f.saved[0].Metadata["kind"] != "weekly-report" {
		t.Errorf("km2 = %+v", f.saved)
	}
	if !strings.Contains(strings.Join(f.send.sent, "\n"), "EMAIL|sam@example.com|Atlas daily report, 2026-10-05|") {
		t.Errorf("sent = %v", f.send.sent)
	}
	scope := store.StateScope{CrewID: peCrew, ScheduleID: f.scope.ScheduleID}
	note := func(name string) string {
		n, err := f.st.LatestNote(ctx, store.NoteKey{StateScope: scope, Subject: "project:atlas", Name: name})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return n.Content
	}
	if note(model.ReportNote) != "# Atlas weekly" || note(model.DailyReportNote) != "# Atlas today" {
		t.Errorf("notes = %q / %q", note(model.ReportNote), note(model.DailyReportNote))
	}

	// The same daily report again in the run sends nothing twice; a different one is refused
	sent := len(f.send.sent)
	if again := submit("daily", "# Atlas today"); !again.Existing || again.Report.ID != rep.ID || len(f.send.sent) != sent {
		t.Errorf("repeat = %+v, sent %d -> %d", again, sent, len(f.send.sent))
	}
	_, err = f.svc.Submit(ctx, Submission{Scope: f.scope, Location: denver, Report: model.ReportRequest{Project: "Atlas", Markdown: "# other", Cadence: "daily"}})
	var conflict *store.ConflictError
	if !errors.As(err, &conflict) || !strings.Contains(conflict.Message, "daily report for Atlas") {
		t.Errorf("a second daily report = %v", err)
	}

	// The work order lists both, each with its cadence and period
	list, err := f.st.WorkOrderReports(ctx, f.wo.ID)
	if err != nil || len(list) != 2 || list[0].Cadence != "weekly" || list[1].Cadence != "daily" || list[1].Period != "2026-10-05" {
		t.Errorf("work order reports = %+v %v", list, err)
	}
	// The action log knows the cadence
	entries, _, err := f.st.ListActions(ctx, store.ActionFilter{Scope: scope, AllSchedules: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Details["cadence"] == "daily" && e.Details["period"] == "2026-10-05" {
			found = true
		}
	}
	if !found {
		t.Error("no action log entry carries the daily cadence")
	}
}

// In a dry-run watch a daily report is stored as a dry run and kept on the work order (as weekly ones are), and every
// destination only would send.
func TestDailyReportInADryRun(t *testing.T) {
	f := newFixture(t, true)
	out, err := f.svc.Submit(context.Background(), Submission{Scope: f.scope, WorkOrderID: &f.wo.ID, WorkOrderNumber: f.wo.Number,
		Report: model.ReportRequest{Project: "Atlas", Markdown: "# Today", Cadence: "daily"}})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Report.DryRun || out.Report.Period != "2026-10-05" {
		t.Errorf("report = %+v", out.Report)
	}
	if got := statuses(out.Report); got != "work_order=sent slack=would_send teams=would_send email=would_send km2=would_send" {
		t.Errorf("statuses = %s", got)
	}
	if len(f.send.sent) != 0 {
		t.Errorf("a dry run sent %v", f.send.sent)
	}
	list, err := f.st.WorkOrderReports(context.Background(), f.wo.ID)
	if err != nil || len(list) != 1 || !list[0].DryRun || list[0].Cadence != "daily" {
		t.Errorf("work order reports = %+v %v", list, err)
	}
}
