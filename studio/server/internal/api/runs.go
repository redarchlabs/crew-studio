package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/interactions"
	"crewstudio/internal/journal"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
)

const (
	streamPingInterval = 15 * time.Second
	streamRetryMS      = 3000
)

func (s *Server) runRoutes(r chi.Router) {
	r.Route("/runs", func(r chi.Router) {
		r.Use(s.needAny(auth.RunsView, auth.WorkOrdersView))
		r.With(s.need(auth.RunsView)).Get("/", s.listRuns)
		r.Get("/{id}", s.getRun)
		r.With(s.need(auth.RunsDelete)).Delete("/{id}", s.deleteRun)
		r.With(s.needAny(auth.RunsCancel, auth.WorkOrdersCancel)).Post("/{id}/cancel", s.cancelRun)
		r.With(s.needAny(auth.RunsCancel, auth.WorkOrdersCancel)).Post("/{id}/compact", s.compactRun)
		r.Get("/{id}/events", s.listRunEvents)
		r.Get("/{id}/interactions", s.runInteractions)
		r.Get("/{id}/llm-calls", s.runLLMCalls)
		r.Get("/{id}/artifacts", s.runArtifacts)
		r.Get("/{id}/deliveries", s.runDeliveries)
		r.Get("/{id}/journal", s.runJournal)
	})
}

// canViewRun: runs.view covers every run in your areas; work_orders.view covers work order attempts. A chat turn or
// hand-off holds a person's private conversation, so only that person sees it.
func canViewRun(p *auth.Principal, run model.Run) bool {
	if !p.SeesAreas(run.AreaIDs) {
		return false
	}
	if model.IsChatKind(run.Kind) && (p == nil || run.CreatedBy == nil || *run.CreatedBy != p.UserID) {
		return false
	}
	return p.Can(auth.RunsView) || (run.WorkOrderID != nil && p.Can(auth.WorkOrdersView))
}

// loadRun fetches a run the caller may see, or answers 404.
func (s *Server) loadRun(w http.ResponseWriter, r *http.Request) (model.Run, bool) {
	id, valid := pathID(w, r)
	if !valid {
		return model.Run{}, false
	}
	run, err := s.store.GetRun(r.Context(), id)
	if err == nil && !canViewRun(principal(r), run) {
		err = store.ErrNotFound
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return run, false
	}
	return run, true
}

// loadCrew fetches a crew in the caller's areas, or answers 404.
func (s *Server) loadCrew(w http.ResponseWriter, r *http.Request) (model.Crew, bool) {
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

type validation struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
	// Warnings do not stop a run, but it is likely to fail (such as a model whose provider key is missing)
	Warnings []string `json:"warnings"`
}

func (s *Server) validateCrew(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadCrew(w, r)
	if !found {
		return
	}
	lib, err := s.launcher.Library(r.Context())
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	problems := runspec.Check(crew, lib)
	ok(w, validation{Valid: len(problems) == 0, Errors: orEmpty(problems), Warnings: s.crewKeyWarnings(crew, lib)})
}

type startRunRequest struct {
	Inputs      map[string]string `json:"inputs"`
	BudgetUSD   *float64          `json:"budget_usd"`
	CallbackURL string            `json:"callback_url"`
}

const maxIdempotencyKeyLen = 200

// idempotencyKey reads the Idempotency-Key header; ok is false (after answering 422) when it is malformed.
func idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) > maxIdempotencyKeyLen {
		fail(w, http.StatusUnprocessableEntity, "validation_failed", "The Idempotency-Key header is too long.",
			fmt.Sprintf("Idempotency-Key: at most %d characters", maxIdempotencyKeyLen))
		return "", false
	}
	return key, true
}

