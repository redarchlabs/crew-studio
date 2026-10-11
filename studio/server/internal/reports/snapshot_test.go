package reports

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/duereport"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

type fakeSnaps struct {
	raw json.RawMessage
	why string
	req *SnapshotRequest
}

func (f *fakeSnaps) Snapshot(_ context.Context, req SnapshotRequest) (json.RawMessage, string) {
	f.req = &req
	return f.raw, f.why
}

func sampleSnapshot(t *testing.T) json.RawMessage {
	t.Helper()
	now := time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)
	issues := []duereport.Tracked{
		{Issue: tracker.Issue{Key: "ERP-2", Title: "Vendor <b>access</b>", URL: "https://km2.example.com/r?id=2", Status: "Todo", StatusCategory: model.StatusToDo,
			DueDate: "2026-10-01", Assignee: &tracker.Person{AccountID: "s", Name: "Sam"}, Labels: []string{"blocked"}}, Epic: "ERP-1"},
		{Issue: tracker.Issue{Key: "ERP-5", Title: "Mapping", Status: "Todo", StatusCategory: model.StatusToDo, DueDate: "2026-10-08"}, Epic: "ERP-1"},
	}
	raw, err := duereport.Encode(duereport.Make(duereport.Input{Project: "Atlas", Cadence: "daily", Location: time.UTC, Now: now, Issues: issues}))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// With a snapshot, chat gets the compact summary, email the whole report as text plus its HTML page, and KM2 the
// Markdown built from the data; the action log records that the snapshot was included.
func TestDeliveriesAreBuiltFromTheSnapshot(t *testing.T) {
	f := newFixture(t, false)
	snaps := &fakeSnaps{raw: sampleSnapshot(t)}
	f.svc.WithSnapshots(snaps)
	out, err := f.svc.Submit(context.Background(), Submission{Scope: f.scope, WorkOrderID: &f.wo.ID, WorkOrderNumber: f.wo.Number, Location: time.UTC,
		Report: model.ReportRequest{Project: "Atlas", Markdown: "On track; vendor access is the critical path.", Cadence: "daily"}})
	if err != nil {
		t.Fatal(err)
	}
	if snaps.req == nil || snaps.req.Project != "Atlas" || snaps.req.Cadence != "daily" || snaps.req.CrewID != peCrew {
		t.Fatalf("snapshot request = %+v", snaps.req)
	}
	if len(out.Report.Snapshot) == 0 || statuses(out.Report) != "work_order=sent slack=sent teams=sent email=sent km2=sent" {
		t.Fatalf("report = %+v", out.Report)
	}
	slack := f.send.msgs["SLACK"].Markdown
	if !strings.HasPrefix(slack, "**Overdue 1 · Due within 7 days 1") || !strings.Contains(slack, "**Most overdue**") ||
		!strings.Contains(slack, "On track; vendor access") || f.send.msgs["SLACK"].HTML != "" {
		t.Errorf("slack = %q", slack)
	}
	mail := f.send.msgs["EMAIL"]
	if !strings.Contains(mail.Markdown, "## Overdue") || !strings.Contains(mail.HTML, "<!DOCTYPE html>") ||
		!strings.Contains(mail.HTML, "Vendor &lt;b&gt;access&lt;/b&gt;") || strings.Contains(mail.HTML, "<b>access") {
		t.Errorf("email = %q / html %d bytes", mail.Markdown, len(mail.HTML))
	}
	if len(f.saved) != 1 || !strings.Contains(f.saved[0].Content, "## Summary") || !strings.Contains(f.saved[0].Content, "## Overdue") {
		t.Errorf("km2 = %+v", f.saved)
	}
	entries, _, err := f.st.ListActions(context.Background(), store.ActionFilter{Scope: store.StateScope{CrewID: peCrew}, AllSchedules: true, Limit: 20})
	if err != nil || len(entries) == 0 || entries[0].Details["snapshot"] != "included" {
		t.Errorf("entries = %+v %v", entries, err)
	}
}

// Without a snapshot the report goes out as the model wrote it, and the action log says why.
func TestDeliveryWithoutASnapshot(t *testing.T) {
	f := newFixture(t, false)
	f.svc.WithSnapshots(&fakeSnaps{why: "the tracker did not answer within 20 seconds"})
	out := f.submit(t, "Atlas", "# Atlas\nAs written.")
	if string(out.Report.Snapshot) != "" && string(out.Report.Snapshot) != "null" {
		t.Errorf("snapshot = %s", out.Report.Snapshot)
	}
	if f.send.msgs["SLACK"].Markdown != "# Atlas\nAs written." || f.send.msgs["EMAIL"].HTML != "" || f.saved[0].Content != "# Atlas\nAs written." {
		t.Errorf("messages = %+v / km2 %q", f.send.msgs, f.saved[0].Content)
	}
	entries, _, err := f.st.ListActions(context.Background(), store.ActionFilter{Scope: store.StateScope{CrewID: peCrew}, AllSchedules: true, Limit: 20})
	if err != nil || entries[0].Details["snapshot"] != "left out: the tracker did not answer within 20 seconds" {
		t.Errorf("entries = %+v %v", entries, err)
	}
}
