package launch

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
)

// ResumeRequest resumes a failed or cancelled crew run from its first unfinished task.
type ResumeRequest struct {
	RunID string
	// BudgetUSD is the new run's budget for its own spending; nil uses the earlier run's budget
	BudgetUSD *float64
	// UseCurrent runs the remaining tasks with the crew's current agents, tasks and LLMs instead of the frozen ones;
	// the finished tasks must still be the crew's first tasks, unchanged
	UseCurrent     bool
	TriggerType    string
	IdempotencyKey string
	CallbackURL    string
}

var (
	errNotResumable = &store.ConflictError{Code: "not_resumable",
		Message: "Only a failed or cancelled crew run with at least one finished task and at least one task left can be resumed."}
	errNotLatestAttempt = &store.ConflictError{Code: "not_latest_attempt",
		Message: "Only the latest attempt of a work order can be resumed."}
)

// ResumeRun starts a run that reuses the earlier run's finished task outputs and runs only the tasks after them.
// existing is true when an idempotency key matched an earlier resume of the same run.
func (l *Launcher) ResumeRun(ctx context.Context, p *auth.Principal, req ResumeRequest) (model.Run, bool, error) {
	src, err := l.store.GetRunDetail(ctx, req.RunID)
	if err != nil {
		return model.Run{}, false, err
	}
	if !p.SeesAreas(src.AreaIDs) || model.IsChatKind(src.Kind) {
		return model.Run{}, false, store.ErrNotFound
	}
	// Scoped to the run being resumed, so a repeated key returns the earlier resume of this run only. A retry is
	// answered before the checks below: once the first request succeeded, the run is no longer the latest attempt
	scope := idempotencyScope(p) + ":resume:" + src.ID
	if req.IdempotencyKey != "" {
		if err := requireAny(p, auth.CrewsRun, auth.WorkOrdersCreate); err != nil {
			return model.Run{}, false, err
		}
		if prior, found, err := l.store.RunByIdempotencyKey(ctx, scope, req.IdempotencyKey); err != nil || found {
			return prior, found, err
		}
	}
	wo, err := l.mayResume(ctx, p, src)
	if err != nil {
		return model.Run{}, false, err
	}
	hint, ok := model.ResumePoint(src)
	if !ok {
		return model.Run{}, false, errNotResumable
	}
	// As for starting a run, the crew must still exist and be in the person's areas
	crew, err := l.resumeCrew(ctx, p, src)
	if err != nil {
		return model.Run{}, false, err
	}
	spec, err := l.resumeSpec(ctx, src, crew, wo, req, hint.StartPosition)
	if err != nil {
		return model.Run{}, false, err
	}
	cb, err := callbackFor(p, req.CallbackURL)
	if err != nil {
		return model.Run{}, false, err
	}
	meta := store.RunMeta{
		AreaIDs: crew.AreaIDs, CreatedBy: p.UserID, CreatedByName: p.Name, TriggerType: req.TriggerType, Callback: cb,
		Resume: &store.RunResume{FromRunID: src.ID, StartPosition: hint.StartPosition, Outputs: spec.Resume.Outputs},
	}
	if wo != nil {
		meta.AreaIDs, meta.WorkOrderID, meta.TriggerType = wo.AreaIDs, wo.ID, "work_order"
	}
	if req.IdempotencyKey != "" {
		meta.IdempotencyScope, meta.IdempotencyKey = scope, req.IdempotencyKey
	}
	run, existing, err := l.store.CreateRun(ctx, spec, meta)
	if err == nil && !existing {
		l.runs.Notify()
	}
	return run, existing, err
}

// mayResume checks the permission a resume needs: work_orders.create for a work order's attempt (only its latest
// attempt, and it returns the work order), crews.run for any other run.
func (l *Launcher) mayResume(ctx context.Context, p *auth.Principal, src model.Run) (*model.WorkOrder, error) {
	if src.WorkOrderID == nil {
		return nil, require(p, auth.CrewsRun)
	}
	if err := require(p, auth.WorkOrdersCreate); err != nil {
		return nil, err
	}
	wo, err := l.store.GetWorkOrder(ctx, *src.WorkOrderID)
	if err != nil {
		return nil, err
	}
	if !p.SeesAreas(wo.AreaIDs) {
		return nil, store.ErrNotFound
	}
	if wo.CurrentRunID == nil || *wo.CurrentRunID != src.ID {
		return nil, errNotLatestAttempt
	}
	return &wo, nil
}

