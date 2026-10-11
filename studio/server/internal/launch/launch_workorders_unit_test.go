package launch

import (
	"errors"
	"slices"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
	"crewstudio/internal/store"
)

func minimalLibrary(crew model.Crew) (runspec.Library, model.Crew) {
	llm := model.LLM{ID: "l1", Name: "L", Model: "m"}
	agent := model.Agent{ID: "a1", Name: "A", Role: "r", Goal: "g", Backstory: "b", LLMID: &llm.ID}
	task := model.Task{ID: "t1", Name: "T", Description: "d", ExpectedOutput: "e", AgentID: &agent.ID}
	crew.AgentIDs, crew.TaskIDs = []string{agent.ID}, []string{task.ID}
	return runspec.Library{
		LLMs: map[string]model.LLM{llm.ID: llm}, Agents: map[string]model.Agent{agent.ID: agent}, Tasks: map[string]model.Task{task.ID: task},
		Pricing: []model.ModelPrice{{Model: "m"}}, Tools: map[string]model.Tool{},
	}, crew
}

func emptyLib() runspec.Library {
	return runspec.Library{LLMs: map[string]model.LLM{}, Agents: map[string]model.Agent{}, Tasks: map[string]model.Task{}, Tools: map[string]model.Tool{}}
}

func (e *env) wo(title string, input model.WorkOrderInput) model.WorkOrderInput {
	if input.Title == "" {
		input.Title = title
	}
	if input.Brief == "" {
		input.Brief = "brief"
	}
	if input.CrewID == nil {
		input.CrewID = &e.crew.ID
	}
	return input
}

func TestCreateWorkOrderDraftAndSource(t *testing.T) {
	e := newEnv(t)
	session := e.full("pat")
	api := e.principal("bot", pOpts{perms: []string{auth.WorkOrdersCreate}, areas: []string{e.area}, service: true, via: auth.ViaAPIKey})

	c, err := e.l.CreateWorkOrder(bg(), session, e.wo("Draft", model.WorkOrderInput{Inputs: map[string]string{"company": "Acme"}}), WorkOrderSource{}, Idempotency{}, nil)
	ok(t, err)
	wo := c.WorkOrder
	if c.Run != nil || c.Replayed || wo.Status != "draft" || wo.Source != model.SourceManual || wo.TargetName != "Cr" || wo.CreatedBy == nil ||
		*wo.CreatedBy != session.UserID || wo.CreatedByName != "pat" || !slices.Equal(wo.AreaIDs, []string{e.area}) {
		t.Errorf("draft = %+v run=%v", wo, c.Run)
	}
	if e.notifier.n.Load() != 0 {
		t.Error("a draft must not wake the dispatcher")
	}
	// API principals get source=api; an explicit source wins
	c, err = e.l.CreateWorkOrder(bg(), api, e.wo("ByKey", model.WorkOrderInput{}), WorkOrderSource{}, Idempotency{}, nil)
	ok(t, err)
	if c.WorkOrder.Source != model.SourceAPI {
		t.Errorf("source = %s", c.WorkOrder.Source)
	}
	sid := "55555555-0000-4000-8000-0000000000aa"
	c, err = e.l.CreateWorkOrder(bg(), session, e.wo("Sched", model.WorkOrderInput{}), WorkOrderSource{Kind: model.SourceSchedule, ID: sid}, Idempotency{}, nil)
	ok(t, err)
	if c.WorkOrder.Source != model.SourceSchedule || c.WorkOrder.SourceID == nil || *c.WorkOrder.SourceID != sid {
		t.Errorf("scheduled = %+v", c.WorkOrder)
	}
	// a draft with an idempotency key is not deduplicated (only submitted ones are)
	a, _ := e.l.CreateWorkOrder(bg(), session, e.wo("D1", model.WorkOrderInput{}), WorkOrderSource{}, Idempotency{Key: "same"}, nil)
	b, _ := e.l.CreateWorkOrder(bg(), session, e.wo("D2", model.WorkOrderInput{}), WorkOrderSource{}, Idempotency{Key: "same"}, nil)
	if a.WorkOrder.ID == b.WorkOrder.ID {
		t.Error("drafts must not be deduplicated")
	}
}

