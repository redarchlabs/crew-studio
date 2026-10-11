package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"crewstudio/internal/km2/km2test"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

func TestMemorySettingsAPI(t *testing.T) {
	e := newMemoryEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	base := "/api/crews/" + crew.ID + "/memory/settings"
	view := decodeData[memorySettingsView](t, e.do(t, "GET", base, nil))
	if view.Settings.Mode != model.MemoryOff || !view.Available || view.EmbedModel != "letters:26" || view.SuggestedLLMID == nil ||
		*view.Settings.RetentionDays != 365 || !view.Settings.ReviewRunSaves {
		t.Fatalf("view = %+v", view)
	}
	in := view.Settings
	in.Mode, in.RecallLimit, in.RetentionDays = model.MemoryAutomatic, 8, nil
	r := e.do(t, "PUT", base, in)
	wantStatus(t, r, http.StatusOK)
	saved := decodeData[memorySettingsView](t, r)
	if saved.Settings.Mode != model.MemoryAutomatic || saved.Settings.RecallLimit != 8 || saved.Settings.RetentionDays != nil ||
		saved.Settings.UpdatedByName != "Ada Admin" || !saved.Available {
		t.Errorf("saved = %+v", saved)
	}

	bad := in
	bad.RecallLimit = 50
	wantStatus(t, e.do(t, "PUT", base, bad), http.StatusUnprocessableEntity)
	llm, err := e.st.CreateLLM(context.Background(), model.LLM{Name: "No price", Model: "openai/unpriced-thing"})
	if err != nil {
		t.Fatal(err)
	}
	bad = in
	bad.LLMID = &llm.ID
	r = e.do(t, "PUT", base, bad)
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if !strings.Contains(r.body.Error.Details[0], "no price") {
		t.Errorf("details = %v", r.body.Error.Details)
	}
	bad.LLMID = ptrTo(missingID)
	wantStatus(t, e.do(t, "PUT", base, bad), http.StatusUnprocessableEntity)

	// Without an embedder memory can only be turned off
	e.emb.configured = false
	wantCode(t, e.do(t, "PUT", base, in), http.StatusConflict, "memory_unavailable")
	in.Mode = model.MemoryOff
	if got := decodeData[memorySettingsView](t, e.do(t, "PUT", base, in)); got.Available || got.UnavailableReason != "needs a key" || got.Settings.Mode != model.MemoryOff {
		t.Errorf("off = %+v", got)
	}

	// Viewers read, builders without crews.edit... cannot change; other areas see nothing
	_, viewer := e.person(t, "viewer@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(viewer).do(t, "GET", base, nil), http.StatusOK)
	wantStatus(t, e.as(viewer).do(t, "PUT", base, in), http.StatusForbidden)
	_, outsider := e.person(t, "it@example.com", []string{builderRole}, []string{itArea}, nil)
	wantStatus(t, e.as(outsider).do(t, "GET", base, nil), http.StatusNotFound)
}

func ptrTo[T any](v T) *T { return &v }

func TestMemoryTabAPI(t *testing.T) {
	e := newMemoryEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	base := "/api/crews/" + crew.ID + "/memory"

	r := e.do(t, "POST", base, map[string]any{"content": "Sam usually replies late on Fridays", "scope": "/People/Sam", "categories": []string{"people"}, "pinned": true})
	wantStatus(t, r, http.StatusCreated)
	sam := decodeData[model.MemoryRecord](t, r)
	if sam.Status != model.MemoryActive || sam.Source != "user:Ada Admin" || sam.WrittenBy != "Ada Admin" || !sam.Pinned || sam.Scope != "/people/sam" || sam.RunID != nil {
		t.Fatalf("created = %+v", sam)
	}
	wantCode(t, e.do(t, "POST", base, map[string]any{"content": "sam usually replies late on fridays", "scope": "/people/sam"}), http.StatusConflict, "memory_exists")
	r = e.do(t, "POST", base, map[string]any{"content": "", "scope": "/../x"})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if d := strings.Join(r.body.Error.Details, ";"); !strings.Contains(d, "content: is required") || !strings.Contains(d, "scope:") {
		t.Errorf("details = %s", d)
	}
	vendor := decodeData[model.MemoryRecord](t, e.do(t, "POST", base, map[string]any{"content": "The vendor status page lies about outages", "importance": 0.3}))

	// A run's pending memory waits for review
	auto := e.reviewedRun(t, model.MemoryAutomatic, 10)
	r = e.call(t, auto, "POST", "/memory/records", map[string]any{"records": []map[string]any{{"content": "PRJ-15 dates slipped three times", "scope": "/projects"}}})
	pendingID := decodeData[saveAnswer](t, r).Saved[0].ID

	page := func(query string) ([]model.MemoryRecord, int) {
		t.Helper()
		r := e.do(t, "GET", base+query, nil)
		wantStatus(t, r, http.StatusOK)
		return decodeData[[]model.MemoryRecord](t, r), r.body.Meta.Total
	}
	if items, total := page(""); total != 3 || len(items) != 3 || items[0].ID != pendingID {
		t.Errorf("all = %d %+v", total, items)
	}
	if items, total := page("?status=pending"); total != 1 || items[0].ID != pendingID {
		t.Errorf("pending = %d", total)
	}
	if _, total := page("?pinned=true"); total != 1 {
		t.Errorf("pinned = %d", total)
	}
	if _, total := page("?pinned=false&scope=/people"); total != 0 {
		t.Errorf("unpinned people = %d", total)
	}
	if items, total := page("?run_id=" + auto.run.ID); total != 1 || items[0].ID != pendingID {
		t.Errorf("by run = %d", total)
	}
	// Search by meaning, keeping text matches
	if items, total := page("?q=" + "vendor+status+page"); total < 1 || items[0].ID != vendor.ID {
		t.Errorf("semantic = %d %+v", total, items)
	}
	if items, _ := page("?q=PRJ-15&limit=1"); len(items) != 1 || items[0].ID != pendingID {
		t.Errorf("text match = %+v", items)
	}
	e.emb.configured = false
	if items, total := page("?q=fridays"); total != 1 || items[0].ID != sam.ID {
		t.Errorf("fallback = %d %+v", total, items)
	}
	e.emb.fail, e.emb.configured = true, true
	if _, total := page("?q=vendor"); total != 1 {
		t.Errorf("provider failure fallback = %d", total)
	}
	e.emb.fail = false
	for _, q := range []string{"?status=gone", "?pinned=maybe", "?run_id=nope", "?scope=/../x"} {
		wantStatus(t, e.do(t, "GET", base+q, nil), http.StatusUnprocessableEntity)
	}
	scopes := decodeData[[]model.MemoryScopeCount](t, e.do(t, "GET", base+"/scopes", nil))
	if len(scopes) != 3 || scopes[0].Scope != "/" {
		t.Errorf("scopes = %+v", scopes)
	}

	// Approve, edit, and the checks on edits
	approved := decodeData[model.MemoryRecord](t, e.do(t, "PATCH", base+"/"+pendingID, map[string]any{"status": "active"}))
	if approved.Status != model.MemoryActive {
		t.Errorf("approved = %+v", approved)
	}
	wantStatus(t, e.do(t, "PATCH", base+"/"+pendingID, map[string]any{"status": "pending"}), http.StatusUnprocessableEntity)
	calls := e.emb.calls
	edited := decodeData[model.MemoryRecord](t, e.do(t, "PATCH", base+"/"+vendor.ID, map[string]any{"content": "The vendor status page under-reports outages",
		"scope": "/vendors", "categories": []string{"vendors"}, "importance": 0.6, "pinned": false}))
	if edited.Scope != "/vendors" || edited.Importance != 0.6 || e.emb.calls != calls+1 {
		t.Errorf("edited = %+v", edited)
	}
	// Saving the same content does not embed again
	_ = decodeData[model.MemoryRecord](t, e.do(t, "PATCH", base+"/"+vendor.ID, map[string]any{"content": edited.Content, "pinned": true}))
	if e.emb.calls != calls+1 {
		t.Error("unchanged content was embedded again")
	}
	wantCode(t, e.do(t, "PATCH", base+"/"+vendor.ID, map[string]any{"content": sam.Content, "scope": "/people/sam"}), http.StatusConflict, "memory_exists")
	e.emb.configured = false
	wantCode(t, e.do(t, "PATCH", base+"/"+vendor.ID, map[string]any{"content": "new text"}), http.StatusConflict, "memory_unavailable")
	wantCode(t, e.do(t, "POST", base, map[string]any{"content": "new text"}), http.StatusConflict, "memory_unavailable")
	e.emb.configured = true
	wantStatus(t, e.do(t, "PATCH", base+"/"+missingID, map[string]any{}), http.StatusNotFound)
	wantStatus(t, e.do(t, "PATCH", base+"/nope", map[string]any{}), http.StatusNotFound)

	// Permissions and areas
	_, viewer := e.person(t, "viewer@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(viewer).do(t, "GET", base, nil), http.StatusOK)
	wantStatus(t, e.as(viewer).do(t, "POST", base, map[string]any{"content": "x"}), http.StatusForbidden)
	wantStatus(t, e.as(viewer).do(t, "DELETE", base+"/"+sam.ID, nil), http.StatusForbidden)
	_, outsider := e.person(t, "it@example.com", []string{builderRole}, []string{itArea}, nil)
	wantStatus(t, e.as(outsider).do(t, "GET", base, nil), http.StatusNotFound)
	wantStatus(t, e.as(outsider).do(t, "PATCH", base+"/"+sam.ID, map[string]any{"pinned": false}), http.StatusNotFound)

	// Bulk delete: preview, then delete (pinned included); one at a time
	if got := decodeData[memoriesDeleteResponse](t, e.do(t, "POST", base+"/delete", map[string]any{"contains": "sam", "dry_run": true})); got.Matched != 1 || got.Deleted != 0 {
		t.Errorf("dry run = %+v", got)
	}
	if got := decodeData[memoriesDeleteResponse](t, e.do(t, "POST", base+"/delete", map[string]any{"scope_prefix": "/people", "contains": "SAM"})); got.Deleted != 1 {
		t.Errorf("delete = %+v", got)
	}
	wantStatus(t, e.do(t, "POST", base+"/delete", map[string]any{"dry_run": true}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", base+"/delete", map[string]any{"ids": []string{"x"}}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "POST", base+"/delete", map[string]any{"scope_prefix": "/.."}), http.StatusUnprocessableEntity)
	wantStatus(t, e.do(t, "DELETE", base+"/"+pendingID, nil), http.StatusNoContent)
	wantStatus(t, e.do(t, "DELETE", base+"/"+pendingID, nil), http.StatusNotFound)
	if got := decodeData[memoriesDeleteResponse](t, e.do(t, "POST", base+"/delete", map[string]any{"ids": []string{vendor.ID}})); got.Deleted != 1 {
		t.Errorf("by id = %+v", got)
	}

	// Audit entries name ids and scopes, never the text
	var details string
	if err := e.st.Pool().QueryRow(context.Background(), `SELECT string_agg(action || ' ' || details::text, E'\n') FROM audit_events WHERE action LIKE 'memor%'`).Scan(&details); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"memory_created", "memory_updated", "memory_deleted", "memories_deleted"} {
		if !strings.Contains(details, action) {
			t.Errorf("audit lacks %s", action)
		}
	}
	if strings.Contains(details, "Fridays") || strings.Contains(details, "vendor status") {
		t.Errorf("audit holds memory text:\n%s", details)
	}
}

func TestPromoteMemoriesToKM2(t *testing.T) {
	e := newMemoryEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	base := "/api/crews/" + crew.ID + "/memory"
	a := decodeData[model.MemoryRecord](t, e.do(t, "POST", base, map[string]any{"content": "Sam replies late on Fridays", "scope": "/people/sam"}))
	b := decodeData[model.MemoryRecord](t, e.do(t, "POST", base, map[string]any{"content": "Vendor page lies"}))
	promote := map[string]any{"memory_ids": []string{a.ID, b.ID}, "title": "Lessons about Sam"}

	wantCode(t, e.do(t, "POST", base+"/promote", promote), http.StatusConflict, "no_km2")
	conn := e.km2Connection(t, "KM2 Finance")
	if err := e.st.SetCrewConnections(context.Background(), crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	for field, body := range map[string]map[string]any{
		"memory_ids":   {"memory_ids": []string{}, "title": "x"},
		"memory_ids:":  {"memory_ids": []string{"nope"}, "title": "x"},
		"memory_ids: ": {"memory_ids": []string{missingID}, "title": "x"},
		"title":        {"memory_ids": []string{a.ID}, "title": ""},
		"external_ref": {"memory_ids": []string{a.ID}, "title": "x", "external_ref": "bad ref!"},
	} {
		r := e.do(t, "POST", base+"/promote", body)
		wantStatus(t, r, http.StatusUnprocessableEntity)
		if !strings.HasPrefix(r.body.Error.Details[0], strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(field), ":"))+":") {
			t.Errorf("%s: details = %v", field, r.body.Error.Details)
		}
	}
	r := e.do(t, "POST", base+"/promote", promote)
	wantStatus(t, r, http.StatusOK)
	res := decodeData[memoryPromoteResponse](t, r)
	if res.Outcome != "created" || res.DocumentID == "" || res.FolderID != km2test.FolderAtlas || !strings.HasPrefix(res.ExternalRef, "crew-memory-"+crew.ID[:8]) {
		t.Fatalf("promote = %+v", res)
	}
	saved := e.data.Saved()
	if len(saved) != 1 || saved[0][2] != "Lessons about Sam" || !strings.Contains(saved[0][3], "- Sam replies late on Fridays") || !strings.Contains(saved[0][3], "scope /people/sam") {
		t.Errorf("saved = %+v", saved)
	}
	if got := decodeData[model.MemoryRecord](t, e.do(t, "PATCH", base+"/"+a.ID, map[string]any{})); got.KM2DocumentID == nil || *got.KM2DocumentID != res.DocumentID {
		t.Errorf("memory not linked: %+v", got.KM2DocumentID)
	}
	// The same promotion again is not sent twice
	if again := decodeData[memoryPromoteResponse](t, e.do(t, "POST", base+"/promote", promote)); again.Outcome != "already_saved" || again.DocumentID != res.DocumentID {
		t.Errorf("again = %+v", again)
	}
	// The log entry has no run and counts against the person's daily cap
	var entries int
	if err := e.st.Pool().QueryRow(context.Background(), `SELECT count(*) FROM action_log WHERE action = 'km2:save' AND run_id IS NULL
		AND details->>'promoted_by' = $1 AND details->>'tool' = 'memory_promote'`, e.adminID).Scan(&entries); err != nil || entries != 1 {
		t.Errorf("entries = %d %v", entries, err)
	}
	if _, err := e.st.Pool().Exec(context.Background(), `INSERT INTO action_log (crew_id, subject, action, details)
		SELECT $1, 'km2:x' || g, 'km2:save', jsonb_build_object('promoted_by', $2::text) FROM generate_series(1, $3) g`, crew.ID, e.adminID, maxPromotionsPerPersonDay); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.do(t, "POST", base+"/promote", map[string]any{"memory_ids": []string{b.ID}, "title": "Another"}), http.StatusTooManyRequests, "km2_promote_cap")

	// A connection outside the person's areas cannot be used, and two are ambiguous
	_, builder := e.person(t, "hr@example.com", []string{builderRole}, []string{financeArea}, nil)
	if _, err := e.st.Pool().Exec(context.Background(), `UPDATE connections SET area_ids = $2 WHERE id = $1`, conn.ID, []string{hrArea}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetCrewConnections(context.Background(), crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.as(builder).do(t, "POST", base+"/promote", promote), http.StatusConflict, "no_km2")
	second := e.km2Connection(t, "KM2 Second")
	if err := e.st.SetCrewConnections(context.Background(), crew.ID, []string{conn.ID, second.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.do(t, "POST", base+"/promote", promote), http.StatusConflict, "km2_ambiguous")
}