// resumeCrew loads the crew the earlier run used; a deleted crew, or one moved out of the person's areas, cannot be
// resumed.
func (l *Launcher) resumeCrew(ctx context.Context, p *auth.Principal, src model.Run) (model.Crew, error) {
	gone := &ProblemsError{Message: "This cannot run yet.", Details: []string{"The crew this run used was deleted."}}
	if src.CrewID == nil {
		return model.Crew{}, gone
	}
	crew, err := l.store.GetCrew(ctx, *src.CrewID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !p.SeesAreas(crew.AreaIDs)) {
		return model.Crew{}, gone
	}
	return crew, err
}

// resumeSpec is the spec the resumed run starts with: the earlier run's frozen spec, or the crew as it is now when
// asked, plus the outputs it reuses and the budget for its own spending.
func (l *Launcher) resumeSpec(ctx context.Context, src model.Run, crew model.Crew, wo *model.WorkOrder, req ResumeRequest, start int) (model.RunSpec, error) {
	budget := src.Spec.BudgetUSD
	if req.BudgetUSD != nil {
		budget = *req.BudgetUSD
	}
	if budget <= 0 || budget > maxRunBudgetUSD {
		return model.RunSpec{}, &ProblemsError{Message: "This cannot run yet.",
			Details: []string{fmt.Sprintf("budget_usd: must be more than 0 and at most %d", maxRunBudgetUSD)}}
	}
	lib, err := l.Library(ctx)
	if err != nil {
		return model.RunSpec{}, err
	}
	var spec model.RunSpec
	if req.UseCurrent {
		if spec, err = l.currentSpec(ctx, src, crew, wo, lib, budget, start); err != nil {
			return model.RunSpec{}, err
		}
	} else {
		if problems := frozenProblems(*src.Spec, lib); len(problems) > 0 {
			return model.RunSpec{}, &ProblemsError{Message: "This cannot resume as it was.", Details: problems}
		}
		spec = *src.Spec
	}
	spec.BudgetUSD = budget
	spec.Resume = &model.ResumeSpec{FromRunID: src.ID, StartPosition: start, Outputs: model.ReusedOutputs(src, start)}
	if err := l.checkInputFiles(ctx, spec); err != nil {
		return model.RunSpec{}, err
	}
	return spec, nil
}

// frozenProblems lists what was taken away since the run started that it would still use as it was: an LLM that was
// deleted, or a tool that no longer exists or is no longer available. Resuming must not bring back what an
// administrator removed.
func frozenProblems(spec model.RunSpec, lib runspec.Library) []string {
	var problems []string
	llmGone := func(owner string, id *string) {
		if id != nil {
			if _, ok := lib.LLMs[*id]; !ok {
				problems = append(problems, owner+"'s LLM was deleted; resume with the crew's current settings instead.")
			}
		}
	}
	if spec.Crew.Process == model.ProcessHierarchical {
		llmGone("The manager", spec.Crew.ManagerLLMID)
	}
	for _, agent := range spec.Agents {
		owner := fmt.Sprintf("Agent %q", agent.Name)
		llmGone(owner, agent.LLMID)
		for _, key := range agent.Tools {
			if tool, ok := lib.Tools[key]; !ok || !tool.Available {
				problems = append(problems, fmt.Sprintf("%s uses tool %q, which is no longer available.", owner, key))
			}
		}
	}
	return problems
}