func TestCreateWorkOrderPermissionsAndTargets(t *testing.T) {
	e := newEnv(t)
	noPerm := e.principal("nope", pOpts{perms: []string{auth.CrewsRun}, areas: []string{e.area}})
	_, err := e.l.CreateWorkOrder(bg(), noPerm, e.wo("x", model.WorkOrderInput{}), WorkOrderSource{}, Idempotency{}, nil)
	wantForbidden(t, err, auth.WorkOrdersCreate)
	_, err = e.l.CreateWorkOrder(bg(), nil, e.wo("x", model.WorkOrderInput{}), WorkOrderSource{}, Idempotency{}, nil)
	wantForbidden(t, err, auth.WorkOrdersCreate)

	beta, _ := e.st.CreateArea(bg(), model.Area{Name: "Beta"})
	outsider := e.principal("out", pOpts{perms: []string{auth.WorkOrdersCreate}, areas: []string{beta.ID}})
	_, err = e.l.CreateWorkOrder(bg(), outsider, e.wo("x", model.WorkOrderInput{}), WorkOrderSource{}, Idempotency{}, nil)
	wantProblems(t, err, "crew_id: no such crew")

	p := e.full("pat")
	_, err = e.l.CreateWorkOrder(bg(), p, model.WorkOrderInput{Title: "t", CrewID: ptr(missingID)}, WorkOrderSource{}, Idempotency{}, nil)
	wantProblems(t, err, "crew_id: no such crew")
	var n int
	_, total, _ := e.st.ListWorkOrders(bg(), store.WorkOrderFilter{Scope: store.AllAreas, Limit: 5})
	n = total
	if n != 0 {
		t.Errorf("%d work orders created despite errors", n)
	}
}

func TestCreateWorkOrderSubmitAndReplay(t *testing.T) {
	e := newEnv(t)
	p := e.full("pat")
	in := e.wo("Go", model.WorkOrderInput{Submit: true, Inputs: map[string]string{"company": "Acme"}, BudgetUSD: ptr(3.0)})

	c, err := e.l.CreateWorkOrder(bg(), p, in, WorkOrderSource{}, Idempotency{Key: "idem-1"}, nil)
	ok(t, err)
	if c.Run == nil || c.Replayed || c.WorkOrder.Status != "queued" || c.Run.WorkOrderID == nil || *c.Run.WorkOrderID != c.WorkOrder.ID ||
		c.Run.BudgetUSD != 3 || c.Run.TriggerType != "work_order" || c.WorkOrder.RunCount != 1 {
		t.Fatalf("created = %+v run=%+v", c.WorkOrder, c.Run)
	}
	if e.notifier.n.Load() != 1 {
		t.Errorf("notifications = %d", e.notifier.n.Load())
	}
	spec, _ := e.st.GetRunSpec(bg(), c.Run.ID)
	if spec.WorkOrder == nil || spec.WorkOrder.ID != c.WorkOrder.ID || spec.WorkOrder.Brief != "brief" || spec.WorkOrder.Number != c.WorkOrder.Number {
		t.Errorf("spec.WorkOrder = %+v", spec.WorkOrder)
	}

	// replay: same key, same principal -> the first work order, nothing new
	again, err := e.l.CreateWorkOrder(bg(), p, in, WorkOrderSource{}, Idempotency{Key: "idem-1"}, nil)
	ok(t, err)
	if !again.Replayed || again.WorkOrder.ID != c.WorkOrder.ID || again.Run == nil || again.Run.ID != c.Run.ID {
		t.Errorf("replay = %+v", again)
	}
	if _, total, _ := e.st.ListWorkOrders(bg(), store.WorkOrderFilter{Scope: store.AllAreas, Limit: 10}); total != 1 {
		t.Errorf("work orders = %d", total)
	}
	if e.notifier.n.Load() != 1 {
		t.Error("replay notified")
	}
	// a replay is answered before target checks, so even a changed body returns the original
	changed := e.wo("Other", model.WorkOrderInput{Submit: true, Inputs: map[string]string{"company": "Other"}})
	again, err = e.l.CreateWorkOrder(bg(), p, changed, WorkOrderSource{}, Idempotency{Key: "idem-1"}, nil)
	ok(t, err)
	if again.WorkOrder.ID != c.WorkOrder.ID {
		t.Error("changed body must still replay")
	}
	// explicit scope shared between principals (e.g. a webhook trigger)
	q := e.full("quinn")
	s1, err := e.l.CreateWorkOrder(bg(), p, in, WorkOrderSource{}, Idempotency{Scope: "trigger:x", Key: "evt-1"}, nil)
	ok(t, err)
	s2, err := e.l.CreateWorkOrder(bg(), q, in, WorkOrderSource{}, Idempotency{Scope: "trigger:x", Key: "evt-1"}, nil)
	ok(t, err)
	if !s2.Replayed || s2.WorkOrder.ID != s1.WorkOrder.ID {
		t.Error("explicit scope not shared")
	}
	// another principal's default scope is separate
	other, err := e.l.CreateWorkOrder(bg(), q, in, WorkOrderSource{}, Idempotency{Key: "idem-1"}, nil)
	ok(t, err)
	if other.Replayed || other.WorkOrder.ID == c.WorkOrder.ID {
		t.Error("default scope must be per principal")
	}
}

