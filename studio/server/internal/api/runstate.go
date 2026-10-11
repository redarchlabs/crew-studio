package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"crewstudio/internal/model"
	"crewstudio/internal/runner"
	"crewstudio/internal/store"
)

// The run-state API is what a run's state tools (notes_get, log_check, ...) call while the run executes. It
// accepts only the run's own token, which the runner gets on stdin, and every call is limited to that run's
// crew and schedule. Each call adds a readable "state" line to the run's events and journal.

const (
	// Per run: plenty for an agent loop, low enough to stop one that runs away
	runStatePerSecond = 20
	runStateBurst     = 60
	maxAgentRoleLen   = 120
	// maxRunStateWrites bounds what one run (a looping or prompt-injected agent) can add to notes and the log
	maxRunStateWrites = 1000
	// agentRoleHeader is sent by the runner so journal lines name the agent that called
	agentRoleHeader = "X-Crew-Agent"
)

// StateRecorder adds a line to a run's events; the run manager implements it.
type StateRecorder interface {
	RecordStateEvent(ctx context.Context, runID string, payload map[string]any) error
}

// storeStateRecorder is used when no run manager is wired in (tests, tools): it appends after the last event.
type storeStateRecorder struct {
	store  *store.Store
	broker *runner.Broker
}

func (d storeStateRecorder) RecordStateEvent(ctx context.Context, runID string, payload map[string]any) error {
	event := model.RunEvent{Type: runner.StateEventType, CreatedAt: time.Now(), Payload: payload}
	seq, err := d.store.AppendEventNext(ctx, runID, event)
	if err == nil && d.broker != nil {
		event.Seq = seq
		d.broker.Publish(runID, event)
	}
	return err
}

type runStateKey struct{}

// runCall is the verified caller of the run-state API.
type runCall struct {
	scope     store.RunScope
	agentRole string
}

func runCallOf(r *http.Request) runCall {
	c, _ := r.Context().Value(runStateKey{}).(runCall)
	return c
}

func (c runCall) stateScope() store.StateScope {
	return store.StateScope{CrewID: c.scope.CrewID, ScheduleID: c.scope.ScheduleID, DryRun: c.scope.DryRun}
}

func (s *Server) runStateRoutes(r chi.Router) {
	r.Route("/run-state", func(r chi.Router) {
		r.Use(middleware.Timeout(requestTimeout))
		r.Use(s.requireRunToken)
		r.Get("/scope", s.runStateScope)
		r.Get("/control", s.runControlState)
		r.Get("/notes", s.chatTool("notes_list", s.runNotesList))
		r.Get("/notes/{name}", s.chatTool("notes_get", s.runNoteGet))
		r.Put("/notes/{name}", s.refuseChat("notes_put", s.runNotePut))
		r.Get("/log/check", s.chatTool("log_check", s.runLogCheck))
		r.Post("/log", s.refuseChat("log_record", s.runLogRecord))
		r.Post("/proposals", s.runPropose)
		s.trackerRunRoutes(r)
		s.km2RunRoutes(r)
		s.reportRunRoutes(r)
		s.memoryRunRoutes(r)
		s.questionRunRoutes(r)
		s.mcpRunRoutes(r)
	})
}

