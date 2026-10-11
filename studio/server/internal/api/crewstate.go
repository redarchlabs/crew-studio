package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// A crew's state as people see it on the crew's State tab: notes with their versions, and the action log.
// Everything is limited to crews in the caller's areas.

// noScheduleParam picks crew-wide state (no schedule) in a schedule_id filter.
const noScheduleParam = "none"

// crewStateRoutes hangs off /crews (see mountLibrary).
func (s *Server) crewStateRoutes(r chi.Router) {
	r.Route("/{id}/state", func(r chi.Router) {
		view, manage := s.need(auth.StateView), s.need(auth.StateManage)
		r.With(view).Get("/notes", s.listStateNotes)
		r.With(view).Get("/notes/versions", s.listNoteVersions)
		r.With(manage).Put("/notes", s.putStateNote)
		r.With(manage).Post("/notes/rollback", s.rollbackStateNote)
		r.With(manage).Delete("/notes", s.deleteStateNote)
		r.With(view).Get("/log", s.listStateLog)
		r.With(manage).Delete("/log/{entryId}", s.deleteStateLogEntry)
		r.With(view).Get("/watches", s.listCrewWatches)
		s.trackingRoutes(r)
	})
	s.crewMemoryRoutes(r)
	r.With(s.need(auth.ConnectionsView)).Get("/{id}/connections", s.listCrewConnections)
	r.With(s.need(auth.CrewsEdit, auth.ConnectionsView)).Put("/{id}/connections", s.setCrewConnections)
}

// loadStateCrew reads the crew in {id}, answering 404 itself when it is missing or outside the caller's areas.
func (s *Server) loadStateCrew(w http.ResponseWriter, r *http.Request) (model.Crew, bool) {
	id, valid := pathID(w, r)
	if !valid {
		return model.Crew{}, false
	}
	crew, err := s.store.GetCrew(r.Context(), id)
	if err == nil && !principal(r).SeesAreas(crew.AreaIDs) {
		err = store.ErrNotFound
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return crew, false
	}
	return crew, true
}

// scheduleScope turns a schedule_id value into a scope. "" or "none" is crew-wide; a UUID must be one of the
// crew's schedules.
func (s *Server) scheduleScope(ctx context.Context, crewID, scheduleID string) (store.StateScope, error) {
	scope := store.StateScope{CrewID: crewID}
	if scheduleID == "" || scheduleID == noScheduleParam {
		return scope, nil
	}
	if !uuidPattern.MatchString(scheduleID) {
		return scope, invalid(`schedule_id: must be a schedule's id or "none"`)
	}
	owned, err := s.store.ScheduleOfCrew(ctx, scheduleID, crewID)
	if err != nil {
		return scope, err
	}
	if !owned {
		return scope, invalid("schedule_id: is not one of this crew's schedules")
	}
	scope.ScheduleID = &scheduleID
	return scope, nil
}

// noteKey reads schedule_id, subject and name, which together identify one note of the crew.
func (s *Server) noteKey(ctx context.Context, crewID, scheduleID, subject, name string) (store.NoteKey, error) {
	scope, err := s.scheduleScope(ctx, crewID, scheduleID)
	if err != nil {
		return store.NoteKey{}, err
	}
	cleanSubject, err := model.NormalizeSubject(subject)
	if err != nil {
		return store.NoteKey{}, err
	}
	name = strings.TrimSpace(name)
	if !model.ValidNoteName(name) {
		return store.NoteKey{}, invalid("name: use letters, digits, '.', '_', ':' or '-' (at most 100)")
	}
	return store.NoteKey{StateScope: scope, Subject: cleanSubject, Name: name}, nil
}

func (s *Server) noteKeyFromQuery(r *http.Request, crewID string) (store.NoteKey, error) {
	q := r.URL.Query()
	return s.noteKey(r.Context(), crewID, q.Get("schedule_id"), q.Get("subject"), q.Get("name"))
}

func (s *Server) listStateNotes(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	q := r.URL.Query()
	f := store.NoteFilter{AllSchedules: q.Get("schedule_id") == ""}
	var err error
	f.Scope, err = s.scheduleScope(r.Context(), crew.ID, q.Get("schedule_id"))
	if err == nil && q.Has("subject") {
		var subject string
		if subject, err = model.NormalizeSubject(q.Get("subject")); err == nil {
			f.Subject = &subject
		}
	}
	var notes []model.NoteInfo
	if err == nil {
		notes, err = s.store.ListNotes(r.Context(), f)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, notes)
}

func (s *Server) listNoteVersions(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	key, err := s.noteKeyFromQuery(r, crew.ID)
	var versions []model.NoteVersion
	if err == nil {
		versions, err = s.store.NoteVersions(r.Context(), key)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, versions)
}

