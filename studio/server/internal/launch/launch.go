// Package launch starts runs and work orders. The API, the scheduler and webhook triggers all go through it,
// so permission and area checks, run checks and spec freezing happen in one place.
package launch

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
)

const maxRunBudgetUSD = 1000

// Notifier wakes the run dispatcher.
type Notifier interface{ Notify() }

// ToolCatalog lists the tools agents can use.
type ToolCatalog interface {
	ByKey() (map[string]model.Tool, error)
}

// ProblemsError means the request is understood but cannot run yet; Details say why.
type ProblemsError struct {
	Message string
	Details []string
}

func (e *ProblemsError) Error() string { return e.Message + ": " + strings.Join(e.Details, "; ") }

// ForbiddenError means the principal lacks a permission.
type ForbiddenError struct{ Permission string }

func (e *ForbiddenError) Error() string { return "missing permission " + e.Permission }

// MemoryEmbeddings says whether the server can embed memories, and with which model.
type MemoryEmbeddings interface {
	Available() (bool, string)
	Model() string
}

type Launcher struct {
	store *store.Store
	tools ToolCatalog
	runs  Notifier
	// memory, when available, lets runs of crews with memory on get it in their spec
	memory MemoryEmbeddings
}

func New(st *store.Store, tools ToolCatalog, runs Notifier) *Launcher {
	return &Launcher{store: st, tools: tools, runs: runs}
}

// WithMemory turns crew memory on for runs this launcher starts, while m can embed. Without it memory is left out
// of every spec.
func (l *Launcher) WithMemory(m MemoryEmbeddings) *Launcher {
	l.memory = m
	return l
}

// freezeMemory adds the crew's memory settings to spec when memory is on and the server can embed. A model memory
// needs that has no price stops the run, like any other unpriced model.
func (l *Launcher) freezeMemory(ctx context.Context, crew model.Crew, lib runspec.Library, spec *model.RunSpec) error {
	if l.memory == nil || crew.ID == "" {
		return nil
	}
	if ok, _ := l.memory.Available(); !ok {
		return nil
	}
	settings, err := l.store.MemorySettings(ctx, crew.ID)
	if err != nil {
		return err
	}
	if problems := runspec.FreezeMemory(spec, settings, lib, l.memory.Model()); len(problems) > 0 {
		return &ProblemsError{Message: "This cannot run yet.", Details: problems}
	}
	return nil
}

// freezeEscalation adds the crew's escalation rules to spec when escalation is on, so its agents can ask a person.
func (l *Launcher) freezeEscalation(ctx context.Context, crew model.Crew, spec *model.RunSpec) error {
	if crew.ID == "" {
		return nil
	}
	settings, err := l.store.EscalationSettings(ctx, crew.ID)
	if err != nil || !settings.Enabled {
		return err
	}
	spec.Escalation = &model.EscalationSpec{
		Rules: settings.Rules, TimeoutMinutes: settings.TimeoutMinutes, OnTimeout: settings.OnTimeout, MaxQuestions: settings.MaxQuestions,
	}
	return nil
}

// MissingProviderKeys maps each model whose provider key is not set to that key's variable name (see
// runspec.MissingProviderKeys). A tool catalog that cannot tell (it does not read the provider env file) reports none.
func (l *Launcher) MissingProviderKeys(models ...string) (map[string]string, error) {
	checker, ok := l.tools.(runspec.KeyChecker)
	if !ok {
		return map[string]string{}, nil
	}
	return runspec.MissingProviderKeys(checker, models)
}