// requireRunToken lets in only a live run token (Authorization: Bearer csr_...).
func (s *Server) requireRunToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		token = strings.TrimSpace(token)
		if !found || !strings.HasPrefix(token, store.RunTokenPrefix) || len(token) > 200 {
			fail(w, http.StatusUnauthorized, "unauthenticated", "This endpoint is for running crews and needs the run's state token.")
			return
		}
		scope, err := s.store.RunTokenScope(r.Context(), token)
		if errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusUnauthorized, "unauthenticated", "The run token is not valid or has expired (it ends shortly after its run).")
			return
		}
		if err != nil {
			failErr(w, r, s.log, err)
			return
		}
		if !s.runStateCalls.allow(scope.RunID) {
			tooMany(w, "Too many state calls from this run; slow down.")
			return
		}
		// An agent-chat turn or hand-off has no crew: it may only make the calls on its allowlist
		if scope.ChatAgentID != "" && scope.CrewID == "" {
			if !agentChatMayCall(r) {
				fail(w, http.StatusForbidden, "forbidden", "An agent chat turn or hand-off has no crew state; it can only propose and read through its connections.")
				return
			}
		} else if scope.CrewID == "" {
			fail(w, http.StatusConflict, "crew_deleted", "This run's crew has been deleted, so it has no state to read or write.")
			return
		}
		// Without this the run would quietly read and write the crew-wide state instead of its schedule's
		if scope.FromSchedule && scope.ScheduleID == nil {
			fail(w, http.StatusConflict, "schedule_deleted", "The schedule that started this run has been deleted, so its state is gone.")
			return
		}
		call := runCall{scope: scope, agentRole: cleanAgentRole(r.Header.Get(agentRoleHeader))}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), runStateKey{}, call)))
	})
}

// agentChatRoutes are the only run-state calls a crew-less agent-chat turn or hand-off may make
// (docs/agent-chat-handoffs-design.md, section 4): proposing (a hand-off may not propose another hand-off; see
// chat.Propose), asking the person (only a hand-off whose agent allows it; see mayAskPerson), the compaction control
// read, the tracker and KM2 reads, and MCP read tools. Each read still needs its tool in the turn's frozen spec
// (chatTool, or chat.mcp_tools) and a connection the run has. Everything else (notes, the log, writes, memory, reports,
// the scope) is refused.
var agentChatRoutes = func() *chi.Mux {
	m := chi.NewRouter()
	allow := func(http.ResponseWriter, *http.Request) {}
	m.Post("/api/run-state/proposals", allow)
	// Asking the person: a hand-off with questions on may (runAskQuestion refuses everything else); reading its own
	// question back is limited to the run's own questions
	m.Post("/api/run-state/questions", allow)
	m.Get("/api/run-state/questions/{qid}", allow)
	m.Get("/api/run-state/control", allow)
	m.Get("/api/run-state/trackers", allow)
	m.Get("/api/run-state/trackers/projects", allow)
	m.Get("/api/run-state/trackers/issues", allow)
	m.Get("/api/run-state/trackers/issues/{ref}", allow)
	m.Get("/api/run-state/trackers/issues/{ref}/children", allow)
	m.Post("/api/run-state/km2/search", allow)
	m.Post("/api/run-state/km2/ask", allow)
	// MCP read tools (slice 3e): a strict pattern, so only a uuid and a plain tool name can follow; runMCPCall then
	// checks the call against the frozen spec
	m.Post("/api/run-state"+mcpCallPattern, allow)
	return m
}()

// agentChatMayCall reports whether a crew-less agent-chat turn may make this request: an exact route match, as the
// router itself matches routes, never a prefix or suffix. A path with encoded characters (RawPath set) is refused
// outright: the router would route on the encoded form, so the allowlist and the router could disagree.
func agentChatMayCall(r *http.Request) bool {
	return r.URL.RawPath == "" && agentChatRoutes.Match(chi.NewRouteContext(), r.Method, r.URL.Path)
}

// cleanAgentRole reads the calling agent's role, which the runner percent-encodes so any language fits in a header.
func cleanAgentRole(role string) string {
	if decoded, err := url.PathUnescape(role); err == nil {
		role = decoded
	}
	role = strings.Join(strings.FieldsFunc(role, unicode.IsControl), " ")
	role = strings.TrimSpace(role)
	if r := []rune(role); len(r) > maxAgentRoleLen {
		role = string(r[:maxAgentRoleLen])
	}
	return role
}

// withinWriteQuota answers 429 itself when the run has already written its share of state.
func (s *Server) withinWriteQuota(w http.ResponseWriter, r *http.Request, tool string) bool {
	n, err := s.store.RunStateWrites(r.Context(), runCallOf(r).scope.RunID)
	if err != nil {
		failErr(w, r, s.log, err)
		return false
	}
	if n >= maxRunStateWrites {
		s.journal(r, tool, fmt.Sprintf("State: write refused, the run already wrote %d notes and log entries", n), "")
		tooMany(w, fmt.Sprintf("This run has already written %d notes and log entries, the most one run may.", n))
		return false
	}
	return true
}

