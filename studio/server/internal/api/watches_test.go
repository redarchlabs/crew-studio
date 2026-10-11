package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

type watchViewBody struct {
	Watch       *model.Watch            `json:"watch"`
	Stats       *model.WatchStats       `json:"stats"`
	Checks      []model.WatchCheck      `json:"checks"`
	SourceKinds []model.WatchSourceKind `json:"source_kinds"`
	Connections []watchConnection       `json:"connections"`
}

type previewBody struct {
	Changes struct {
		Source          string           `json:"source"`
		PreviousCheckAt *time.Time       `json:"previous_check_at"`
		Added           []map[string]any `json:"added"`
	} `json:"changes"`
	Followups *struct {
		Due []map[string]any `json:"due"`
	} `json:"followups_due"`
	WouldRun  bool             `json:"would_run"`
	Reason    string           `json:"reason"`
	ItemCount int              `json:"item_count"`
	Sample    []map[string]any `json:"sample"`
}

func httpWatchBody(connID string) map[string]any {
	return map[string]any{"connection_id": connID, "id_field": "key", "tracked_fields": []string{"status", "due"},
		"source":    map[string]any{"kind": "http_json", "path": "/issues", "items_path": "items"},
		"run_when":  "changed_or_followups",
		"followups": map[string]any{"date_field": "due", "steps": []map[string]any{{"name": "due_today", "offset": "0d", "notify": "owner"}}}}
}