// Library gathers everything a crew can reference, for run checks and spec freezing.
func (l *Launcher) Library(ctx context.Context) (runspec.Library, error) {
	lib := runspec.Library{LLMs: map[string]model.LLM{}, Agents: map[string]model.Agent{}, Tasks: map[string]model.Task{}}
	llms, err := l.store.ListLLMs(ctx)
	if err != nil {
		return lib, err
	}
	for _, x := range llms {
		lib.LLMs[x.ID] = x
	}
	agents, err := l.store.ListAgents(ctx, store.AllAreas)
	if err != nil {
		return lib, err
	}
	for _, a := range agents {
		lib.Agents[a.ID] = a
	}
	tasks, err := l.store.ListTasks(ctx, store.AllAreas)
	if err != nil {
		return lib, err
	}
	for _, t := range tasks {
		lib.Tasks[t.ID] = t
	}
	if lib.Pricing, err = l.store.ListPrices(ctx); err != nil {
		return lib, err
	}
	if lib.Settings, err = l.store.GetSettings(ctx); err != nil {
		return lib, err
	}
	if lib.Tools, err = l.tools.ByKey(); err != nil {
		return lib, err
	}
	if lib.Crews, err = l.store.ListCrews(ctx, store.AllAreas); err != nil {
		return lib, err
	}
	return lib, nil
}

func require(p *auth.Principal, perms ...string) error {
	for _, perm := range perms {
		if !p.Can(perm) {
			return &ForbiddenError{Permission: perm}
		}
	}
	return nil
}

// CallbackRequest asks for a signed POST when the run finishes.
type CallbackRequest struct {
	URL string
	// Source and Owner say which secret signs it: a trigger, or the calling service account
	Source string
	Owner  string
}

// callbackFor checks a caller-supplied callback URL: only service accounts may ask for one.
func callbackFor(p *auth.Principal, url string) (*store.Callback, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, nil
	}
	if p.Kind != auth.KindService {
		return nil, &ProblemsError{Message: "Callbacks are for service accounts.",
			Details: []string{"callback_url: only API keys of service accounts can request callbacks"}}
	}
	if problem := model.CheckCallbackURL(url); problem != "" {
		return nil, &ProblemsError{Message: "The callback URL is not usable.", Details: []string{"callback_url: " + problem}}
	}
	return &store.Callback{URL: url, SecretSource: store.CallbackFromServiceAccount, SecretOwner: p.UserID}, nil
}

func idempotencyScope(p *auth.Principal) string { return "user:" + p.UserID }

// CrewRunRequest starts a crew directly (the classic "Run" button and POST /crews/{id}/runs).
type CrewRunRequest struct {
	CrewID         string
	Inputs         map[string]string
	BudgetUSD      *float64
	TriggerType    string
	IdempotencyKey string
	CallbackURL    string
}

// StartCrewRun checks permissions, areas and the crew, then queues a run. existing is true when an
// idempotency key matched an earlier run.
func (l *Launcher) StartCrewRun(ctx context.Context, p *auth.Principal, req CrewRunRequest) (model.Run, bool, error) {
	if err := require(p, auth.CrewsRun); err != nil {
		return model.Run{}, false, err
	}
	crew, err := l.store.GetCrew(ctx, req.CrewID)
	if err != nil {
		return model.Run{}, false, err
	}
	if !p.SeesAreas(crew.AreaIDs) {
		return model.Run{}, false, store.ErrNotFound
	}
	cb, err := callbackFor(p, req.CallbackURL)
	if err != nil {
		return model.Run{}, false, err
	}
	lib, err := l.Library(ctx)
	if err != nil {
		return model.Run{}, false, err
	}
	spec, err := freeze(crew, lib, req.Inputs, req.BudgetUSD, crew.BudgetUSD, false)
	if err == nil {
		err = l.freezeMemory(ctx, crew, lib, &spec)
	}
	if err == nil {
		err = l.freezeEscalation(ctx, crew, &spec)
	}
	if err != nil {
		return model.Run{}, false, err
	}
	meta := store.RunMeta{
		AreaIDs: crew.AreaIDs, CreatedBy: p.UserID, CreatedByName: p.Name, TriggerType: req.TriggerType,
		IdempotencyKey: req.IdempotencyKey, Callback: cb,
	}
	if req.IdempotencyKey != "" {
		meta.IdempotencyScope = idempotencyScope(p)
	}
	run, existing, err := l.store.CreateRun(ctx, spec, meta)
	if err == nil && existing && (run.WorkOrderID != nil || run.CrewID == nil || *run.CrewID != crew.ID) {
		return model.Run{}, false, errKeyReused
	}
	if err == nil && !existing {
		l.runs.Notify()
	}
	return run, existing, err
}

