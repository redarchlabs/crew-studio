package api

import (
	"net/http"
	"slices"
	"strings"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

var (
	contentRevisionTypes = []string{"llm", "pricing", "agent", "task", "crew", "settings", "work_order", "schedule", "trigger"}
	adminRevisionTypes   = []string{"area", "role", "user", "api_key", "access_settings"}
)

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	o, err := s.store.Overview(r.Context(), scopeOf(principal(r)))
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, o)
}

func (s *Server) listTools(w http.ResponseWriter, r *http.Request) {
	tools, err := s.tools.Tools()
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, tools)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context())
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, st)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in model.Settings
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeSettings(in)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	out, err := s.store.UpdateSettings(r.Context(), clean)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	// A higher concurrency limit may let queued runs start now
	s.runs.Notify()
	ok(w, out)
}

// listHistory shows content revisions (history.view, limited to your areas) and admin revisions (admin.audit).
func (s *Server) listHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := principal(r)
	f := store.RevisionFilter{EntityType: q.Get("entity_type"), EntityID: q.Get("entity_id"), Scope: scopeOf(p)}
	if p.Can(auth.HistoryView) {
		f.Types = append(f.Types, contentRevisionTypes...)
	}
	if p.Can(auth.AdminAudit) {
		f.Types = append(f.Types, adminRevisionTypes...)
	}
	if f.EntityType != "" && !slices.Contains(f.Types, f.EntityType) {
		fail(w, http.StatusUnprocessableEntity, "validation_failed", "Unknown entity type.",
			"entity_type: must be one of "+strings.Join(f.Types, ", "))
		return
	}
	if len(f.EntityID) > 64 {
		fail(w, http.StatusUnprocessableEntity, "validation_failed", "entity_id is too long.")
		return
	}
	f.Limit, f.Offset = pageParams(r)
	revs, total, err := s.store.ListRevisions(r.Context(), f)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	page(w, revs, model.Page{Total: total, Limit: f.Limit, Offset: f.Offset})
}