func TestScheduleWatchAPI(t *testing.T) {
	e := newEnv(t)
	sr := e.scheduledRun(t)
	today := time.Now().UTC().Format(time.DateOnly)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items": [{"key": "PRJ-1", "status": "Open", "due": "` + today + `"}, {"key": "PRJ-2", "status": "Open", "due": null}]}`))
	}))
	defer api.Close()
	base := "/api/schedules/" + sr.schedule.ID + "/watch"

	view := decodeData[watchViewBody](t, e.do(t, "GET", base, nil))
	if view.Watch != nil || len(view.SourceKinds) != 5 || len(view.Connections) != 0 || len(view.Checks) != 0 {
		t.Fatalf("empty view = %+v", view)
	}
	wantStatus(t, e.do(t, "PUT", base, map[string]any{"source": map[string]any{"kind": "jira"}}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "PUT", base, map[string]any{"source": map[string]any{"kind": "web_page", "url": "https://example.com", "selector": "p[["}}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "PUT", base, httpWatchBody("99999999-0000-4000-8000-000000000000")), http.StatusUnprocessableEntity)

	ctx := store.WithActor(context.Background(), store.Actor{ID: e.adminID, Name: "Ada Admin"})
	conn, err := e.st.CreateConnection(ctx, "99999999-0000-4000-8000-0000000000c1", model.ConnectionInput{Name: "Tracker", Kind: "http",
		Config: map[string]string{"base_url": api.URL}, AreaIDs: []string{financeArea}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := e.do(t, "PUT", base, httpWatchBody(conn.ID))
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(strings.Join(r.body.Error.Details, " "), "may not use the connection Tracker") {
		t.Errorf("unlinked: %+v", r.body.Error)
	}
	if err := e.st.SetCrewConnections(ctx, sr.crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}

	// Preview the unsaved definition: a first check, nothing saved
	r = e.do(t, "POST", base+"/preview", httpWatchBody(conn.ID))
	wantStatus(t, r, http.StatusOK)
	p := decodeData[previewBody](t, r)
	if !p.WouldRun || p.ItemCount != 2 || len(p.Changes.Added) != 2 || p.Changes.PreviousCheckAt != nil || p.Followups == nil ||
		len(p.Followups.Due) != 1 || len(p.Sample) != 2 || p.Changes.Source != "Tracker: /issues" {
		t.Errorf("preview = %+v", p)
	}
	if v := decodeData[watchViewBody](t, e.do(t, "GET", base, nil)); v.Watch != nil {
		t.Error("preview saved the watch")
	}

	r = e.do(t, "PUT", base, httpWatchBody(conn.ID))
	wantStatus(t, r, http.StatusOK)
	view = decodeData[watchViewBody](t, r)
	if view.Watch == nil || view.Watch.ConnectionName != "Tracker" || !view.Watch.DryRun || view.Stats == nil || len(view.Connections) != 1 {
		t.Fatalf("saved view = %+v", view)
	}

	// A source that fails is a 422 with the reason
	bad := map[string]any{"source": map[string]any{"kind": "web_page", "url": api.URL + "/page"}}
	r = e.do(t, "POST", base+"/preview", bad)
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if r.body.Error.Code != "source_failed" || !strings.Contains(strings.Join(r.body.Error.Details, " "), "not allowed") {
		t.Errorf("loopback page preview: %+v", r.body.Error)
	}

	// The crew's State tab lists the watch with its savings
	list := decodeData[[]map[string]any](t, e.do(t, "GET", "/api/crews/"+sr.crew.ID+"/state/watches", nil))
	if len(list) != 1 || list[0]["schedule_name"] != "atlas sweep" || list[0]["stats"] == nil {
		t.Errorf("crew watches = %+v", list)
	}

	// Viewers see the watch but cannot change it or list connections; other areas cannot see it
	_, viewer := e.person(t, "viewer@example.com", []string{viewerRole}, []string{financeArea}, nil)
	if v := decodeData[watchViewBody](t, e.as(viewer).do(t, "GET", base, nil)); v.Watch == nil || len(v.Connections) != 0 {
		t.Errorf("viewer view = %+v", v)
	}
	wantStatus(t, e.as(viewer).do(t, "PUT", base, httpWatchBody(conn.ID)), http.StatusForbidden)
	wantStatus(t, e.as(viewer).do(t, "POST", base+"/preview", httpWatchBody(conn.ID)), http.StatusForbidden)
	_, outsider := e.person(t, "it@example.com", []string{builderRole}, []string{itArea}, nil)
	wantStatus(t, e.as(outsider).do(t, "GET", base, nil), http.StatusNotFound)
	wantStatus(t, e.as(outsider).do(t, "PUT", base, httpWatchBody(conn.ID)), http.StatusNotFound)

	wantStatus(t, e.do(t, "DELETE", base+"/snapshot", nil), http.StatusNoContent)
	wantStatus(t, e.do(t, "DELETE", base, nil), http.StatusNoContent)
	wantStatus(t, e.do(t, "DELETE", base, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", base+"/snapshot", nil), http.StatusNotFound)
	audit := string(e.do(t, "GET", "/api/admin/audit?limit=20", nil).body.Data)
	for _, action := range []string{"watch_saved", "watch_deleted", "watch_snapshot_cleared"} {
		if !strings.Contains(audit, action) {
			t.Errorf("audit misses %s", action)
		}
	}

	// Previews are throttled per person
	limited := false
	for i := 0; i < 15 && !limited; i++ {
		limited = e.do(t, "POST", base+"/preview", bad).status == http.StatusTooManyRequests
	}
	if !limited {
		t.Error("previews were never throttled")
	}
}

func TestDryRunRunsRecordDryRunEntries(t *testing.T) {
	e := newEnv(t)
	sr := e.scheduledRun(t)
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE runs SET inputs = '{"dry_run": "true"}' WHERE id = $1`, sr.run.ID); err != nil {
		t.Fatal(err)
	}
	r := e.call(t, sr, "POST", "/log", map[string]any{"subject": "PRJ-1", "action": "followup:due_today"})
	wantStatus(t, r, http.StatusCreated)
	var rec struct {
		Entry model.ActionEntry `json:"entry"`
	}
	if err := json.Unmarshal(r.body.Data, &rec); err != nil || !rec.Entry.DryRun {
		t.Fatalf("entry = %+v %v", rec, err)
	}
	check := e.call(t, sr, "GET", "/log/check?subject=PRJ-1&action=followup:due_today", nil)
	if !strings.Contains(string(check.body.Data), `"dry_run":true`) {
		t.Errorf("a dry run sees its own entries: %s", check.body.Data)
	}
	scope := e.call(t, sr, "GET", "/scope", nil)
	if !strings.Contains(string(scope.body.Data), `"dry_run":true`) {
		t.Errorf("scope = %s", scope.body.Data)
	}
	journal := string(e.do(t, "GET", "/api/runs/"+sr.run.ID+"/journal", nil).body.Data)
	if !strings.Contains(journal, "Log: recorded followup:due_today for PRJ-1 (dry run)") {
		t.Errorf("journal misses the dry-run line")
	}
}

func TestOnlySchedulesCarryWatchInputs(t *testing.T) {
	e := newEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	r := e.do(t, "POST", "/api/work-orders", map[string]any{"title": "forged", "crew_id": crew.ID, "submit": true,
		"inputs": map[string]string{"dry_run": "true", "followups_due": `{"due": []}`}})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	details := strings.Join(r.body.Error.Details, " ")
	if !strings.Contains(details, "inputs.dry_run") || !strings.Contains(details, "inputs.followups_due") {
		t.Errorf("details = %v", r.body.Error.Details)
	}
	wantStatus(t, e.do(t, "DELETE", "/api/schedules/99999999-0000-4000-8000-000000000000/watch/snapshot", nil), http.StatusNotFound)
}