// freeze checks the crew can run with these inputs and budget and builds its spec.
// watched allows the inputs a schedule's watch adds; only work orders a schedule created may carry them.
func freeze(crew model.Crew, lib runspec.Library, inputs map[string]string, budget *float64, fallback float64, watched bool) (model.RunSpec, error) {
	problems := runspec.Check(crew, lib)
	resolve := runspec.ResolveInputs
	if watched {
		resolve = runspec.ResolveWatchedInputs
	}
	resolved, inputProblems := resolve(crew, inputs)
	problems = append(problems, inputProblems...)
	b := fallback
	if budget != nil {
		b = *budget
	}
	if b <= 0 || b > maxRunBudgetUSD {
		problems = append(problems, fmt.Sprintf("budget_usd: must be more than 0 and at most %d", maxRunBudgetUSD))
	}
	selected, selectProblems := runspec.SelectTasks(crew, lib, resolved[model.InputRunTasks])
	problems = append(problems, selectProblems...)
	if len(problems) > 0 {
		return model.RunSpec{}, &ProblemsError{Message: "This cannot run yet.", Details: problems}
	}
	return runspec.Build(selected, lib, resolved, b), nil
}

// Target is the resolved crew of a work order, schedule or trigger.
type Target struct {
	Name    string
	AreaIDs []string
}

// ResolveTarget loads the crew and checks the principal can see it.
func (l *Launcher) ResolveTarget(ctx context.Context, p *auth.Principal, crewID *string) (Target, error) {
	if crewID == nil {
		return Target{}, &ProblemsError{Message: "Please fix the highlighted fields.", Details: []string{"crew_id: pick a crew"}}
	}
	crew, err := l.store.GetCrew(ctx, *crewID)
	if err != nil {
		return Target{}, notFoundAs(err, "crew_id: no such crew")
	}
	if !p.SeesAreas(crew.AreaIDs) {
		return Target{}, &ProblemsError{Message: "Please fix the highlighted fields.", Details: []string{"crew_id: no such crew"}}
	}
	return Target{Name: crew.Name, AreaIDs: crew.AreaIDs}, nil
}

func notFoundAs(err error, detail string) error {
	if errors.Is(err, store.ErrNotFound) {
		return &ProblemsError{Message: "Please fix the highlighted fields.", Details: []string{detail}}
	}
	return err
}

// WorkOrderSource records where a work order came from.
type WorkOrderSource struct {
	Kind string // manual, api, schedule, webhook
	ID   string
}

// Idempotency makes retried requests safe: the same Key in the same Scope returns the first result.
// An empty Scope means the calling principal.
type Idempotency struct {
	Scope string
	Key   string
}

// Created is the result of CreateWorkOrder.
type Created struct {
	WorkOrder model.WorkOrder
	// Run is set when the work order was submitted
	Run *model.Run
	// Replayed means an earlier request with the same idempotency key already created it
	Replayed bool
}

// CreateWorkOrder stores a work order (and submits it when in.Submit). With an idempotency key and submit,
// a repeated request returns the work order created the first time. When submitting fails, the work order
// is kept as a draft and returned along with the error.
func (l *Launcher) CreateWorkOrder(ctx context.Context, p *auth.Principal, in model.WorkOrderInput, src WorkOrderSource,
	idem Idempotency, callback *CallbackRequest) (Created, error) {
	if err := require(p, auth.WorkOrdersCreate); err != nil {
		return Created{}, err
	}
	if idem.Scope == "" {
		idem.Scope = idempotencyScope(p)
	}
	if in.Submit && idem.Key != "" {
		if prior, ok, err := l.priorWorkOrder(ctx, idem.Scope, idem.Key); err != nil || ok {
			return prior, err
		}
	}
	target, err := l.ResolveTarget(ctx, p, in.CrewID)
	if err != nil {
		return Created{}, err
	}
	if src.Kind == "" {
		src.Kind = model.SourceManual
		if p.Via == auth.ViaAPIKey {
			src.Kind = model.SourceAPI
		}
	}
	wo, err := l.store.CreateWorkOrder(ctx, in, store.WorkOrderMeta{
		TargetName: target.Name, AreaIDs: target.AreaIDs, Source: src.Kind, SourceID: src.ID,
		CreatedBy: p.UserID, CreatedByName: p.Name,
	})
	if err != nil || !in.Submit {
		return Created{WorkOrder: wo}, err
	}
	run, existing, err := l.submit(ctx, p, wo, SubmitOptions{IdempotencyScope: idem.Scope, IdempotencyKey: idem.Key, Callback: callback})
	if err != nil {
		return Created{WorkOrder: wo}, err
	}
	if existing && (run.WorkOrderID == nil || *run.WorkOrderID != wo.ID) {
		// An identical request won the race: drop this duplicate draft and answer with the first one
		if derr := l.store.DeleteWorkOrder(ctx, wo.ID); derr != nil {
			return Created{}, derr
		}
		prior, _, err := l.priorWorkOrder(ctx, idem.Scope, idem.Key)
		return prior, err
	}
	wo, err = l.store.GetWorkOrder(ctx, wo.ID)
	return Created{WorkOrder: wo, Run: &run}, err
}

