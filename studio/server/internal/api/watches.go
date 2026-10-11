package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/watch"
)

// A schedule's watch (docs/crew-state-and-pmo-design.md, section 5). Managing it is part of managing the
// schedule (schedules.manage); seeing it is part of seeing the schedule (schedules.view).

const (
	watchCheckListLimit = 20
	watchStatsDays      = 30
	// Previews fetch outside systems on someone's behalf, so they are throttled per person
	watchPreviewsPerMinute = 10
	watchPreviewBurst      = 5
)

var watchPreviews = newKeyedLimiter(rate.Every(time.Minute/watchPreviewsPerMinute), watchPreviewBurst)

// watchRoutes hangs off /schedules (see scheduleRoutes).
func (s *Server) watchRoutes(r chi.Router) {
	r.With(s.need(auth.SchedulesView)).Get("/{id}/watch", s.getWatch)
	r.With(s.need(auth.SchedulesManage)).Put("/{id}/watch", s.putWatch)
	r.With(s.need(auth.SchedulesManage)).Delete("/{id}/watch", s.deleteWatch)
	r.With(s.need(auth.SchedulesManage)).Post("/{id}/watch/preview", s.previewWatch)
	r.With(s.need(auth.SchedulesManage, auth.StateManage)).Delete("/{id}/watch/snapshot", s.clearWatchSnapshot)
}

// watchConnection is a connection a watch may read through, for the source picker (never its secrets).
type watchConnection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type watchView struct {
	// Watch is null when the schedule has none
	Watch       *model.Watch            `json:"watch"`
	Stats       *model.WatchStats       `json:"stats"`
	Checks      []model.WatchCheck      `json:"checks"`
	SourceKinds []model.WatchSourceKind `json:"source_kinds"`
	// Connections the schedule's crew may use for a watch; only for people who can manage the schedule
	Connections []watchConnection `json:"connections"`
}

func (s *Server) watchView(ctx context.Context, p *auth.Principal, sc model.Schedule) (watchView, error) {
	view := watchView{Checks: []model.WatchCheck{}, SourceKinds: model.WatchSourceKinds, Connections: []watchConnection{}}
	w, err := s.store.WatchOfSchedule(ctx, sc.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return view, err
	default:
		view.Watch = &w
		stats, err := s.store.WatchStats(ctx, w.ID, sc.CrewID, watchStatsDays, time.Now())
		if err != nil {
			return view, err
		}
		view.Stats = &stats
		if view.Checks, err = s.store.ListWatchChecks(ctx, w.ID, watchCheckListLimit); err != nil {
			return view, err
		}
	}
	if p.Can(auth.SchedulesManage) && sc.CrewID != nil {
		conns, err := s.store.CrewConnections(ctx, *sc.CrewID, scopeOf(p))
		if err != nil {
			return view, err
		}
		for _, c := range conns {
			if c.Enabled && (c.Kind == "http" || model.IsTrackerKind(c.Kind)) && sharesArea(c.AreaIDs, sc.AreaIDs) {
				view.Connections = append(view.Connections, watchConnection{ID: c.ID, Name: c.Name, Kind: c.Kind})
			}
		}
	}
	return view, nil
}

func sharesArea(a, b []string) bool {
	return slices.ContainsFunc(a, func(id string) bool { return slices.Contains(b, id) })
}

func (s *Server) getWatch(w http.ResponseWriter, r *http.Request) {
	sc, found := s.loadSchedule(w, r)
	if !found {
		return
	}
	view, err := s.watchView(r.Context(), principal(r), sc)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, view)
}

// cleanWatch validates a watch definition for a schedule, including its CSS selector and connection.
func (s *Server) cleanWatch(ctx context.Context, p *auth.Principal, sc model.Schedule, in model.WatchInput) (model.WatchInput, error) {
	clean, err := model.NormalizeWatch(in)
	if err != nil {
		return clean, err
	}
	if problem := watch.CheckSelector(clean.Source.Selector); problem != "" {
		return clean, invalid("source.selector: " + problem)
	}
	if clean.ConnectionID == nil {
		return clean, nil
	}
	if sc.CrewID == nil {
		return clean, invalid("crew_id: the schedule's crew was deleted")
	}
	use, err := s.store.ConnectionForCrew(ctx, *clean.ConnectionID, *sc.CrewID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !p.SeesAreas(use.Connection.AreaIDs)) {
		return clean, invalid("connection_id: no such connection")
	}
	if err != nil {
		return clean, err
	}
	if problem := watch.ConnectionProblem(use, clean.Source.Kind); problem != "" {
		return clean, invalid("connection_id: " + problem)
	}
	if clean.Source.JQL != "" && use.Connection.Kind != model.TrackerJira {
		return clean, invalid("source.jql: JQL only applies to Jira connections")
	}
	return clean, nil
}