// currentSpec freezes the crew as it is now with the earlier run's inputs. The finished tasks must still be the
// crew's first tasks, in order and unchanged, with at least one task after them.
func (l *Launcher) currentSpec(ctx context.Context, src model.Run, crew model.Crew, wo *model.WorkOrder, lib runspec.Library,
	budget float64, start int) (model.RunSpec, error) {
	watched := wo != nil && wo.Source == model.SourceSchedule
	spec, err := freeze(crew, lib, src.Inputs, &budget, crew.BudgetUSD, watched)
	var problems *ProblemsError
	if errors.As(err, &problems) {
		// Such as an input the run was given that the crew no longer has
		return model.RunSpec{}, &ProblemsError{Message: "This cannot resume with the crew's current settings; resume it as it was, or run it again.",
			Details: problems.Details}
	}
	if err == nil {
		err = l.freezeMemory(ctx, crew, lib, &spec)
	}
	if err == nil {
		err = l.freezeEscalation(ctx, crew, &spec)
	}
	if err != nil {
		return model.RunSpec{}, err
	}
	if !samePrefix(*src.Spec, spec, start) {
		return model.RunSpec{}, &ProblemsError{Message: "This cannot resume with the crew's current settings.",
			Details: []string{finishedChanged(start) + " since this run finished them; resume it as it was, or run it again."}}
	}
	spec.WorkOrder = src.Spec.WorkOrder
	return spec, nil
}

// samePrefix reports whether the first n tasks are the same tasks with the same instructions in both specs, done by
// agents with the same instructions and tools, and the second spec has more tasks after them. Reusing an output is
// only sound when what produced it has not changed; an agent's LLM may change (switching models is what a resume with
// the current settings is often for).
func samePrefix(before, now model.RunSpec, n int) bool {
	if len(before.Tasks) < n || len(now.Tasks) <= n {
		return false
	}
	for i := range n {
		a, b := before.Tasks[i], now.Tasks[i]
		if a.ID != b.ID || a.Name != b.Name || a.Description != b.Description || a.ExpectedOutput != b.ExpectedOutput ||
			!sameID(a.AgentID, b.AgentID) || !slices.Equal(a.ContextTaskIDs, b.ContextTaskIDs) {
			return false
		}
		if a.AgentID != nil && !sameAgent(agentByID(before.Agents, *a.AgentID), agentByID(now.Agents, *a.AgentID)) {
			return false
		}
	}
	return true
}

func agentByID(agents []model.Agent, id string) *model.Agent {
	for i := range agents {
		if agents[i].ID == id {
			return &agents[i]
		}
	}
	return nil
}

// sameAgent compares what an agent is told and can do, leaving out its LLM.
func sameAgent(a, b *model.Agent) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Role == b.Role && a.Goal == b.Goal && a.Backstory == b.Backstory && slices.Equal(a.Tools, b.Tools) &&
		a.MaxIter == b.MaxIter && a.AllowDelegation == b.AllowDelegation
}

func sameID(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// checkInputFiles makes sure a work order's input files still exist before a resumed run is queued.
func (l *Launcher) checkInputFiles(ctx context.Context, spec model.RunSpec) error {
	if spec.WorkOrder == nil {
		return nil
	}
	var missing []string
	for _, a := range spec.WorkOrder.Attachments {
		if _, err := l.store.GetAttachment(ctx, a.ID); errors.Is(err, store.ErrNotFound) {
			missing = append(missing, "The input file "+a.Filename+" was deleted.")
		} else if err != nil {
			return err
		}
	}
	if len(missing) > 0 {
		return &ProblemsError{Message: "This cannot run yet.", Details: missing}
	}
	return nil
}

// finishedChanged says which finished tasks changed: "The crew's first task was changed or moved" or "The crew's first
// 3 tasks were changed or reordered".
func finishedChanged(n int) string {
	if n == 1 {
		return "The crew's first task (or its agent) was changed or moved"
	}
	return fmt.Sprintf("The crew's first %d tasks (or their agents) were changed or reordered", n)
}

// requireAny refuses unless the principal holds at least one of the permissions.
func requireAny(p *auth.Principal, perms ...string) error {
	for _, perm := range perms {
		if p.Can(perm) {
			return nil
		}
	}
	return &ForbiddenError{Permission: perms[0]}
}