func (l *Launcher) priorWorkOrder(ctx context.Context, scope, key string) (Created, bool, error) {
	run, err := l.store.FindRunByIdempotency(ctx, scope, key)
	if errors.Is(err, store.ErrNotFound) {
		return Created{}, false, nil
	}
	if err != nil {
		return Created{}, false, err
	}
	if run.WorkOrderID == nil {
		return Created{}, false, errKeyReused
	}
	wo, err := l.store.GetWorkOrder(ctx, *run.WorkOrderID)
	if err != nil {
		return Created{}, false, err
	}
	return Created{WorkOrder: wo, Run: &run, Replayed: true}, true, nil
}

var errKeyReused = &ProblemsError{Message: "That Idempotency-Key was already used for a different request.",
	Details: []string{"Idempotency-Key: already used for a different request"}}

// SubmitOptions tune one submit.
type SubmitOptions struct {
	BudgetUSD        *float64
	IdempotencyScope string
	IdempotencyKey   string
	Callback         *CallbackRequest
	CallbackURL      string // from an API caller; turned into a Callback for service accounts
	// Revision, when set, makes the attempt revise an earlier one with someone's feedback
	Revision *model.RevisionSpec
}

// SubmitWorkOrder starts a new attempt of a work order the principal can see.
func (l *Launcher) SubmitWorkOrder(ctx context.Context, p *auth.Principal, workOrderID string, opts SubmitOptions) (model.Run, bool, error) {
	if err := require(p, auth.WorkOrdersCreate); err != nil {
		return model.Run{}, false, err
	}
	wo, err := l.store.GetWorkOrder(ctx, workOrderID)
	if err != nil {
		return model.Run{}, false, err
	}
	if !p.SeesAreas(wo.AreaIDs) {
		return model.Run{}, false, store.ErrNotFound
	}
	if opts.IdempotencyKey != "" && opts.IdempotencyScope == "" {
		opts.IdempotencyScope = idempotencyScope(p)
	}
	return l.submit(ctx, p, wo, opts)
}