func (s *Server) putWatch(w http.ResponseWriter, r *http.Request) {
	sc, found := s.loadSchedule(w, r)
	if !found {
		return
	}
	var in model.WatchInput
	if !decode(w, r, &in) {
		return
	}
	p := principal(r)
	clean, err := s.cleanWatch(r.Context(), p, sc, in)
	var saved model.Watch
	if err == nil {
		saved, err = s.store.SaveWatch(r.Context(), sc.ID, clean)
	}
	var view watchView
	if err == nil {
		s.auditWatch(r, "watch_saved", sc, map[string]any{"source_kind": saved.Source.Kind, "run_when": saved.RunWhen,
			"dry_run": saved.DryRun, "connection_id": saved.ConnectionID, "followups": saved.Followups != nil})
		view, err = s.watchView(r.Context(), p, sc)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, view)
}

func (s *Server) deleteWatch(w http.ResponseWriter, r *http.Request) {
	sc, found := s.loadSchedule(w, r)
	if !found {
		return
	}
	if err := s.store.DeleteWatch(r.Context(), sc.ID); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditWatch(r, "watch_deleted", sc, nil)
	noContent(w)
}

func (s *Server) clearWatchSnapshot(w http.ResponseWriter, r *http.Request) {
	sc, found := s.loadSchedule(w, r)
	if !found {
		return
	}
	wt, err := s.store.WatchOfSchedule(r.Context(), sc.ID)
	if err == nil {
		err = s.store.ClearSnapshot(r.Context(), wt.ID)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditWatch(r, "watch_snapshot_cleared", sc, nil)
	noContent(w)
}

// auditWatch records who changed a schedule's watch (watches have no revision history of their own).
func (s *Server) auditWatch(r *http.Request, action string, sc model.Schedule, extra map[string]any) {
	p := principal(r)
	details := map[string]any{"schedule_id": sc.ID, "schedule_name": sc.Name}
	for k, v := range extra {
		details[k] = v
	}
	s.audit(r, action, p.UserID, p.Name, details)
}

// previewWatch fetches the source of the definition in the body now and answers with the changes and due
// follow-ups a tick would see. It saves nothing and starts nothing.
func (s *Server) previewWatch(w http.ResponseWriter, r *http.Request) {
	sc, found := s.loadSchedule(w, r)
	if !found {
		return
	}
	p := principal(r)
	if !watchPreviews.allow(p.UserID) {
		tooMany(w, "Previews are being run too quickly; wait a moment.")
		return
	}
	var in model.WatchInput
	if !decode(w, r, &in) {
		return
	}
	clean, err := s.cleanWatch(r.Context(), p, sc, in)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	check := watch.Check{Def: clean, Schedule: sc}
	if saved, err := s.store.WatchOfSchedule(r.Context(), sc.ID); err == nil {
		check.Saved = &saved
	} else if !errors.Is(err, store.ErrNotFound) {
		failErr(w, r, s.log, err)
		return
	}
	ev, err := s.watches.Evaluate(r.Context(), check, time.Now())
	if err != nil {
		var serr *watch.SourceError
		var limit *watch.LimitError
		if errors.As(err, &serr) || errors.As(err, &limit) {
			fail(w, http.StatusUnprocessableEntity, "source_failed", "The watch could not read its source.", err.Error())
			return
		}
		failErr(w, r, s.log, err)
		return
	}
	ok(w, ev)
}

// crewWatch is one watch on the crew's State tab.
type crewWatch struct {
	model.Watch
	Stats model.WatchStats `json:"stats"`
}

func (s *Server) listCrewWatches(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	watches, err := s.store.CrewWatches(r.Context(), crew.ID)
	out := make([]crewWatch, 0, len(watches))
	for i := 0; err == nil && i < len(watches); i++ {
		var stats model.WatchStats
		stats, err = s.store.WatchStats(r.Context(), watches[i].ID, &crew.ID, watchStatsDays, time.Now())
		out = append(out, crewWatch{Watch: watches[i], Stats: stats})
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, out)
}
