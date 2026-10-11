package api

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

func (s *Server) workOrderRoutes(r chi.Router) {
	r.Route("/work-orders", func(r chi.Router) {
		r.With(s.need(auth.WorkOrdersView)).Get("/", s.listWorkOrders)
		r.With(s.need(auth.WorkOrdersCreate)).Post("/", s.createWorkOrder)
		r.With(s.need(auth.WorkOrdersView)).Get("/{id}", s.getWorkOrder)
		r.With(s.need(auth.WorkOrdersCreate)).Put("/{id}", s.updateWorkOrder)
		r.With(s.need(auth.WorkOrdersDelete)).Delete("/{id}", s.deleteWorkOrder)
		r.With(s.need(auth.WorkOrdersCreate)).Post("/{id}/submit", s.submitWorkOrder)
		r.With(s.need(auth.WorkOrdersCancel)).Post("/{id}/cancel", s.cancelWorkOrder)
		r.With(s.need(auth.WorkOrdersView)).Get("/{id}/journal", s.workOrderJournal)
		r.With(s.need(auth.WorkOrdersView)).Get("/{id}/questions", s.workOrderQuestions)
		s.followUpRoutes(r)
		r.With(s.need(auth.WorkOrdersCreate)).Delete("/{id}/attachments/{attachmentId}", s.deleteAttachment)
		s.workOrderReportRoutes(r)
	})
}

// loadWorkOrder fetches a work order in the caller's areas, or answers 404.
func (s *Server) loadWorkOrder(w http.ResponseWriter, r *http.Request) (model.WorkOrder, bool) {
	id, valid := pathID(w, r)
	if !valid {
		return model.WorkOrder{}, false
	}
	wo, err := s.store.GetWorkOrder(r.Context(), id)
	if err == nil && !principal(r).SeesAreas(wo.AreaIDs) {
		err = store.ErrNotFound
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return wo, false
	}
	return wo, true
}

var (
	workOrderStatuses = []string{model.WorkOrderDraft, "queued", "running", "succeeded", "failed", "cancelled"}
	workOrderSources  = []string{model.SourceManual, model.SourceAPI, model.SourceSchedule, model.SourceWebhook, model.SourceChat}
)

func (s *Server) listWorkOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.WorkOrderFilter{
		Scope: scopeOf(principal(r)), Status: q.Get("status"), Source: q.Get("source"), SourceID: q.Get("source_id"),
		CrewID: q.Get("crew_id"), Query: q.Get("q"),
	}
	var problems []string
	if f.Status != "" && !slices.Contains(workOrderStatuses, f.Status) {
		problems = append(problems, "status: unknown status")
	}
	if f.Source != "" && !slices.Contains(workOrderSources, f.Source) {
		problems = append(problems, "source: unknown source")
	}
	if f.SourceID != "" && !uuidPattern.MatchString(f.SourceID) {
		problems = append(problems, "source_id: must be a UUID")
	}
	if f.CrewID != "" && !uuidPattern.MatchString(f.CrewID) {
		problems = append(problems, "crew_id: must be a UUID")
	}
	if len(f.Query) > 200 {
		problems = append(problems, "q: at most 200 characters")
	}
	if len(problems) > 0 {
		failErr(w, r, s.log, invalid(problems...))
		return
	}
	f.Limit, f.Offset = pageParams(r)
	items, total, err := s.store.ListWorkOrders(r.Context(), f)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	page(w, items, model.Page{Total: total, Limit: f.Limit, Offset: f.Offset})
}

type workOrderRequest struct {
	model.WorkOrderInput
	CallbackURL string `json:"callback_url"`
}