func TestCreateWorkOrderKeyReusedByDirectRun(t *testing.T) {
	e := newEnv(t)
	p := e.full("pat")
	_, _, err := e.l.StartCrewRun(bg(), p, CrewRunRequest{CrewID: e.crew.ID, Inputs: map[string]string{"company": "Acme"}, IdempotencyKey: "shared"})
	ok(t, err)
	_, err = e.l.CreateWorkOrder(bg(), p, e.wo("x", model.WorkOrderInput{Submit: true, Inputs: map[string]string{"company": "Acme"}}), WorkOrderSource{}, Idempotency{Key: "shared"}, nil)
	wantProblems(t, err, "Idempotency-Key")
	if _, total, _ := e.st.ListWorkOrders(bg(), store.WorkOrderFilter{Scope: store.AllAreas, Limit: 10}); total != 0 {
		t.Errorf("no work order should be stored, got %d", total)
	}
}

func TestCreateWorkOrderSubmitFailureKeepsDraft(t *testing.T) {
	e := newEnv(t)
	p := e.full("pat")
	// missing required input: submitting fails but the draft is kept so the person can fix it
	c, err := e.l.CreateWorkOrder(bg(), p, e.wo("Missing", model.WorkOrderInput{Submit: true}), WorkOrderSource{}, Idempotency{Key: "k"}, nil)
	wantProblems(t, err, "inputs.company: is required")
	if c.WorkOrder.ID == "" || c.WorkOrder.Status != "draft" || c.Run != nil {
		t.Errorf("draft not returned: %+v", c)
	}
	if _, err := e.st.GetWorkOrder(bg(), c.WorkOrder.ID); err != nil {
		t.Errorf("draft not stored: %v", err)
	}
	if e.notifier.n.Load() != 0 {
		t.Error("failed submit notified")
	}
	// a bad callback from a human is also a submit failure
	c, err = e.l.CreateWorkOrder(bg(), p, e.wo("Cb", model.WorkOrderInput{Submit: true, Inputs: map[string]string{"company": "x"}}), WorkOrderSource{}, Idempotency{},
		&CallbackRequest{URL: "https://example.com/x", Source: store.CallbackFromTrigger, Owner: missingID})
	if err != nil {
		// callbacks given explicitly (by triggers) are accepted for any principal; the owner must exist as a trigger or user only at delivery time
		t.Fatalf("explicit callback: %v", err)
	}
	ds, _ := e.st.ListDeliveries(bg(), c.Run.ID)
	if len(ds) != 1 || ds[0].SecretSource != store.CallbackFromTrigger {
		t.Errorf("deliveries = %+v", ds)
	}
}