// stateNoteRequest edits a note by hand (PUT) or rolls it back to an earlier version (POST .../rollback).
type stateNoteRequest struct {
	ScheduleID *string `json:"schedule_id"`
	Subject    string  `json:"subject"`
	Name       string  `json:"name"`
	Content    string  `json:"content"`
	// Version is the version to roll back to
	Version         int  `json:"version"`
	ExpectedVersion *int `json:"expected_version"`
}

func (req stateNoteRequest) schedule() string {
	if req.ScheduleID == nil {
		return ""
	}
	return *req.ScheduleID
}

func (s *Server) putStateNote(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	var req stateNoteRequest
	if !decode(w, r, &req) {
		return
	}
	clean, err := model.NormalizeNotePut(model.NotePut{Name: req.Name, Subject: req.Subject, Content: req.Content, ExpectedVersion: req.ExpectedVersion})
	var key store.NoteKey
	if err == nil {
		key, err = s.noteKey(r.Context(), crew.ID, req.schedule(), clean.Subject, clean.Name)
	}
	var out model.NoteVersion
	if err == nil {
		out, err = s.store.PutNote(r.Context(), key, clean.Content, clean.ExpectedVersion, store.NoteWriter{Name: principal(r).Name})
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditNote(r, "note_written", crew, out, nil)
	ok(w, out)
}

// rollbackStateNote saves an earlier version's content as the newest version, so nothing is lost.
func (s *Server) rollbackStateNote(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	var req stateNoteRequest
	if !decode(w, r, &req) {
		return
	}
	key, err := s.noteKey(r.Context(), crew.ID, req.schedule(), req.Subject, req.Name)
	var old, out model.NoteVersion
	if err == nil {
		old, err = s.store.NoteVersion(r.Context(), key, req.Version)
	}
	if err == nil {
		out, err = s.store.PutNote(r.Context(), key, old.Content, req.ExpectedVersion,
			store.NoteWriter{Name: principal(r).Name + " (rolled back to v" + strconv.Itoa(old.Version) + ")"})
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditNote(r, "note_rolled_back", crew, out, map[string]any{"restored_version": old.Version})
	ok(w, out)
}

// auditNote records a person's change to a note: which note and version, never its content.
func (s *Server) auditNote(r *http.Request, action string, crew model.Crew, n model.NoteVersion, extra map[string]any) {
	p := principal(r)
	details := map[string]any{"crew_id": crew.ID, "crew_name": crew.Name, "schedule_id": n.ScheduleID,
		"subject": n.Subject, "name": n.Name, "version": n.Version}
	for k, v := range extra {
		details[k] = v
	}
	s.audit(r, action, p.UserID, p.Name, details)
}

func (s *Server) deleteStateNote(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	key, err := s.noteKeyFromQuery(r, crew.ID)
	if err == nil {
		err = s.store.DeleteNote(r.Context(), key)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	p := principal(r)
	s.audit(r, "note_deleted", p.UserID, p.Name, map[string]any{"crew_id": crew.ID, "crew_name": crew.Name,
		"schedule_id": key.ScheduleID, "subject": key.Subject, "name": key.Name})
	noContent(w)
}

func (s *Server) listStateLog(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	q := r.URL.Query()
	f := store.ActionFilter{AllSchedules: q.Get("schedule_id") == "", Subject: strings.TrimSpace(q.Get("subject")),
		Action: strings.TrimSpace(q.Get("action"))}
	var err error
	f.Scope, err = s.scheduleScope(r.Context(), crew.ID, q.Get("schedule_id"))
	if err == nil && (len(f.Subject) > 200 || len(f.Action) > 100) {
		err = invalid("subject or action filter is too long")
	}
	f.Limit, f.Offset = pageParams(r)
	var entries []model.ActionEntry
	var total int
	if err == nil {
		entries, total, err = s.store.ListActions(r.Context(), f)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	page(w, entries, model.Page{Total: total, Limit: f.Limit, Offset: f.Offset})
}

func (s *Server) deleteStateLogEntry(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	entryID, valid := pathUUID(w, r, "entryId")
	if !valid {
		return
	}
	entry, err := s.store.GetAction(r.Context(), crew.ID, entryID)
	if err == nil {
		err = s.store.DeleteAction(r.Context(), crew.ID, entryID)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	p := principal(r)
	s.audit(r, "action_log_entry_deleted", p.UserID, p.Name, map[string]any{"crew_id": crew.ID, "crew_name": crew.Name,
		"entry_id": entry.ID, "subject": entry.Subject, "action": entry.Action, "at": entry.At})
	noContent(w)
}