// journal records one readable line for the call. A failure to record is logged; the call itself stands.
func (s *Server) journal(r *http.Request, tool, message, detail string) {
	call := runCallOf(r)
	payload := map[string]any{"tool": tool, "message": message}
	if detail != "" {
		payload["detail"] = detail
	}
	if call.agentRole != "" {
		payload["agent_role"] = call.agentRole
	}
	// A fresh context: the line must be written even if the caller has gone
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if err := s.stateEvents.RecordStateEvent(ctx, call.scope.RunID, payload); err != nil {
		s.log.Warn("record run-state call", "run_id", call.scope.RunID, "tool", tool, "err", err)
	}
}

// safeName is a note name as sent, made safe for a journal line even when it was refused.
func safeName(name string) string {
	return cleanAgentRole(name)
}

func subjectDetail(subject string) string {
	if subject == "" {
		return ""
	}
	return "Subject: " + subject
}

// problemText is the first validation problem, or the error, for a journal line.
func problemText(err error) string {
	var verr *model.ValidationError
	if errors.As(err, &verr) && len(verr.Details) > 0 {
		return verr.Details[0]
	}
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		return conflict.Message
	}
	if errors.Is(err, store.ErrNotFound) {
		return "not found"
	}
	return "server error"
}

func (s *Server) runStateScope(w http.ResponseWriter, r *http.Request) {
	call := runCallOf(r)
	where := "crew-wide"
	if call.scope.ScheduleID != nil {
		where = "schedule " + call.scope.ScheduleName
	}
	s.journal(r, "scope", fmt.Sprintf("State: read the run's scope (%s, %d connections)", where, len(call.scope.Connections)), "")
	ok(w, call.scope)
}