func TestCreateWorkOrderConcurrentIdempotentSubmits(t *testing.T) {
	e := newEnv(t)
	p := e.full("pat")
	in := e.wo("Race", model.WorkOrderInput{Submit: true, Inputs: map[string]string{"company": "Acme"}})
	type res struct {
		c   Created
		err error
	}
	results := make([]res, 8)
	done := make(chan int, len(results))
	start := make(chan struct{})
	for i := range results {
		go func(i int) {
			<-start
			c, err := e.l.CreateWorkOrder(bg(), p, in, WorkOrderSource{}, Idempotency{Key: "race"}, nil)
			results[i] = res{c, err}
			done <- i
		}(i)
	}
	close(start)
	for range results {
		<-done
	}
	ids := map[string]bool{}
	for i, r := range results {
		if r.err != nil {
			t.Errorf("call %d: %v", i, r.err)
			continue
		}
		ids[r.c.WorkOrder.ID] = true
		if r.c.Run == nil {
			t.Errorf("call %d has no run", i)
		}
	}
	if len(ids) != 1 {
		t.Errorf("callers saw %d different work orders, want 1", len(ids))
	}
	// duplicate drafts were deleted
	_, total, _ := e.st.ListWorkOrders(bg(), store.WorkOrderFilter{Scope: store.AllAreas, Limit: 50})
	if total != 1 {
		t.Errorf("%d work orders remain, want exactly 1", total)
	}
	runs, _, _ := e.st.ListRuns(bg(), store.RunFilter{Scope: store.AllAreas, Limit: 50})
	if len(runs) != 1 {
		t.Errorf("%d runs, want 1", len(runs))
	}
}

func TestSubmitWorkOrder(t *testing.T) {
	e := newEnv(t)
	p := e.full("pat")
	wo := e.draftWorkOrder(p)

	// permissions and visibility
	noPerm := e.principal("nope", pOpts{perms: []string{auth.CrewsRun}, areas: []string{e.area}})
	_, _, err := e.l.SubmitWorkOrder(bg(), noPerm, wo.ID, SubmitOptions{})
	wantForbidden(t, err, auth.WorkOrdersCreate)
	beta, _ := e.st.CreateArea(bg(), model.Area{Name: "Beta"})
	outsider := e.principal("out", pOpts{perms: []string{auth.WorkOrdersCreate}, areas: []string{beta.ID}})
	if _, _, err = e.l.SubmitWorkOrder(bg(), outsider, wo.ID, SubmitOptions{}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("outsider: %v", err)
	}
	if _, _, err = e.l.SubmitWorkOrder(bg(), p, missingID, SubmitOptions{}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
	if e.notifier.n.Load() != 0 {
		t.Fatal("refusals notified")
	}

	// budget: the option beats the work order's own, which beats the crew's
	run, existing, err := e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{BudgetUSD: ptr(9.0)})
	ok(t, err)
	if existing || run.BudgetUSD != 9 || run.WorkOrderID == nil || *run.WorkOrderID != wo.ID || run.CreatedBy == nil || *run.CreatedBy != p.UserID {
		t.Errorf("run = %+v", run)
	}
	// busy while queued or running
	if _, _, err = e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{}); !errors.Is(err, store.ErrWorkOrderActive) {
		t.Errorf("queued resubmit: %v", err)
	}
	started, _ := e.st.StartRun(bg(), run.ID)
	if !started {
		t.Fatal("start")
	}
	if _, _, err = e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{}); !errors.Is(err, store.ErrWorkOrderActive) {
		t.Errorf("running resubmit: %v", err)
	}
	// after a failure the work order can be tried again; the crew's budget applies now
	_, err = e.st.FinishRun(bg(), run.ID, store.RunResult{Status: model.RunFailed})
	ok(t, err)
	run2, _, err := e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{})
	ok(t, err)
	if run2.ID == run.ID || run2.BudgetUSD != 2 {
		t.Errorf("retry = %+v", run2)
	}
	if e.notifier.n.Load() != 2 {
		t.Errorf("notifications = %d, want 2", e.notifier.n.Load())
	}
	// bad budget override
	_, err = e.st.FinishRun(bg(), run2.ID, store.RunResult{Status: model.RunSucceeded})
	ok(t, err)
	_, _, err = e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{BudgetUSD: ptr(5000.0)})
	wantProblems(t, err, "budget_usd:")
}