func (s *Server) createWorkOrder(w http.ResponseWriter, r *http.Request) {
	var req workOrderRequest
	if !decode(w, r, &req) {
		return
	}
	key, valid := idempotencyKey(w, r)
	if !valid {
		return
	}
	clean, err := model.NormalizeWorkOrder(req.WorkOrderInput)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	p := principal(r)
	if clean.Submit && !s.runStarts.allow(p.UserID) {
		tooMany(w, "Work orders are being submitted too quickly; wait a moment.")
		return
	}
	var callback *launch.CallbackRequest
	if req.CallbackURL != "" {
		if p.Kind != auth.KindService {
			failErr(w, r, s.log, invalid("callback_url: only API keys of service accounts can request callbacks"))
			return
		}
		if problem := model.CheckCallbackURL(req.CallbackURL); problem != "" {
			failErr(w, r, s.log, invalid("callback_url: "+problem))
			return
		}
		callback = &launch.CallbackRequest{URL: req.CallbackURL, Source: store.CallbackFromServiceAccount, Owner: p.UserID}
	}
	result, err := s.launcher.CreateWorkOrder(r.Context(), p, clean, launch.WorkOrderSource{}, launch.Idempotency{Key: key}, callback)
	wo := result.WorkOrder
	if err != nil {
		var problems *launch.ProblemsError
		if wo.ID != "" && errors.As(err, &problems) {
			// Created, but it cannot run yet: say so, and point at the saved draft
			fail(w, http.StatusUnprocessableEntity, "validation_failed",
				fmt.Sprintf("The work order was saved as a draft (WO-%d, id %s) but cannot run yet.", wo.Number, wo.ID), problems.Details...)
			return
		}
		failErr(w, r, s.log, err)
		return
	}
	if result.Replayed {
		ok(w, wo)
		return
	}
	created(w, wo)
}

func (s *Server) getWorkOrder(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	detail, err := s.store.GetWorkOrderDetail(r.Context(), wo.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, detail)
}

func (s *Server) updateWorkOrder(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	var in model.WorkOrderInput
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeWorkOrder(in)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	target, err := s.launcher.ResolveTarget(r.Context(), principal(r), clean.CrewID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	out, err := s.store.UpdateWorkOrder(r.Context(), wo.ID, clean, store.WorkOrderMeta{TargetName: target.Name, AreaIDs: target.AreaIDs})
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, out)
}

func (s *Server) deleteWorkOrder(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	if err := s.store.DeleteWorkOrder(r.Context(), wo.ID); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	noContent(w)
}

type submitRequest struct {
	BudgetUSD   *float64 `json:"budget_usd"`
	CallbackURL string   `json:"callback_url"`
}

func (s *Server) submitWorkOrder(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	var req submitRequest
	if !decode(w, r, &req) {
		return
	}
	key, valid := idempotencyKey(w, r)
	if !valid {
		return
	}
	p := principal(r)
	if !s.runStarts.allow(p.UserID) {
		tooMany(w, "Work orders are being submitted too quickly; wait a moment.")
		return
	}
	run, existing, err := s.launcher.SubmitWorkOrder(r.Context(), p, wo.ID, launch.SubmitOptions{
		BudgetUSD: req.BudgetUSD, IdempotencyKey: key, CallbackURL: req.CallbackURL,
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

func (s *Server) cancelWorkOrder(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	if wo.CurrentRunID == nil {
		failErr(w, r, s.log, &store.ConflictError{Message: "This work order has not been submitted."})
		return
	}
	run, err := s.runs.Cancel(r.Context(), *wo.CurrentRunID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, run)
}

func (s *Server) workOrderJournal(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	runID := r.URL.Query().Get("run_id")
	if runID == "" {
		if wo.CurrentRunID == nil {
			failErr(w, r, s.log, &store.ConflictError{Message: "This work order has not been submitted yet, so there is no journal."})
			return
		}
		runID = *wo.CurrentRunID
	}
	if !uuidPattern.MatchString(runID) {
		failErr(w, r, s.log, invalid("run_id: must be a UUID"))
		return
	}
	run, err := s.store.GetRun(r.Context(), runID)
	if err == nil && (run.WorkOrderID == nil || *run.WorkOrderID != wo.ID) {
		err = store.ErrNotFound
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.writeJournal(w, r, run)
}
