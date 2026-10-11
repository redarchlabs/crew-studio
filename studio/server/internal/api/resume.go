package api

import (
	"context"
	"fmt"
	"net/http"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
)

type resumeRunRequest struct {
	BudgetUSD   *float64 `json:"budget_usd"`
	UseCurrent  bool     `json:"use_current"`
	CallbackURL string   `json:"callback_url"`
}

// resumeRun starts a run that reuses a failed or cancelled run's finished tasks and runs the rest (POST /runs/{id}/resume).
func (s *Server) resumeRun(w http.ResponseWriter, r *http.Request) {
	run, found := s.loadRun(w, r)
	if !found {
		return
	}
	var req resumeRunRequest
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
	resumed, existing, err := s.launcher.ResumeRun(r.Context(), p, launch.ResumeRequest{
		RunID: run.ID, BudgetUSD: req.BudgetUSD, UseCurrent: req.UseCurrent, TriggerType: trigger,
		IdempotencyKey: key, CallbackURL: req.CallbackURL,
	})
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if existing {
		ok(w, resumed)
		return
	}
	created(w, resumed)
}

// resumeHint says where a run could be resumed from, for people who may resume it: crews.run, or work_orders.create
// for the latest attempt of a work order.
func (s *Server) resumeHint(ctx context.Context, p *auth.Principal, run model.Run) (*model.ResumeHint, error) {
	hint, resumable := model.ResumePoint(run)
	if !resumable {
		return nil, nil
	}
	hint.Warnings = s.frozenKeyWarnings(run.Spec)
	if run.WorkOrderID == nil {
		if !p.Can(auth.CrewsRun) {
			return nil, nil
		}
		return &hint, nil
	}
	if !p.Can(auth.WorkOrdersCreate) {
		return nil, nil
	}
	wo, err := s.store.GetWorkOrder(ctx, *run.WorkOrderID)
	if err != nil || wo.CurrentRunID == nil || *wo.CurrentRunID != run.ID {
		return nil, nil //nolint:nilerr // a work order that is gone or moved on has nothing to resume
	}
	return &hint, nil
}

// hideUnviewableResumeLinks keeps run detail from naming runs the reader may not open: the run this one resumed, the
// runs that resumed it, and where reused outputs came from (flagged unavailable instead of linked). The runs are
// looked up in one query.
func (s *Server) hideUnviewableResumeLinks(ctx context.Context, p *auth.Principal, run *model.Run) error {
	var ids []string
	if run.ResumedFromRunID != nil {
		ids = append(ids, *run.ResumedFromRunID)
	}
	if run.ResumedBy != nil {
		ids = append(ids, *run.ResumedBy...)
	}
	if run.TaskOutputs != nil {
		for _, o := range *run.TaskOutputs {
			if o.ReusedFromRunID != nil {
				ids = append(ids, *o.ReusedFromRunID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	found, err := s.store.RunsByIDs(ctx, ids)
	if err != nil {
		return err
	}
	viewable := make(map[string]bool, len(found))
	for _, other := range found {
		viewable[other.ID] = canViewRun(p, other)
	}
	if run.ResumedFromRunID != nil && !viewable[*run.ResumedFromRunID] {
		run.ResumedFromRunID = nil
	}
	if run.ResumedBy != nil {
		kept := make([]string, 0, len(*run.ResumedBy))
		for _, id := range *run.ResumedBy {
			if viewable[id] {
				kept = append(kept, id)
			}
		}
		run.ResumedBy = &kept
	}
	if run.TaskOutputs != nil {
		for i, o := range *run.TaskOutputs {
			if o.ReusedFromRunID != nil && !viewable[*o.ReusedFromRunID] {
				(*run.TaskOutputs)[i].ReusedFromUnavailable = true
			}
		}
	}
	return nil
}

// frozenKeyWarnings says which LLMs the run started with would fail to authenticate now, as a crew's validation does.
func (s *Server) frozenKeyWarnings(spec *model.RunSpec) []string {
	if spec == nil || len(spec.LLMs) == 0 {
		return []string{}
	}
	models := make([]string, len(spec.LLMs))
	for i, l := range spec.LLMs {
		models[i] = l.Model
	}
	missing := s.missingProviderKeys(models)
	out := []string{}
	for _, l := range spec.LLMs {
		if env := missing[l.Model]; env != "" {
			out = append(out, fmt.Sprintf("LLM %s (%s): %s", l.Name, l.Model, providerKeyWarning(env)))
		}
	}
	return out
}