func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	var req startRunRequest
	if !decode(w, r, &req) {
		return
	}
	key, valid := idempotencyKey(w, r)
	if !valid {
		return
	}
	p := principal(r)
	if !s.runStarts.allow(p.UserID) {
		tooMany(w, "Runs are being started too quickly; wait a moment.")
		return
	}
	trigger := "manual"
	if p.Via == auth.ViaAPIKey {
		trigger = "api"
	}
	run, existing, err := s.launcher.StartCrewRun(r.Context(), p, launch.CrewRunRequest{
		CrewID: id, Inputs: req.Inputs, BudgetUSD: req.BudgetUSD, TriggerType: trigger,
		IdempotencyKey: key, CallbackURL: req.CallbackURL,
	})
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if existing {
		ok(w, run)
		return
	}
	created(w, run)
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.RunFilter{
		CrewID: q.Get("crew_id"), WorkOrderID: q.Get("work_order_id"), Status: q.Get("status"),
		TriggerType: q.Get("trigger_type"), Kind: q.Get("kind"), Scope: scopeOf(principal(r)), Viewer: principal(r).UserID,
	}
	var problems []string
	for field, v := range map[string]string{"crew_id": f.CrewID, "work_order_id": f.WorkOrderID} {
		if v != "" && !uuidPattern.MatchString(v) {
			problems = append(problems, field+": must be a UUID")
		}
	}
	if f.Status != "" && !slices.Contains([]string{"queued", "running", "succeeded", "failed", "cancelled"}, f.Status) {
		problems = append(problems, "status: unknown status")
	}
	if f.TriggerType != "" && !slices.Contains([]string{"manual", "api", "schedule", "webhook", "work_order"}, f.TriggerType) {
		problems = append(problems, "trigger_type: unknown trigger type")
	}
	if f.Kind != "" && f.Kind != model.RunKindCrew && !model.IsChatKind(f.Kind) {
		problems = append(problems, "kind: must be crew, chat or handoff")
	}
	if len(problems) > 0 {
		failErr(w, r, s.log, invalid(problems...))
		return
	}
	f.Limit, f.Offset = pageParams(r)
	runs, total, err := s.store.ListRuns(r.Context(), f)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	page(w, runs, model.Page{Total: total, Limit: f.Limit, Offset: f.Offset})
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, found := s.loadRun(w, r)
	if !found {
		return
	}
	detail, err := s.store.GetRunDetail(r.Context(), run.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, detail)
}

func (s *Server) deleteRun(w http.ResponseWriter, r *http.Request) {
	run, found := s.loadRun(w, r)
	if !found {
		return
	}
	// Chat budgets are counted from the chat turns and hand-offs of the last 24 hours, so those stay until they no
	// longer count
	if model.IsChatKind(run.Kind) && time.Since(run.CreatedAt) < chatBudgetWindow {
		fail(w, http.StatusConflict, "chat_turn_recent", "Chat turns and hand-offs count towards chat budgets for 24 hours; delete this one after that.")
		return
	}
	if err := s.store.DeleteRun(r.Context(), run.ID); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	noContent(w)
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	run, found := s.loadRun(w, r)
	if !found {
		return
	}
	p := principal(r)
	if !p.Can(auth.RunsCancel) && !(run.WorkOrderID != nil && p.Can(auth.WorkOrdersCancel)) {
		forbidden(w, auth.RunsCancel)
		return
	}
	out, err := s.runs.Cancel(r.Context(), run.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, out)
}

func (s *Server) runArtifacts(w http.ResponseWriter, r *http.Request) {
	run, found := s.loadRun(w, r)
	if !found {
		return
	}
	files, err := s.store.ListAttachments(r.Context(), store.AttachmentFilter{RunID: run.ID})
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, files)
}

func (s *Server) runDeliveries(w http.ResponseWriter, r *http.Request) {
	run, found := s.loadRun(w, r)
	if !found {
		return
	}
	deliveries, err := s.store.ListDeliveries(r.Context(), run.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, deliveries)
}

func (s *Server) runJournal(w http.ResponseWriter, r *http.Request) {
	run, found := s.loadRun(w, r)
	if !found {
		return
	}
	s.writeJournal(w, r, run)
}

