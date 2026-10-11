package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/pmo"
	"crewstudio/internal/store"
)

// Set up a PMO (design section 12): the wizard's options and the one-click install. Installing creates agents,
// tasks, a crew and two schedules, so it needs the permissions for each (pmo.Needs).

func (s *Server) pmoRoutes(r chi.Router) {
	r.Route("/pmo", func(r chi.Router) {
		r.With(s.need(pmo.Needs...)).Get("/setup", s.pmoSetupOptions)
		r.With(s.need(pmo.Needs...)).Post("/setup", s.installPMO)
	})
}

func (s *Server) pmoSetupOptions(w http.ResponseWriter, r *http.Request) {
	opts, err := pmo.NewService(s.store).SetupOptions(r.Context(), principal(r))
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	models := make([]string, len(opts.LLMs))
	for i, l := range opts.LLMs {
		models[i] = l.Kind
	}
	missing := s.missingProviderKeys(models)
	for i, l := range opts.LLMs {
		if env := missing[l.Kind]; env != "" {
			opts.LLMs[i].Warning = providerKeyWarning(env)
		}
	}
	ok(w, opts)
}

// withArticle puts "a" or "an" before an entity name.
func withArticle(entity string) string {
	if entity != "" && strings.ContainsRune("aeiou", rune(entity[0])) {
		return "an " + entity
	}
	return "a " + entity
}

func (s *Server) installPMO(w http.ResponseWriter, r *http.Request) {
	var in pmo.Request
	if !decode(w, r, &in) {
		return
	}
	p := principal(r)
	svc := pmo.NewService(s.store)
	req, err := pmo.Normalize(in)
	var plan store.PMOInstall
	if err == nil {
		plan, err = svc.Prepare(r.Context(), p, req, time.Now())
	}
	var out store.PMOInstalled
	if err == nil {
		// Schedules run as whoever installs the PMO, like any schedule someone saves
		ctx := store.WithActor(r.Context(), store.Actor{ID: p.UserID, Name: p.Name, APIKeyID: p.APIKeyID})
		out, err = svc.Install(ctx, plan)
	}
	// Everything the PMO makes is named after it, so a taken name is a problem with the Name field
	var conflict *store.ConflictError
	if errors.As(err, &conflict) && conflict.Name != "" {
		err = invalid(fmt.Sprintf("name: %s named %q already exists; choose another name", withArticle(conflict.Entity), conflict.Name))
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.scheduler.Notify()
	scheduleIDs := make([]string, len(out.Schedules))
	for i, sc := range out.Schedules {
		scheduleIDs[i] = sc.ID
	}
	s.audit(r, "pmo_installed", p.UserID, p.Name, map[string]any{"crew_id": out.Crew.ID, "crew_name": out.Crew.Name,
		"schedule_ids": scheduleIDs, "tracker_connection_id": req.TrackerConnectionID, "destinations": len(req.Destinations)})
	created(w, out)
}