func TestSubmitWorkOrderIdempotentReplay(t *testing.T) {
	e := newEnv(t)
	p := e.full("pat")
	wo := e.draftWorkOrder(p)
	r1, existing, err := e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{IdempotencyKey: "sub-1"})
	ok(t, err)
	if existing {
		t.Fatal("first")
	}
	r2, existing, err := e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{IdempotencyKey: "sub-1"})
	// the work order is active, so the busy error wins over the replay when nothing finished...
	if err == nil {
		if !existing || r2.ID != r1.ID {
			t.Errorf("replay = %+v existing=%v", r2, existing)
		}
	} else if !errors.Is(err, store.ErrWorkOrderActive) {
		t.Errorf("replay error: %v", err)
	}
	// once finished, the same key replays the first run instead of starting another
	_, err = e.st.FinishRun(bg(), r1.ID, store.RunResult{Status: model.RunFailed})
	ok(t, err)
	r3, existing, err := e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{IdempotencyKey: "sub-1"})
	ok(t, err)
	if !existing || r3.ID != r1.ID {
		t.Errorf("replay after finish = %+v existing=%v", r3, existing)
	}
}

func TestSubmitWorkOrderCallbacks(t *testing.T) {
	e := newEnv(t)
	human := e.full("pat")
	svc := e.principal("bot", pOpts{perms: []string{auth.WorkOrdersCreate, auth.CrewsRun}, areas: []string{e.area}, service: true, via: auth.ViaAPIKey})
	wo := e.draftWorkOrder(human)

	_, _, err := e.l.SubmitWorkOrder(bg(), human, wo.ID, SubmitOptions{CallbackURL: "https://example.com/cb"})
	wantProblems(t, err, "service accounts")
	run, _, err := e.l.SubmitWorkOrder(bg(), svc, wo.ID, SubmitOptions{CallbackURL: "https://example.com/cb"})
	ok(t, err)
	ds, _ := e.st.ListDeliveries(bg(), run.ID)
	if len(ds) != 1 || ds[0].SecretSource != store.CallbackFromServiceAccount || ds[0].SecretOwnerID != svc.UserID {
		t.Errorf("deliveries = %+v", ds)
	}
	_, err = e.st.FinishRun(bg(), run.ID, store.RunResult{Status: model.RunSucceeded})
	ok(t, err)
	// an explicit Callback (from a trigger) takes precedence over CallbackURL
	run, _, err = e.l.SubmitWorkOrder(bg(), human, wo.ID, SubmitOptions{Callback: &CallbackRequest{URL: "https://example.com/t", Source: store.CallbackFromTrigger, Owner: missingID}, CallbackURL: "https://ignored.example.com"})
	ok(t, err)
	ds, _ = e.st.ListDeliveries(bg(), run.ID)
	if len(ds) != 1 || ds[0].URL != "https://example.com/t" || ds[0].SecretSource != store.CallbackFromTrigger {
		t.Errorf("deliveries = %+v", ds)
	}
}