func (s *Server) writeJournal(w http.ResponseWriter, r *http.Request, run model.Run) {
	spec, err := s.store.GetRunSpec(r.Context(), run.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	events, err := s.store.InteractionEvents(r.Context(), run.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	responses, err := s.store.LLMResponses(r.Context(), run.ID, journalResponseChars)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, journal.Build(run, spec, events, responses))
}

const journalResponseChars = 5000

func afterParam(r *http.Request) int {
	raw := r.URL.Query().Get("after")
	if raw == "" {
		raw = r.Header.Get("Last-Event-ID")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (s *Server) listRunEvents(w http.ResponseWriter, r *http.Request) {
	run, found := s.loadRun(w, r)
	if !found {
		return
	}
	events, err := s.store.ListEvents(r.Context(), run.ID, afterParam(r), store.MaxEventsPerRead)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, events)
}

func (s *Server) runInteractions(w http.ResponseWriter, r *http.Request) {
	run, found := s.loadRun(w, r)
	if !found {
		return
	}
	spec, err := s.store.GetRunSpec(r.Context(), run.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	events, err := s.store.InteractionEvents(r.Context(), run.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, interactions.Build(spec, events))
}

func (s *Server) runLLMCalls(w http.ResponseWriter, r *http.Request) {
	run, found := s.loadRun(w, r)
	if !found {
		return
	}
	calls, err := s.store.ListLLMCalls(r.Context(), run.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, calls)
}

func orEmpty[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// sseWriter writes Server-Sent Events and flushes after each one.
type sseWriter struct {
	w        http.ResponseWriter
	rc       *http.ResponseController
	lastSeq  int
	terminal bool // a final run_status has been sent
}

func (sw *sseWriter) event(ev model.RunEvent) error {
	if ev.Seq <= sw.lastSeq {
		return nil // already sent from the database backlog
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(sw.w, "id: %d\nevent: run_event\ndata: %s\n\n", ev.Seq, data); err != nil {
		return err
	}
	sw.lastSeq = ev.Seq
	if ev.Type == "run_status" {
		if status, _ := ev.Payload["status"].(string); model.RunStatus(status).Terminal() {
			sw.terminal = true
		}
	}
	return sw.rc.Flush()
}

func (sw *sseWriter) comment(text string) error {
	if _, err := fmt.Fprintf(sw.w, ": %s\n\n", text); err != nil {
		return err
	}
	return sw.rc.Flush()
}

// backlog sends every stored event after lastSeq.
func (s *Server) backlog(ctx context.Context, sw *sseWriter, runID string) error {
	for {
		events, err := s.store.ListEvents(ctx, runID, sw.lastSeq, store.MaxEventsPerRead)
		if err != nil {
			return err
		}
		for _, ev := range events {
			if err := sw.event(ev); err != nil {
				return err
			}
		}
		if len(events) < store.MaxEventsPerRead {
			return nil
		}
	}
}

func (s *Server) streamRun(w http.ResponseWriter, r *http.Request) {
	run, found := s.loadRun(w, r)
	if !found {
		return
	}
	id := run.ID
	ctx := r.Context()
	select {
	case s.streams <- struct{}{}:
		defer func() { <-s.streams }()
	default:
		fail(w, http.StatusServiceUnavailable, "too_many_streams", "Too many live run streams are open; close some run pages.")
		return
	}
	p := principal(r)
	if !s.streamsPer.acquire(p.UserID) {
		fail(w, http.StatusServiceUnavailable, "too_many_streams",
			fmt.Sprintf("You have %d live run streams open; close some run pages.", maxStreamsPerPrincipal))
		return
	}
	defer s.streamsPer.release(p.UserID)

	// Subscribe before reading the backlog so no event falls between the two
	live, unsubscribe := s.broker.Subscribe(id)
	defer unsubscribe()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	sw := &sseWriter{w: w, rc: http.NewResponseController(w), lastSeq: afterParam(r)}
	if _, err := fmt.Fprintf(w, "retry: %d\n\n", streamRetryMS); err != nil {
		return
	}

	if err := s.backlog(ctx, sw, id); err != nil {
		s.logStreamErr(id, err)
		return
	}
	if sw.terminal {
		return
	}
	if run, err := s.store.GetRun(ctx, id); err != nil || run.Status.Terminal() {
		// The run finished; its final run_status event is written just after the status, so wait for the
		// broker to close the run (or a short timeout) and send what is stored
		s.drainUntilClosed(ctx, live)
		if err := s.backlog(ctx, sw, id); err != nil {
			s.logStreamErr(id, err)
		}
		return
	}

	ping := time.NewTicker(streamPingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			// A person who was disabled, signed out or moved out of the run's areas stops receiving it
			fresh, err := s.store.StillValid(ctx, p)
			if err != nil || !canViewRun(fresh, run) {
				return
			}
			if err := sw.comment("ping"); err != nil {
				return
			}
		case ev, open := <-live:
			if !open {
				// Run finished, or this client fell behind: send whatever is stored, then let it reconnect
				if err := s.backlog(ctx, sw, id); err != nil {
					s.logStreamErr(id, err)
				}
				return
			}
			if err := sw.event(ev); err != nil || sw.terminal {
				return
			}
		}
	}
}

const finalEventWait = 5 * time.Second

func (s *Server) drainUntilClosed(ctx context.Context, live <-chan model.RunEvent) {
	timeout := time.NewTimer(finalEventWait)
	defer timeout.Stop()
	for {
		select {
		case _, open := <-live:
			if !open {
				return
			}
		case <-timeout.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) logStreamErr(runID string, err error) {
	if !errors.Is(err, context.Canceled) {
		s.log.Warn("run stream ended with error", "run_id", runID, "err", err)
	}
}