func (l *Launcher) submit(ctx context.Context, p *auth.Principal, wo model.WorkOrder, opts SubmitOptions) (model.Run, bool, error) {
	if wo.Status == string(model.RunQueued) || wo.Status == string(model.RunRunning) {
		return model.Run{}, false, store.ErrWorkOrderActive
	}
	var cb *store.Callback
	if opts.Callback != nil {
		cb = &store.Callback{URL: opts.Callback.URL, SecretSource: opts.Callback.Source, SecretOwner: opts.Callback.Owner}
	} else {
		var err error
		if cb, err = callbackFor(p, opts.CallbackURL); err != nil {
			return model.Run{}, false, err
		}
	}
	lib, err := l.Library(ctx)
	if err != nil {
		return model.Run{}, false, err
	}
	crew, err := l.workOrderCrew(ctx, p, wo)
	if err != nil {
		return model.Run{}, false, err
	}
	budget := opts.BudgetUSD
	if budget == nil {
		budget = wo.BudgetUSD
	}
	spec, err := freeze(crew, lib, wo.Inputs, budget, crew.BudgetUSD, wo.Source == model.SourceSchedule)
	if err == nil {
		err = l.freezeMemory(ctx, crew, lib, &spec)
	}
	if err == nil {
		err = l.freezeEscalation(ctx, crew, &spec)
	}
	if err != nil {
		return model.Run{}, false, err
	}
	attachments, err := l.store.ListAttachments(ctx, store.AttachmentFilter{WorkOrderID: wo.ID, Kind: model.AttachmentInput})
	if err != nil {
		return model.Run{}, false, err
	}
	spec.WorkOrder = workOrderSpec(wo, attachments)
	spec.WorkOrder.Revision = opts.Revision
	run, existing, err := l.store.CreateRun(ctx, spec, store.RunMeta{
		AreaIDs: wo.AreaIDs, CreatedBy: p.UserID, CreatedByName: p.Name, TriggerType: "work_order", WorkOrderID: wo.ID,
		IdempotencyScope: opts.IdempotencyScope, IdempotencyKey: opts.IdempotencyKey, Callback: cb,
	})
	if err == nil && !existing {
		l.runs.Notify()
	}
	return run, existing, err
}

// workOrderCrew returns the crew a work order runs.
func (l *Launcher) workOrderCrew(ctx context.Context, p *auth.Principal, wo model.WorkOrder) (model.Crew, error) {
	gone := &ProblemsError{Message: "This cannot run yet.", Details: []string{"The crew this work order targets was deleted."}}
	if wo.CrewID == nil {
		return model.Crew{}, gone
	}
	crew, err := l.store.GetCrew(ctx, *wo.CrewID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !p.SeesAreas(crew.AreaIDs)) {
		return model.Crew{}, gone
	}
	return crew, err
}

// AttachmentsDir is where input files go inside a run's workspace.
const AttachmentsDir = "attachments"

func workOrderSpec(wo model.WorkOrder, attachments []model.Attachment) *model.WorkOrderSpec {
	return &model.WorkOrderSpec{ID: wo.ID, Number: wo.Number, Title: wo.Title, Brief: wo.Brief, Attachments: SpecAttachments(attachments)}
}

// SpecAttachments freezes stored files as a run's input files, each at its own path under AttachmentsDir.
func SpecAttachments(attachments []model.Attachment) []model.SpecAttachment {
	out := make([]model.SpecAttachment, 0, len(attachments))
	used := map[string]bool{}
	for _, a := range attachments {
		out = append(out, model.SpecAttachment{
			ID: a.ID, Filename: a.Filename, ContentType: a.ContentType, SizeBytes: a.SizeBytes,
			Path: path.Join(AttachmentsDir, uniqueName(SafeFilename(a.Filename), used)),
		})
	}
	return out
}

// SafeFilename keeps a file name readable but safe as a single path element.
func SafeFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '.' || r == '-' || r == '_' || r == ' ' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(strings.TrimSpace(b.String()), ".")
	if out == "" {
		out = "file"
	}
	if len(out) > 120 {
		ext := path.Ext(out)
		if len(ext) > 10 {
			ext = ""
		}
		out = out[:120-len(ext)] + ext
	}
	return out
}

func uniqueName(name string, used map[string]bool) string {
	candidate := name
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 2; used[strings.ToLower(candidate)]; n++ {
		candidate = fmt.Sprintf("%s-%d%s", stem, n, ext)
	}
	used[strings.ToLower(candidate)] = true
	return candidate
}

// RunAs loads the person a schedule or trigger acts for, checking they can still create work orders.
func (l *Launcher) RunAs(ctx context.Context, userID *string) (*auth.Principal, error) {
	if userID == nil {
		return nil, &ProblemsError{Message: "Nobody to run as.", Details: []string{"The person who set this up no longer exists; save it again to run as yourself."}}
	}
	p, err := l.store.LoadPrincipal(ctx, *userID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, &ProblemsError{Message: "Nobody to run as.", Details: []string{"The person this runs as is disabled or deleted; save it again to run as yourself."}}
	}
	if err != nil {
		return nil, err
	}
	p.Via = auth.ViaSystem
	return p, nil
}
