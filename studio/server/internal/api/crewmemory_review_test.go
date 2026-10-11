package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// The settings view says how many memories still wait to be re-embedded for the current embedding model.
func TestMemorySettingsReembedBacklog(t *testing.T) {
	e := newMemoryEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	base := "/api/crews/" + crew.ID + "/memory/settings"
	if view := decodeData[memorySettingsView](t, e.do(t, "GET", base, nil)); view.ReembedPending != 0 {
		t.Errorf("empty backlog = %d", view.ReembedPending)
	}
	wantStatus(t, e.do(t, "POST", "/api/crews/"+crew.ID+"/memory", map[string]any{"content": "current model"}), http.StatusCreated)
	if _, err := e.st.Pool().Exec(context.Background(), `INSERT INTO memories (crew_id, scope, content, content_hash, embed_model, vector, written_by)
		VALUES ($1, '/', 'old model', 'h1', 'other:3', '{1,2,3}', 't'), ($1, '/', 'older model', 'h2', 'other:2', '{1,2}', 't')`, crew.ID); err != nil {
		t.Fatal(err)
	}
	if view := decodeData[memorySettingsView](t, e.do(t, "GET", base, nil)); view.ReembedPending != 2 {
		t.Errorf("backlog = %d, want 2", view.ReembedPending)
	}
}

// Pending memories cannot be promoted to KM2 until a person approves them.
func TestPromoteRefusesPendingMemories(t *testing.T) {
	e := newMemoryEnv(t)
	crew := e.crewByName(t, "PE Due Diligence Crew")
	conn := e.km2Connection(t, "KM2 Finance")
	if err := e.st.SetCrewConnections(context.Background(), crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	base := "/api/crews/" + crew.ID + "/memory"
	active := decodeData[model.MemoryRecord](t, e.do(t, "POST", base, map[string]any{"content": "Sam replies late on Fridays"}))
	pending := saveOne(t, e, e.reviewedRun(t, model.MemoryExplicit, 5), "PRJ-15 dates slipped", "/projects").Saved[0].ID
	r := e.do(t, "POST", base+"/promote", map[string]any{"memory_ids": []string{active.ID, pending}, "title": "Lessons"})
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if d := strings.Join(r.body.Error.Details, ";"); d != "memory_ids: approve these memories first" {
		t.Errorf("details = %s", d)
	}
	if len(e.data.Saved()) != 0 {
		t.Error("a pending memory reached KM2")
	}
	wantStatus(t, e.do(t, "PATCH", base+"/"+pending, map[string]any{"status": "active"}), http.StatusOK)
	wantStatus(t, e.do(t, "POST", base+"/promote", map[string]any{"memory_ids": []string{active.ID, pending}, "title": "Lessons"}), http.StatusOK)
}