func TestSubmitWorkOrderTargetsGoneOrBroken(t *testing.T) {
	e := newEnv(t)
	p := e.full("pat")

	// crew deleted after the work order was made
	crew2, err := e.st.CreateCrew(bg(), model.Crew{Name: "Temp", Process: model.ProcessSequential, BudgetUSD: 1, AgentIDs: []string{e.agent.ID}, TaskIDs: []string{e.task.ID},
		AreaIDs: []string{e.area}, Inputs: []model.CrewInput{{Key: "company", Required: true}}})
	ok(t, err)
	wo, err := e.l.CreateWorkOrder(bg(), p, model.WorkOrderInput{Title: "t", CrewID: &crew2.ID, Inputs: map[string]string{"company": "x"}}, WorkOrderSource{}, Idempotency{}, nil)
	ok(t, err)
	ok(t, e.st.DeleteCrew(bg(), crew2.ID))
	_, _, err = e.l.SubmitWorkOrder(bg(), p, wo.WorkOrder.ID, SubmitOptions{})
	wantProblems(t, err, "crew this work order targets was deleted")

	// a crew whose agent has no LLM cannot run
	bare, _ := e.st.CreateAgent(bg(), model.Agent{Name: "Bare", Role: "r", Goal: "g", Backstory: "b", MaxIter: 2, Tools: []string{}, AreaIDs: []string{e.area}})
	bt, _ := e.st.CreateTask(bg(), model.Task{Name: "BT", Description: "d", ExpectedOutput: "e", AgentID: &bare.ID, AreaIDs: []string{e.area}})
	bc, err := e.st.CreateCrew(bg(), model.Crew{Name: "BC", Process: model.ProcessSequential, BudgetUSD: 1, AgentIDs: []string{bare.ID},
		TaskIDs: []string{bt.ID}, AreaIDs: []string{e.area}, Inputs: []model.CrewInput{}})
	ok(t, err)
	woB, err := e.l.CreateWorkOrder(bg(), p, model.WorkOrderInput{Title: "tb", CrewID: &bc.ID}, WorkOrderSource{}, Idempotency{}, nil)
	ok(t, err)
	_, _, err = e.l.SubmitWorkOrder(bg(), p, woB.WorkOrder.ID, SubmitOptions{})
	wantProblems(t, err, "has no LLM")

	// a principal who lost access to the crew's area sees it as deleted, not as an authorization hint
	beta, _ := e.st.CreateArea(bg(), model.Area{Name: "Beta"})
	moved := e.principal("moved", pOpts{perms: []string{auth.WorkOrdersCreate}, areas: []string{beta.ID, e.area}})
	woM := e.draftWorkOrder(moved)
	moved.AreaIDs = []string{beta.ID}
	_, _, err = e.l.SubmitWorkOrder(bg(), moved, woM.ID, SubmitOptions{})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("area lost: %v", err)
	}
}

func TestSubmitWorkOrderAttachmentsInSpec(t *testing.T) {
	e := newEnv(t)
	p := e.full("pat")
	wo := e.draftWorkOrder(p)
	for i, name := range []string{"../notes.txt", "notes.txt", "data.csv"} {
		id := auth.NewUUID()
		_, err := e.st.AddInputAttachment(bg(), model.Attachment{ID: id, WorkOrderID: &wo.ID, Filename: name, ContentType: "text/plain", SizeBytes: int64(10 + i),
			SHA256: "x", StorageKey: "k/" + id}, 10, 1000)
		ok(t, err)
	}
	// artifacts never go to the next run's workspace
	run0, _, err := e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{})
	ok(t, err)
	spec, _ := e.st.GetRunSpec(bg(), run0.ID)
	if spec.WorkOrder == nil || len(spec.WorkOrder.Attachments) != 3 {
		t.Fatalf("spec = %+v", spec.WorkOrder)
	}
	var paths []string
	for _, a := range spec.WorkOrder.Attachments {
		paths = append(paths, a.Path)
	}
	slices.Sort(paths)
	if !slices.Equal(paths, []string{"attachments/data.csv", "attachments/notes-2.txt", "attachments/notes.txt"}) {
		t.Errorf("paths = %v", paths)
	}
	art := auth.NewUUID()
	_, err = e.st.AddAttachment(bg(), model.Attachment{ID: art, RunID: &run0.ID, Kind: model.AttachmentArtifact, Filename: "out.md", ContentType: "x", SHA256: "x", StorageKey: "k/" + art})
	ok(t, err)
	_, err = e.st.FinishRun(bg(), run0.ID, store.RunResult{Status: model.RunSucceeded})
	ok(t, err)
	run1, _, err := e.l.SubmitWorkOrder(bg(), p, wo.ID, SubmitOptions{})
	ok(t, err)
	spec, _ = e.st.GetRunSpec(bg(), run1.ID)
	if len(spec.WorkOrder.Attachments) != 3 {
		t.Errorf("artifacts leaked into the next run: %d attachments", len(spec.WorkOrder.Attachments))
	}
}
