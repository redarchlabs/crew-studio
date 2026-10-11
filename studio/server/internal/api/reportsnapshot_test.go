package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"crewstudio/internal/duereport"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// A daily report delivered by a run whose crew tracks a KM2 project carries the snapshot Crew Studio read itself,
// lists it on the work order and downloads as a safe HTML page; when the tracker fails the report still arrives,
// without a snapshot, and the action log says why.
func TestReportsCarryATrackerSnapshot(t *testing.T) {
	e := newTrackerEnv(t)
	sr := e.scheduledRun(t)
	conn := e.km2Connection(t)
	ctx := context.Background()
	if err := e.st.SetCrewConnections(ctx, sr.crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddTrackingItem(ctx, sr.crew.ID, model.TrackingItemInput{ConnectionID: conn.ID, Project: "Atlas", ItemRef: "PRJ-10",
		IncludeChildren: model.IncludeChildrenAll}, nil); err != nil {
		t.Fatal(err)
	}
	r := e.call(t, sr, "POST", "/reports", map[string]any{"project": "Atlas", "markdown": "**On track.** Vendor access is the critical path.", "cadence": "daily"})
	wantStatus(t, r, http.StatusCreated)
	woID := *sr.run.WorkOrderID
	view := decodeData[workOrderReportsView](t, e.do(t, "GET", "/api/work-orders/"+woID+"/reports", nil))
	if len(view.Reports) != 1 {
		t.Fatalf("reports = %+v", view.Reports)
	}
	rep := view.Reports[0]
	snap, err := duereport.Parse(rep.Snapshot)
	if err != nil || snap == nil {
		t.Fatalf("snapshot = %s %v", rep.Snapshot, err)
	}
	// PRJ-10 with children PRJ-11 (late, blocked, Sam's) and PRJ-12 (done)
	byKey := snap.ItemsByKey()
	if snap.KPIs.Open != 2 || snap.KPIs.Blocked != 1 || byKey["PRJ-11"].Assignee != "Sam Lee" || byKey["PRJ-11"].Epic != "PRJ-10" ||
		snap.StatusMix.Done != 1 || snap.Project != "Atlas" || snap.Cadence != "daily" {
		t.Errorf("snapshot = %+v", snap)
	}
	entries, _, err := e.st.ListActions(ctx, store.ActionFilter{Scope: store.StateScope{CrewID: sr.crew.ID}, AllSchedules: true, Limit: 20})
	if err != nil || len(entries) == 0 || entries[0].Details["snapshot"] != "included" {
		t.Errorf("action log = %+v %v", entries, err)
	}

	// The HTML download: an attachment that runs nothing, for people who may see the work order only
	status, header, body := e.fetch(t, "/api/work-orders/"+woID+"/reports/"+rep.ID+"/html")
	if status != http.StatusOK || !strings.Contains(header.Get("Content-Disposition"), `attachment; filename="atlas-daily-`) ||
		!strings.Contains(header.Get("Content-Security-Policy"), "default-src 'none'") || header.Get("Content-Type") != "text/html; charset=utf-8" ||
		!strings.Contains(body, "Vendor access") || strings.Contains(strings.ToLower(body), "<script") {
		t.Errorf("download = %d %v", status, header)
	}
	wantStatus(t, e.do(t, "GET", "/api/work-orders/"+woID+"/reports/"+missingID+"/html", nil), http.StatusNotFound)
	_, itTok := e.person(t, "it-reader@example.com", []string{viewerRole}, []string{itArea}, nil)
	wantStatus(t, e.as(itTok).do(t, "GET", "/api/work-orders/"+woID+"/reports/"+rep.ID+"/html", nil), http.StatusNotFound)
	wantStatus(t, e.as(itTok).do(t, "GET", "/api/work-orders/"+woID+"/reports", nil), http.StatusNotFound)

	// The tracker fails: the weekly report is delivered without a snapshot, saying why
	e.data.Fail = http.StatusBadGateway
	r = e.call(t, sr, "POST", "/reports", map[string]any{"project": "Atlas", "markdown": "This week."})
	wantStatus(t, r, http.StatusCreated)
	e.data.Fail = 0
	view = decodeData[workOrderReportsView](t, e.do(t, "GET", "/api/work-orders/"+woID+"/reports", nil))
	if len(view.Reports) != 2 || string(view.Reports[1].Snapshot) != "null" {
		t.Fatalf("weekly = %s", view.Reports[1].Snapshot)
	}
	entries, _, _ = e.st.ListActions(ctx, store.ActionFilter{Scope: store.StateScope{CrewID: sr.crew.ID}, AllSchedules: true, Limit: 20})
	note, _ := entries[0].Details["snapshot"].(string)
	if !strings.HasPrefix(note, "left out: ") || strings.Contains(note, km2TrackerKey) {
		t.Errorf("snapshot note = %q", note)
	}
	if status, _, body := e.fetch(t, "/api/work-orders/"+woID+"/reports/"+view.Reports[1].ID+"/html"); status != http.StatusOK || !strings.Contains(body, "This week.") {
		t.Errorf("download without a snapshot = %d", status)
	}
}
