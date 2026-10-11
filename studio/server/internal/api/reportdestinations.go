package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// Where a schedule's reports go (design section 12). Seeing them is part of seeing the schedule (schedules.view);
// changing them is part of managing it (schedules.manage). Each destination uses a connection of its kind that
// the schedule's crew may use, shares an area with the schedule and the person changing it can see.

// reportDestinationRoutes hangs off /schedules (see scheduleRoutes).
func (s *Server) reportDestinationRoutes(r chi.Router) {
	r.With(s.need(auth.SchedulesView)).Get("/{id}/report-destinations", s.getReportDestinations)
	r.With(s.need(auth.SchedulesManage)).Put("/{id}/report-destinations", s.putReportDestinations)
}

type destinationsView struct {
	Destinations []model.ReportDestination `json:"destinations"`
	// Connections are the slack, teams, email and km2 connections the schedule's crew may use; only for people who
	// can manage the schedule
	Connections []watchConnection `json:"connections"`
	// Kinds are the destination kinds; the work order output is always included
	Kinds []string `json:"kinds"`
}

type destinationsRequest struct {
	Destinations []model.ReportDestinationInput `json:"destinations"`
}

func (s *Server) destinationsView(ctx context.Context, p *auth.Principal, sc model.Schedule) (destinationsView, error) {
	view := destinationsView{Connections: []watchConnection{}, Kinds: model.DestinationKinds}
	var err error
	if view.Destinations, err = s.store.ReportDestinations(ctx, sc.ID); err != nil {
		return view, err
	}
	if !p.Can(auth.SchedulesManage) || sc.CrewID == nil {
		return view, nil
	}
	conns, err := s.store.CrewConnections(ctx, *sc.CrewID, scopeOf(p))
	if err != nil {
		return view, err
	}
	for _, c := range conns {
		if c.Enabled && slices.Contains(model.DestinationKinds, c.Kind) && sharesArea(c.AreaIDs, sc.AreaIDs) {
			view.Connections = append(view.Connections, watchConnection{ID: c.ID, Name: c.Name, Kind: c.Kind})
		}
	}
	return view, nil
}

func (s *Server) getReportDestinations(w http.ResponseWriter, r *http.Request) {
	sc, found := s.loadSchedule(w, r)
	if !found {
		return
	}
	view, err := s.destinationsView(r.Context(), principal(r), sc)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, view)
}

func (s *Server) putReportDestinations(w http.ResponseWriter, r *http.Request) {
	sc, found := s.loadSchedule(w, r)
	if !found {
		return
	}
	var in destinationsRequest
	if !decode(w, r, &in) {
		return
	}
	p := principal(r)
	clean, err := model.NormalizeDestinations(in.Destinations)
	if err == nil {
		err = s.checkDestinations(r.Context(), p, sc, clean)
	}
	var view destinationsView
	if err == nil {
		_, err = s.store.SetReportDestinations(r.Context(), sc.ID, clean)
	}
	if err == nil {
		kinds := make([]string, len(clean))
		for i, d := range clean {
			kinds[i] = d.Kind
		}
		s.audit(r, "report_destinations_set", p.UserID, p.Name, map[string]any{"schedule_id": sc.ID, "schedule_name": sc.Name, "kinds": kinds})
		view, err = s.destinationsView(r.Context(), p, sc)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, view)
}

// checkDestinations checks each destination's connection: one the caller can see, of the destination's kind, that
// the schedule's crew may use and that shares an area with the schedule.
func (s *Server) checkDestinations(ctx context.Context, p *auth.Principal, sc model.Schedule, in []model.ReportDestinationInput) error {
	if len(in) > 0 && sc.CrewID == nil {
		return invalid("destinations: the schedule's crew was deleted")
	}
	var problems []string
	for i, d := range in {
		field := fmt.Sprintf("destinations[%d].connection_id", i)
		use, err := s.store.ConnectionForCrew(ctx, d.ConnectionID, *sc.CrewID)
		switch {
		case errors.Is(err, store.ErrNotFound) || (err == nil && !p.SeesAreas(use.Connection.AreaIDs)):
			problems = append(problems, field+": no such connection")
		case err != nil:
			return err
		case !use.Connection.Enabled:
			problems = append(problems, fmt.Sprintf("%s: %s is turned off", field, use.Connection.Name))
		case len(model.RecipientsOutside(use.Connection.Config, d.Recipients)) > 0:
			problems = append(problems, fmt.Sprintf("destinations[%d].recipients: %s may only send to %s", i, use.Connection.Name,
				strings.Join(model.EmailDomains(use.Connection.Config), ", ")))
		case use.Connection.Kind != d.Kind:
			problems = append(problems, fmt.Sprintf("%s: %s is a %s connection, not %s", field, use.Connection.Name, use.Connection.Kind, d.Kind))
		case !use.Linked:
			problems = append(problems, fmt.Sprintf("%s: the crew may not use %s; choose it on the crew's State tab first", field, use.Connection.Name))
		case !sharesArea(use.Connection.AreaIDs, sc.AreaIDs):
			problems = append(problems, fmt.Sprintf("%s: %s shares no functional area with the schedule", field, use.Connection.Name))
		}
	}
	if len(problems) > 0 {
		return invalid(problems...)
	}
	return nil
}
