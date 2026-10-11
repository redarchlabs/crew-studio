package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

func TestCrewStateNotesForPeople(t *testing.T) {
	e := newEnv(t)
	sr := e.scheduledRun(t)
	wantStatus(t, e.call(t, sr, "PUT", "/notes/weekly-report", map[string]any{"subject": "project:atlas", "content": "v1 text"}), http.StatusOK)
	wantStatus(t, e.call(t, sr, "PUT", "/notes/weekly-report", map[string]any{"subject": "project:atlas", "content": "v2 text"}), http.StatusOK)
	base := "/api/crews/" + sr.crew.ID + "/state"
	key := url.Values{"schedule_id": {sr.schedule.ID}, "subject": {"project:atlas"}, "name": {"weekly-report"}}

	notes := decodeData[[]model.NoteInfo](t, e.do(t, "GET", base+"/notes", nil))
	if len(notes) != 1 || notes[0].Version != 2 || notes[0].ScheduleName != "atlas sweep" {
		t.Fatalf("notes = %+v", notes)
	}
	if none := decodeData[[]model.NoteInfo](t, e.do(t, "GET", base+"/notes?schedule_id=none", nil)); len(none) != 0 {
		t.Errorf("crew-wide = %+v", none)
	}
	wantStatus(t, e.do(t, "GET", base+"/notes?schedule_id=bogus", nil), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "GET", base+"/notes?schedule_id=99999999-0000-4000-8000-000000000000", nil), http.StatusUnprocessableEntity)

	versions := decodeData[[]model.NoteVersion](t, e.do(t, "GET", base+"/notes/versions?"+key.Encode(), nil))
	if len(versions) != 2 || versions[0].Content != "v2 text" {
		t.Fatalf("versions = %+v", versions)
	}
	wantStatus(t, e.do(t, "GET", base+"/notes/versions?name=bad%20name", nil), http.StatusUnprocessableEntity)

	edit := map[string]any{"schedule_id": sr.schedule.ID, "subject": "project:atlas", "name": "weekly-report", "content": "fixed by hand", "expected_version": 1}
	r := e.do(t, "PUT", base+"/notes", edit)
	wantStatus(t, r, http.StatusConflict)
	edit["expected_version"] = 2
	r = e.do(t, "PUT", base+"/notes", edit)
	wantStatus(t, r, http.StatusOK)
	if v := decodeData[model.NoteVersion](t, r); v.Version != 3 || v.WrittenBy != "Ada Admin" || v.RunID != nil {
		t.Errorf("edit = %+v", v)
	}
	r = e.do(t, "POST", base+"/notes/rollback", map[string]any{"schedule_id": sr.schedule.ID, "subject": "project:atlas", "name": "weekly-report", "version": 1, "expected_version": 3})
	wantStatus(t, r, http.StatusOK)
	if v := decodeData[model.NoteVersion](t, r); v.Version != 4 || v.Content != "v1 text" || !strings.Contains(v.WrittenBy, "rolled back to v1") {
		t.Errorf("rollback = %+v", v)
	}
	wantStatus(t, e.do(t, "POST", base+"/notes/rollback", map[string]any{"schedule_id": sr.schedule.ID, "subject": "project:atlas", "name": "weekly-report", "version": 99}), http.StatusNotFound)

	_, viewer := e.person(t, "viewer@example.com", []string{viewerRole}, []string{financeArea}, nil)
	if got := decodeData[[]model.NoteInfo](t, e.as(viewer).do(t, "GET", base+"/notes", nil)); len(got) != 1 {
		t.Errorf("viewer sees %d notes", len(got))
	}
	wantStatus(t, e.as(viewer).do(t, "PUT", base+"/notes", edit), http.StatusForbidden)
	_, outsider := e.person(t, "it@example.com", []string{builderRole}, []string{itArea}, nil)
	wantStatus(t, e.as(outsider).do(t, "GET", base+"/notes", nil), http.StatusNotFound)

	raw := string(e.do(t, "GET", "/api/admin/audit?limit=20", nil).body.Data)
	if !strings.Contains(raw, "note_written") || !strings.Contains(raw, "note_rolled_back") || strings.Contains(raw, "fixed by hand") {
		t.Error("hand edits and rollbacks should be audited, without the note's text")
	}
	wantStatus(t, e.do(t, "DELETE", base+"/notes?"+key.Encode(), nil), http.StatusNoContent)
	wantStatus(t, e.do(t, "DELETE", base+"/notes?"+key.Encode(), nil), http.StatusNotFound)
}

func TestCrewStateActionLogForPeople(t *testing.T) {
	e := newEnv(t)
	sr := e.scheduledRun(t)
	// Tracker follow-ups are recorded by the tracker tools (an agent's log_record may not), so they are added directly
	runID := sr.run.ID
	for _, subject := range []string{"jira:PRJ-15", "jira:PRJ-16", "project:atlas"} {
		if _, _, err := e.st.RecordAction(context.Background(), store.StateScope{CrewID: sr.crew.ID, ScheduleID: &sr.schedule.ID}, &runID,
			model.LogRecord{Subject: subject, Action: "followup:due_soon", Details: map[string]any{}}); err != nil {
			t.Fatal(err)
		}
	}
	wantStatus(t, e.call(t, sr, "POST", "/log", map[string]any{"subject": "jira:PRJ-17", "action": "followup:due_soon"}), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "POST", "/log", map[string]any{"subject": "jira:PRJ-17", "action": "tracker:comment"}), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "POST", "/log", map[string]any{"subject": "project:atlas", "action": "report:slack"}), http.StatusUnprocessableEntity)
	wantStatus(t, e.call(t, sr, "POST", "/log", map[string]any{"subject": "x", "action": "note", "idempotency_key": "run:" + sr.run.ID + ":jira:PRJ-17:tracker:comment"}),
		http.StatusUnprocessableEntity)
	base := "/api/crews/" + sr.crew.ID + "/state/log"
	r := e.do(t, "GET", base+"?subject=jira&limit=1", nil)
	wantStatus(t, r, http.StatusOK)
	entries := decodeData[[]model.ActionEntry](t, r)
	if len(entries) != 1 || r.body.Meta.Total != 2 || entries[0].ScheduleName != "atlas sweep" {
		t.Fatalf("filtered = %+v meta %+v", entries, r.body.Meta)
	}
	if r := e.do(t, "GET", base+"?schedule_id=none", nil); r.body.Meta.Total != 0 {
		t.Errorf("crew-wide total = %d", r.body.Meta.Total)
	}
	wantStatus(t, e.do(t, "GET", base+"?subject="+strings.Repeat("s", 300), nil), http.StatusUnprocessableEntity)

	_, viewer := e.person(t, "viewer@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(viewer).do(t, "DELETE", base+"/"+entries[0].ID, nil), http.StatusForbidden)
	wantStatus(t, e.do(t, "DELETE", base+"/"+entries[0].ID, nil), http.StatusNoContent)
	wantStatus(t, e.do(t, "DELETE", base+"/"+entries[0].ID, nil), http.StatusNotFound)
	wantStatus(t, e.do(t, "DELETE", base+"/not-a-uuid", nil), http.StatusNotFound)
	if r := e.do(t, "GET", base, nil); r.body.Meta.Total != 2 {
		t.Errorf("after delete total = %d", r.body.Meta.Total)
	}
	audit := e.do(t, "GET", "/api/admin/audit?limit=20", nil)
	if !strings.Contains(string(audit.body.Data), "action_log_entry_deleted") {
		t.Error("deleting a log entry should be audited")
	}
}