func (s *Server) runNotesList(w http.ResponseWriter, r *http.Request) {
	call := runCallOf(r)
	f := store.NoteFilter{Scope: call.stateScope()}
	var err error
	if r.URL.Query().Has("subject") {
		var subject string
		if subject, err = model.NormalizeSubject(r.URL.Query().Get("subject")); err == nil {
			f.Subject = &subject
		}
	}
	var notes []model.NoteInfo
	if err == nil {
		notes, err = s.store.ListNotes(r.Context(), f)
	}
	if err != nil {
		s.journal(r, "notes_list", "Notes: list failed: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	detail := ""
	if f.Subject != nil {
		detail = subjectDetail(*f.Subject)
	}
	s.journal(r, "notes_list", fmt.Sprintf("Notes: listed %d", len(notes)), detail)
	ok(w, notes)
}

// runNoteKey reads {name} and ?subject= into a key in the run's scope.
func runNoteKey(r *http.Request, subject string) (store.NoteKey, error) {
	name := chi.URLParam(r, "name")
	if !model.ValidNoteName(name) {
		return store.NoteKey{}, invalid("name: use letters, digits, '.', '_', ':' or '-' (at most 100)")
	}
	clean, err := model.NormalizeSubject(subject)
	return store.NoteKey{StateScope: runCallOf(r).stateScope(), Subject: clean, Name: name}, err
}

func (s *Server) runNoteGet(w http.ResponseWriter, r *http.Request) {
	key, err := runNoteKey(r, r.URL.Query().Get("subject"))
	var note model.NoteVersion
	if err == nil {
		note, err = s.store.LatestNote(r.Context(), key)
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.journal(r, "notes_get", "Notes: "+key.Name+" not found", subjectDetail(key.Subject))
		fail(w, http.StatusNotFound, "not_found", fmt.Sprintf("There is no note named %q yet.", key.Name))
	case err != nil:
		s.journal(r, "notes_get", "Notes: read failed: "+problemText(err), "")
		failErr(w, r, s.log, err)
	default:
		s.journal(r, "notes_get", fmt.Sprintf("Notes: read %s v%d", note.Name, note.Version), subjectDetail(note.Subject))
		ok(w, note)
	}
}

func (s *Server) runNotePut(w http.ResponseWriter, r *http.Request) {
	var in model.NotePut
	if !decode(w, r, &in) || !s.withinWriteQuota(w, r, "notes_put") {
		return
	}
	in.Name = chi.URLParam(r, "name")
	clean, err := model.NormalizeNotePut(in)
	var key store.NoteKey
	if err == nil {
		key, err = runNoteKey(r, clean.Subject)
	}
	var note model.NoteVersion
	if err == nil {
		runID := runCallOf(r).scope.RunID
		note, err = s.store.PutNote(r.Context(), key, clean.Content, clean.ExpectedVersion, store.NoteWriter{RunID: &runID, Name: "run"})
	}
	if err != nil {
		s.journal(r, "notes_put", "Notes: write to "+safeName(in.Name)+" refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	s.journal(r, "notes_put", fmt.Sprintf("Notes: wrote %s v%d (%d bytes)", note.Name, note.Version, len(note.Content)), subjectDetail(note.Subject))
	ok(w, note)
}

type logCheckResponse struct {
	// Entry is the latest matching entry, or null when there is none
	Entry *model.ActionEntry `json:"entry"`
}

func (s *Server) runLogCheck(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	subject, err := model.NormalizeSubject(q.Get("subject"))
	action := strings.TrimSpace(q.Get("action"))
	if err == nil && (subject == "" || !model.ValidAction(action)) {
		err = invalid("subject and action are required, such as subject=jira:PRJ-15&action=followup:due_soon")
	}
	var within time.Duration
	if err == nil {
		if within, err = model.ParseWithin(q.Get("within")); err != nil {
			err = invalid(err.Error())
		}
	}
	var entry *model.ActionEntry
	if err == nil {
		var since *time.Time
		if within > 0 {
			t := time.Now().Add(-within)
			since = &t
		}
		entry, err = s.store.CheckAction(r.Context(), runCallOf(r).stateScope(), subject, action, since)
	}
	if err != nil {
		s.journal(r, "log_check", "Log: check failed: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	found := "none"
	if entry != nil {
		found = "last at " + entry.At.UTC().Format("2006-01-02 15:04 UTC")
	}
	if within > 0 {
		found += " (within " + q.Get("within") + ")"
	}
	s.journal(r, "log_check", fmt.Sprintf("Log: checked %s for %s: %s", action, subject, found), "")
	ok(w, logCheckResponse{Entry: entry})
}

type logRecordResponse struct {
	Entry model.ActionEntry `json:"entry"`
	// Duplicate is true when the idempotency key was already recorded; nothing new was added
	Duplicate bool `json:"duplicate"`
}

func (s *Server) runLogRecord(w http.ResponseWriter, r *http.Request) {
	var in model.LogRecord
	if !decode(w, r, &in) || !s.withinWriteQuota(w, r, "log_record") {
		return
	}
	clean, err := model.NormalizeLogRecord(in)
	if err == nil {
		err = model.CheckAgentLogRecord(clean)
	}
	var entry model.ActionEntry
	var duplicate bool
	if err == nil {
		runID := runCallOf(r).scope.RunID
		entry, duplicate, err = s.store.RecordAction(r.Context(), runCallOf(r).stateScope(), &runID, clean)
	}
	if err != nil {
		s.journal(r, "log_record", "Log: record refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	if duplicate {
		s.journal(r, "log_record", fmt.Sprintf("Log: %s for %s was already recorded (same idempotency key)", entry.Action, entry.Subject), "")
		ok(w, logRecordResponse{Entry: entry, Duplicate: true})
		return
	}
	dry := ""
	if entry.DryRun {
		dry = " (dry run)"
	}
	s.journal(r, "log_record", fmt.Sprintf("Log: recorded %s for %s%s", entry.Action, entry.Subject, dry), "")
	created(w, logRecordResponse{Entry: entry})
}
