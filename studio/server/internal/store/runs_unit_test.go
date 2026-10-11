package store

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

const missingID = "99999999-0000-4000-8000-000000000000"

func TestCreateRunIdempotency(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	meta := RunMeta{IdempotencyScope: "user:1", IdempotencyKey: "key-1", TriggerType: "api"}

	first, existing, err := f.s.CreateRun(bg(), f.spec(crew), meta)
	mustOK(t, err)
	if existing || first.TriggerType != "api" || first.Status != model.RunQueued {
		t.Fatalf("first = %+v existing=%v", first, existing)
	}
	again, existing, err := f.s.CreateRun(bg(), f.spec(crew), meta)
	mustOK(t, err)
	if !existing || again.ID != first.ID {
		t.Errorf("repeat = %v existing=%v", again.ID, existing)
	}
	// different scope, same key: a different request
	other, existing, err := f.s.CreateRun(bg(), f.spec(crew), RunMeta{IdempotencyScope: "user:2", IdempotencyKey: "key-1"})
	mustOK(t, err)
	if existing || other.ID == first.ID {
		t.Error("scopes must not share keys")
	}
	// no key: never deduplicated
	a := f.run(crew, RunMeta{})
	b := f.run(crew, RunMeta{})
	if a.ID == b.ID || a.TriggerType != "manual" {
		t.Errorf("a=%+v", a)
	}
	got, err := f.s.FindRunByIdempotency(bg(), "user:1", "key-1")
	mustOK(t, err)
	if got.ID != first.ID {
		t.Error("FindRunByIdempotency")
	}
	_, err = f.s.FindRunByIdempotency(bg(), "user:1", "other")
	wantNotFound(t, err)

	n := 0
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT count(*) FROM runs`).Scan(&n))
	if n != 4 {
		t.Errorf("runs = %d, want 4", n)
	}
}

func TestCreateRunIdempotencyUnderRace(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	type res struct {
		id       string
		existing bool
		err      error
	}
	results := parallel(8, func(int) res {
		r, ex, err := f.s.CreateRun(bg(), f.spec(crew), RunMeta{IdempotencyScope: "s", IdempotencyKey: "race"})
		return res{r.ID, ex, err}
	})
	created, ids := 0, map[string]bool{}
	for _, r := range results {
		if r.err != nil {
			t.Fatalf("error: %v", r.err)
		}
		ids[r.id] = true
		if !r.existing {
			created++
		}
	}
	if created != 1 || len(ids) != 1 {
		t.Errorf("created %d runs, %d distinct ids; want 1 and 1", created, len(ids))
	}
}

func TestCreateRunStoresSpecAreasAndCreator(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	crew := f.simpleCrew("c", a)
	uid := f.admin()
	run := f.run(crew, RunMeta{AreaIDs: crew.AreaIDs, CreatedBy: uid, CreatedByName: "Admin"})
	if run.CrewID == nil || *run.CrewID != crew.ID || run.CrewName != "c" || run.CreatedBy == nil || *run.CreatedBy != uid ||
		!slices.Equal(run.AreaIDs, []string{a}) || run.BudgetUSD != 1 {
		t.Errorf("run = %+v", run)
	}
	spec := must(f.s.GetRunSpec(bg(), run.ID))
	if spec.Crew.ID != crew.ID || spec.Inputs["k"] != "v" {
		t.Errorf("spec = %+v", spec)
	}
	_, err := f.s.GetRunSpec(bg(), missingID)
	wantNotFound(t, err)
	_, err = f.s.GetRun(bg(), missingID)
	wantNotFound(t, err)
	_, err = f.s.GetRunDetail(bg(), missingID)
	wantNotFound(t, err)
	// bad trigger type is rejected by the database
	if _, _, err := f.s.CreateRun(bg(), f.spec(crew), RunMeta{TriggerType: "carrier-pigeon"}); err == nil {
		t.Error("unknown trigger type accepted")
	}
}

func TestCreateRunWithWorkOrderLocksAndLinks(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	wo := f.workOrder("wo", crew)
	if wo.Status != "draft" || wo.RunCount != 0 || wo.CurrentRunID != nil || wo.Number == 0 {
		t.Errorf("draft = %+v", wo)
	}

	r1 := f.run(crew, RunMeta{WorkOrderID: wo.ID, TriggerType: "work_order"})
	got := must(f.s.GetWorkOrder(bg(), wo.ID))
	if got.Status != "queued" || got.CurrentRunID == nil || *got.CurrentRunID != r1.ID || got.SubmittedAt == nil || got.RunCount != 1 {
		t.Errorf("after submit = %+v", got)
	}
	// a second submit while queued or running is refused
	_, _, err := f.s.CreateRun(bg(), f.spec(crew), RunMeta{WorkOrderID: wo.ID})
	if !errors.Is(err, ErrWorkOrderActive) {
		t.Errorf("queued: %v", err)
	}
	started, _ := f.s.StartRun(bg(), r1.ID)
	if !started {
		t.Fatal("start")
	}
	_, _, err = f.s.CreateRun(bg(), f.spec(crew), RunMeta{WorkOrderID: wo.ID})
	if !errors.Is(err, ErrWorkOrderActive) {
		t.Errorf("running: %v", err)
	}
	wantConflict(t, f.s.EnsureWorkOrderIdle(bg(), wo.ID), "queued or running")
	f.finish(r1.ID, model.RunFailed)
	mustOK(t, f.s.EnsureWorkOrderIdle(bg(), wo.ID))
	r2 := f.run(crew, RunMeta{WorkOrderID: wo.ID})
	if got := must(f.s.GetWorkOrder(bg(), wo.ID)); got.RunCount != 2 || *got.CurrentRunID != r2.ID {
		t.Errorf("retry = %+v", got)
	}
	// the failed attempt keeps its slot after submittedAt was first set
	if got := must(f.s.GetWorkOrder(bg(), wo.ID)); got.SubmittedAt == nil {
		t.Error("submitted_at lost")
	}

	_, _, err = f.s.CreateRun(bg(), f.spec(crew), RunMeta{WorkOrderID: missingID})
	wantNotFound(t, err)
	wantNotFound(t, f.s.EnsureWorkOrderIdle(bg(), missingID))
}

func TestCreateRunConcurrentSubmitOfOneWorkOrder(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	wo := f.workOrder("wo", crew)
	errs := parallel(8, func(int) error {
		_, _, err := f.s.CreateRun(bg(), f.spec(crew), RunMeta{WorkOrderID: wo.ID})
		return err
	})
	ok := 0
	for _, e := range errs {
		switch {
		case e == nil:
			ok++
		case errors.Is(e, ErrWorkOrderActive):
		default:
			t.Errorf("unexpected error %v", e)
		}
	}
	if got := must(f.s.GetWorkOrder(bg(), wo.ID)); ok < 1 || got.RunCount != ok {
		t.Errorf("%d submits succeeded but work order has %d runs", ok, got.RunCount)
	}
	if ok != 1 {
		t.Errorf("%d of 8 concurrent submits of one idle work order each started a run (want 1)", ok)
	}
}

func TestRunStateTransitions(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")

	// cancel only while queued
	q := f.run(crew, RunMeta{})
	c, ok, err := f.s.CancelQueuedRun(bg(), q.ID, "no longer needed")
	mustOK(t, err)
	if !ok || c.Status != model.RunCancelled || c.Error == nil || *c.Error != "no longer needed" || c.FinishedAt == nil {
		t.Errorf("cancel = %+v %v", c, ok)
	}
	if _, ok, _ := f.s.CancelQueuedRun(bg(), q.ID, "again"); ok {
		t.Error("cancelled run cancelled twice")
	}
	if started, _ := f.s.StartRun(bg(), q.ID); started {
		t.Error("cancelled run must not start")
	}
	if _, ok, err := f.s.CancelQueuedRun(bg(), missingID, "x"); ok || err != nil {
		t.Errorf("missing run: %v %v", ok, err)
	}
	r := f.run(crew, RunMeta{})
	mustOK(t, func() error { _, _ = f.s.StartRun(bg(), r.ID); return nil }())
	if _, ok, _ := f.s.CancelQueuedRun(bg(), r.ID, "x"); ok {
		t.Error("running run cancelled via queue path")
	}

	// ForceFinishRun: affects only non-terminal runs
	mustOK(t, f.s.ForceFinishRun(bg(), r.ID, model.RunFailed, "could not save"))
	got := must(f.s.GetRun(bg(), r.ID))
	if got.Status != model.RunFailed || *got.Error != "could not save" || got.FinishedAt == nil {
		t.Errorf("forced = %+v", got)
	}
	mustOK(t, f.s.ForceFinishRun(bg(), r.ID, model.RunSucceeded, "ignored"))
	if got := must(f.s.GetRun(bg(), r.ID)); got.Status != model.RunFailed {
		t.Error("terminal run changed by ForceFinishRun")
	}
	// FinishRun on terminal/missing
	_, err = f.s.FinishRun(bg(), q.ID, RunResult{Status: model.RunSucceeded})
	wantNotFound(t, err)
	_, err = f.s.FinishRun(bg(), missingID, RunResult{Status: model.RunSucceeded})
	wantNotFound(t, err)

	// FinishRun can go straight from queued, and rewrites task outputs idempotently
	q2 := f.run(crew, RunMeta{})
	fin, err := f.s.FinishRun(bg(), q2.ID, RunResult{Status: model.RunSucceeded, Output: ptr("out"),
		TaskOutputs: []model.TaskOutput{{Position: 1, TaskName: "b", Raw: "B"}, {Position: 0, TaskName: "a", AgentRole: "r", Raw: "A"}}})
	mustOK(t, err)
	if fin.Status != model.RunSucceeded {
		t.Error(fin.Status)
	}
	d := must(f.s.GetRunDetail(bg(), q2.ID))
	if d.TaskOutputs == nil || len(*d.TaskOutputs) != 2 || (*d.TaskOutputs)[0].TaskName != "a" || (*d.TaskOutputs)[0].AgentRole != "r" {
		t.Errorf("task outputs = %+v", d.TaskOutputs)
	}
	empty := must(f.s.GetRunDetail(bg(), q.ID))
	if empty.TaskOutputs == nil || len(*empty.TaskOutputs) != 0 {
		t.Error("detail must include an empty list, not nil")
	}
	// bad status rejected by CHECK
	q3 := f.run(crew, RunMeta{})
	if _, err := f.s.FinishRun(bg(), q3.ID, RunResult{Status: "exploded"}); err == nil {
		t.Error("bad status accepted")
	}
	if got := must(f.s.GetRun(bg(), q3.ID)); got.Status != model.RunQueued {
		t.Errorf("failed finish changed status: %s", got.Status)
	}
}

func TestQueuedRunIDsOldestFirstWithLimit(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	var ids []string
	for i := 0; i < 4; i++ {
		ids = append(ids, f.run(crew, RunMeta{}).ID)
	}
	f.exec(`UPDATE runs SET status = 'running' WHERE id = $1`, ids[1])
	got := must(f.s.QueuedRunIDs(bg(), 10))
	if !slices.Equal(got, []string{ids[0], ids[2], ids[3]}) {
		t.Errorf("queued = %v", got)
	}
	if got := must(f.s.QueuedRunIDs(bg(), 2)); len(got) != 2 {
		t.Errorf("limit = %d", len(got))
	}
}

func TestListRunsFiltersScopeAndStablePagination(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("Alpha"), f.area("Beta")
	crewA, crewB := f.simpleCrew("ca", a), f.simpleCrew("cb", b)
	var all []string
	for i := 0; i < 7; i++ {
		crew := crewA
		if i%2 == 1 {
			crew = crewB
		}
		meta := RunMeta{AreaIDs: crew.AreaIDs}
		if i == 3 {
			meta.TriggerType = "api"
		}
		all = append(all, f.run(crew, meta).ID)
	}
	f.finish(all[0], model.RunSucceeded)
	// force identical timestamps so only the id tiebreaker orders rows
	f.exec(`UPDATE runs SET created_at = '2024-01-01T00:00:00Z'`)

	collect := func(flt RunFilter, page int) ([]string, int) {
		var out []string
		var total int
		for off := 0; ; off += page {
			flt.Limit, flt.Offset = page, off
			runs, tot, err := f.s.ListRuns(bg(), flt)
			mustOK(t, err)
			total = tot
			for _, r := range runs {
				out = append(out, r.ID)
			}
			if len(runs) < page {
				return out, total
			}
		}
	}
	ids, total := collect(RunFilter{Scope: AllAreas}, 3)
	if total != 7 || len(ids) != 7 || !sameSet(ids, all) {
		t.Fatalf("paged %d of %d: %v", len(ids), total, ids)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate %s across pages", id)
		}
		seen[id] = true
	}
	// order is stable (created_at DESC, id DESC) and equal for different page sizes
	again, _ := collect(RunFilter{Scope: AllAreas}, 5)
	if !slices.Equal(ids, again) {
		t.Errorf("order depends on page size:\n%v\n%v", ids, again)
	}
	desc := slices.Clone(ids)
	slices.SortFunc(desc, func(x, y string) int { return -compareStr(x, y) })
	if !slices.Equal(ids, desc) {
		t.Errorf("ties must break by id desc: %v", ids)
	}

	if _, total := collect(RunFilter{Scope: Scope{AreaIDs: []string{a}}}, 10); total != 4 {
		t.Errorf("scope A total = %d", total)
	}
	if _, total := collect(RunFilter{Scope: Scope{AreaIDs: []string{a, b}}}, 10); total != 7 {
		t.Errorf("scope A+B total = %d", total)
	}
	if _, total := collect(RunFilter{Scope: Scope{}}, 10); total != 0 {
		t.Errorf("empty scope total = %d", total)
	}
	if _, total := collect(RunFilter{Scope: AllAreas, CrewID: crewB.ID}, 10); total != 3 {
		t.Errorf("crew filter total = %d", total)
	}
	if _, total := collect(RunFilter{Scope: AllAreas, Status: "succeeded"}, 10); total != 1 {
		t.Errorf("status filter = %d", total)
	}
	if _, total := collect(RunFilter{Scope: AllAreas, TriggerType: "api"}, 10); total != 1 {
		t.Errorf("trigger filter = %d", total)
	}
	if _, total := collect(RunFilter{Scope: Scope{AreaIDs: []string{a}}, CrewID: crewB.ID}, 10); total != 0 {
		t.Errorf("scope+crew filter = %d", total)
	}
	// offset past the end
	runs, total, err := f.s.ListRuns(bg(), RunFilter{Scope: AllAreas, Limit: 5, Offset: 100})
	mustOK(t, err)
	if len(runs) != 0 || total != 7 || runs == nil {
		t.Errorf("past end: %v %d", runs, total)
	}
}

func compareStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func TestDeleteRunRelinksWorkOrder(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	wo := f.workOrder("wo", crew)
	r1 := f.run(crew, RunMeta{WorkOrderID: wo.ID})
	f.finish(r1.ID, model.RunFailed)
	time.Sleep(5 * time.Millisecond)
	r2 := f.run(crew, RunMeta{WorkOrderID: wo.ID})
	wantConflict(t, f.s.DeleteRun(bg(), r2.ID), "Cancel")
	f.finish(r2.ID, model.RunSucceeded)

	if got := must(f.s.GetWorkOrder(bg(), wo.ID)); *got.CurrentRunID != r2.ID || got.Status != "succeeded" {
		t.Fatalf("before = %+v", got)
	}
	mustOK(t, f.s.DeleteRun(bg(), r2.ID))
	got := must(f.s.GetWorkOrder(bg(), wo.ID))
	if got.CurrentRunID == nil || *got.CurrentRunID != r1.ID || got.Status != "failed" || got.RunCount != 1 {
		t.Errorf("after deleting newest = %+v", got)
	}
	mustOK(t, f.s.DeleteRun(bg(), r1.ID))
	got = must(f.s.GetWorkOrder(bg(), wo.ID))
	if got.CurrentRunID != nil || got.Status != "draft" || got.RunCount != 0 {
		t.Errorf("after deleting all = %+v", got)
	}
	wantNotFound(t, f.s.DeleteRun(bg(), r1.ID))
	// the work order can be submitted again
	f.run(crew, RunMeta{WorkOrderID: wo.ID})
}

func TestDeleteRunRemovesEventsAndQueuesFiles(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	wo := f.workOrder("wo", crew)
	run := f.run(crew, RunMeta{WorkOrderID: wo.ID, Callback: &Callback{URL: "https://example.com/cb", SecretSource: CallbackFromServiceAccount, SecretOwner: missingID}})
	mustOK(t, f.s.AppendEvent(bg(), run.ID, model.RunEvent{Seq: 1, Type: "log", Payload: map[string]any{}, CreatedAt: time.Now()}))
	_, err := f.s.AddAttachment(bg(), model.Attachment{ID: auth.NewUUID(), RunID: &run.ID, Kind: model.AttachmentArtifact, Filename: "out.txt",
		ContentType: "text/plain", SizeBytes: 3, SHA256: "x", StorageKey: "runs/" + run.ID + "/out.txt"})
	mustOK(t, err)
	f.finish(run.ID, model.RunSucceeded)
	mustOK(t, f.s.DeleteRun(bg(), run.ID))

	var events, deliveries int
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT count(*) FROM run_events`).Scan(&events))
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT count(*) FROM callback_deliveries`).Scan(&deliveries))
	if events != 0 || deliveries != 0 {
		t.Errorf("leftovers: %d events, %d deliveries", events, deliveries)
	}
	if keys := must(f.s.PendingStorageDeletions(bg(), 10)); !slices.Equal(keys, []string{"runs/" + run.ID + "/out.txt"}) {
		t.Errorf("storage deletions = %v", keys)
	}
}

func TestFailOrphanedRunsLeavesOthers(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	running, queued := f.run(crew, RunMeta{}), f.run(crew, RunMeta{})
	mustOK(t, func() error { _, err := f.s.StartRun(bg(), running.ID); return err }())
	ids := must(f.s.FailOrphanedRuns(bg()))
	if !slices.Equal(ids, []string{running.ID}) {
		t.Errorf("orphaned = %v", ids)
	}
	if got := must(f.s.GetRun(bg(), queued.ID)); got.Status != model.RunQueued {
		t.Error("queued run touched")
	}
	if ids := must(f.s.FailOrphanedRuns(bg())); len(ids) != 0 {
		t.Errorf("second pass = %v", ids)
	}
}

func TestCallbackDeliveries(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	cb := &Callback{URL: "https://example.com/hook", SecretSource: CallbackFromTrigger, SecretOwner: missingID}
	mk := func() model.Run { return f.run(crew, RunMeta{Callback: cb}) }
	r1, r2, r3 := mk(), mk(), mk()

	ds := must(f.s.ListDeliveries(bg(), r1.ID))
	if len(ds) != 1 || ds[0].Status != "pending" || ds[0].Attempts != 0 || ds[0].URL != cb.URL || ds[0].SecretSource != CallbackFromTrigger {
		t.Fatalf("deliveries = %+v", ds)
	}
	if ds := must(f.s.ListDeliveries(bg(), missingID)); ds == nil || len(ds) != 0 {
		t.Error("empty list must be non-nil")
	}
	// runs that have not finished are not claimed
	if got := must(f.s.ClaimDueDeliveries(bg(), 10, time.Minute)); len(got) != 0 {
		t.Fatalf("claimed %d before runs finished", len(got))
	}
	f.finish(r1.ID, model.RunSucceeded)
	f.finish(r2.ID, model.RunFailed)

	claimed := must(f.s.ClaimDueDeliveries(bg(), 1, time.Hour))
	if len(claimed) != 1 {
		t.Fatalf("limit not honoured: %d", len(claimed))
	}
	// leased: not claimable again, the other one is
	second := must(f.s.ClaimDueDeliveries(bg(), 10, time.Hour))
	if len(second) != 1 || second[0].ID == claimed[0].ID {
		t.Fatalf("lease broken: %+v then %+v", claimed, second)
	}
	if got := must(f.s.ClaimDueDeliveries(bg(), 10, time.Hour)); len(got) != 0 {
		t.Errorf("both leased, still claimed %d", len(got))
	}
	if !claimed[0].NextAttemptAt.After(time.Now().Add(50 * time.Minute)) {
		t.Errorf("lease not applied: %v", claimed[0].NextAttemptAt)
	}

	// failure with a retry time stays pending and is reclaimable once due
	retry := time.Now().Add(-time.Second)
	code := 503
	msg := "unavailable"
	mustOK(t, f.s.RecordDeliveryAttempt(bg(), claimed[0].ID, false, &code, &msg, &retry))
	d := must(f.s.ListDeliveries(bg(), claimed[0].RunID))[0]
	if d.Status != "pending" || d.Attempts != 1 || *d.LastStatusCode != 503 || *d.LastError != msg || d.DeliveredAt != nil {
		t.Errorf("after failure = %+v", d)
	}
	re := must(f.s.ClaimDueDeliveries(bg(), 10, time.Hour))
	if len(re) != 1 || re[0].ID != claimed[0].ID {
		t.Fatalf("retry not claimable: %+v", re)
	}
	// delivered
	ok := 200
	mustOK(t, f.s.RecordDeliveryAttempt(bg(), claimed[0].ID, true, &ok, nil, nil))
	d = must(f.s.ListDeliveries(bg(), claimed[0].RunID))[0]
	if d.Status != "delivered" || d.Attempts != 2 || d.DeliveredAt == nil || d.LastError != nil {
		t.Errorf("delivered = %+v", d)
	}
	// permanent failure: no retry time
	mustOK(t, f.s.RecordDeliveryAttempt(bg(), second[0].ID, false, nil, &msg, nil))
	if d := must(f.s.ListDeliveries(bg(), second[0].RunID))[0]; d.Status != "failed" || d.DeliveredAt != nil {
		t.Errorf("failed = %+v", d)
	}
	f.exec(`UPDATE callback_deliveries SET next_attempt_at = now() - interval '1 hour'`)
	if got := must(f.s.ClaimDueDeliveries(bg(), 10, time.Hour)); len(got) != 0 {
		t.Errorf("finished deliveries must not be claimed: %+v", got)
	}
	_ = r3
}

func TestClaimDueDeliveriesExclusiveAcrossWorkers(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	cb := &Callback{URL: "https://example.com/hook", SecretSource: CallbackFromServiceAccount, SecretOwner: missingID}
	const n = 10
	for i := 0; i < n; i++ {
		r := f.run(crew, RunMeta{Callback: cb})
		f.finish(r.ID, model.RunSucceeded)
	}
	results := parallel(5, func(int) []Delivery {
		got, err := f.s.ClaimDueDeliveries(bg(), 4, time.Hour)
		if err != nil {
			t.Error(err)
		}
		return got
	})
	seen := map[string]int{}
	for _, r := range results {
		for _, d := range r {
			seen[d.ID]++
		}
	}
	for id, c := range seen {
		if c != 1 {
			t.Errorf("delivery %s claimed %d times", id, c)
		}
	}
	if len(seen) != n {
		t.Errorf("claimed %d of %d", len(seen), n)
	}
}

func TestEventsAndLLMCalls(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	run := f.run(crew, RunMeta{})
	now := time.Now()

	if seq := must(f.s.MaxEventSeq(bg(), run.ID)); seq != 0 {
		t.Errorf("empty seq = %d", seq)
	}
	for i := 1; i <= 5; i++ {
		mustOK(t, f.s.AppendEvent(bg(), run.ID, model.RunEvent{Seq: i, Type: "task_started", Payload: map[string]any{"i": i}, CreatedAt: now}))
	}
	if err := f.s.AppendEvent(bg(), run.ID, model.RunEvent{Seq: 3, Type: "dup", Payload: map[string]any{}, CreatedAt: now}); err == nil {
		t.Error("duplicate seq accepted")
	}
	if err := f.s.AppendEvent(bg(), missingID, model.RunEvent{Seq: 1, Type: "x", Payload: map[string]any{}, CreatedAt: now}); err == nil {
		t.Error("event for unknown run accepted")
	}
	if err := f.s.AppendEvent(bg(), run.ID, model.RunEvent{Seq: 9, Type: "bad", Payload: map[string]any{"c": make(chan int)}, CreatedAt: now}); err == nil {
		t.Error("unencodable payload accepted")
	}
	evs := must(f.s.ListEvents(bg(), run.ID, 2, 2))
	if len(evs) != 2 || evs[0].Seq != 3 || evs[1].Seq != 4 || evs[0].Payload["i"].(float64) != 3 {
		t.Errorf("events = %+v", evs)
	}
	if evs := must(f.s.ListEvents(bg(), run.ID, 5, 10)); evs == nil || len(evs) != 0 {
		t.Errorf("after last = %#v", evs)
	}
	if seq := must(f.s.MaxEventSeq(bg(), run.ID)); seq != 5 {
		t.Errorf("max = %d", seq)
	}

	for seq, resp := range map[int]string{1: "hello world", 2: "short", 3: ""} {
		mustOK(t, f.s.AppendLLMCall(bg(), run.ID, model.LLMCall{Seq: seq, AgentRole: "a", TaskName: "t", Model: "m",
			Messages: []map[string]string{{"role": "user", "content": "hi"}}, Response: resp,
			Usage: model.TokenUsage{PromptTokens: seq, CompletionTokens: 2, CachedPromptTokens: 1, CacheWriteTokens: seq * 10}, CostUSD: 0.01, CreatedAt: now}))
	}
	if err := f.s.AppendLLMCall(bg(), run.ID, model.LLMCall{Seq: 1, Model: "m", CreatedAt: now}); err == nil {
		t.Error("duplicate llm call seq accepted")
	}
	calls := must(f.s.ListLLMCalls(bg(), run.ID))
	if len(calls) != 3 || calls[0].Seq != 1 || calls[2].Usage.PromptTokens != 3 || calls[2].Usage.CacheWriteTokens != 30 || calls[0].Usage.SuccessfulRequests != 1 || calls[0].CostUSD != 0.01 {
		t.Errorf("calls = %+v", calls)
	}
	if got := must(f.s.ListLLMCalls(bg(), missingID)); got == nil || len(got) != 0 {
		t.Error("empty calls must be non-nil")
	}
	resp := must(f.s.LLMResponses(bg(), run.ID, 5))
	if resp[1] != "hello" || resp[2] != "short" || resp[3] != "" || len(resp) != 3 {
		t.Errorf("responses = %v", resp)
	}
	if got := must(f.s.LLMResponses(bg(), missingID, 5)); len(got) != 0 {
		t.Errorf("unknown run = %v", got)
	}
}

func TestInteractionEventsSkipLogsAndOrder(t *testing.T) {
	f := newFixture(t)
	run := f.run(f.simpleCrew("c"), RunMeta{})
	now := time.Now()
	for i, typ := range []string{"log", "tool_call", "log", "delegation"} {
		mustOK(t, f.s.AppendEvent(bg(), run.ID, model.RunEvent{Seq: i + 1, Type: typ, Payload: map[string]any{}, CreatedAt: now}))
	}
	evs := must(f.s.InteractionEvents(bg(), run.ID))
	if len(evs) != 2 || evs[0].Type != "tool_call" || evs[1].Type != "delegation" {
		t.Errorf("events = %+v", evs)
	}
}

func TestGroundedSearchCounting(t *testing.T) {
	f := newFixture(t)
	run := f.run(f.simpleCrew("c"), RunMeta{})
	now := time.Now()
	add := func(seq int, typ string, payload map[string]any, at time.Time) {
		mustOK(t, f.s.AppendEvent(bg(), run.ID, model.RunEvent{Seq: seq, Type: typ, Payload: payload, CreatedAt: at}))
	}
	add(1, "search", map[string]any{"grounded": true}, now)                                            // legacy: counts once
	add(2, "search", map[string]any{"grounded": true, "billable_searches": 3}, now)                    // counts 3
	add(3, "search", map[string]any{"grounded": false, "billable_searches": 9}, now)                   // not grounded
	add(4, "log", map[string]any{"grounded": true}, now)                                               // wrong type
	add(5, "search", map[string]any{"grounded": true, "billable_searches": 2}, now.AddDate(0, 0, -40)) // old
	if n := must(f.s.GroundedSearchesInPeriod(bg(), model.GroundingPeriodMonth)); n != 4 {
		t.Errorf("month = %d, want 4", n)
	}
	if n := must(f.s.GroundedSearchesInPeriod(bg(), model.GroundingPeriodDay)); n != 4 {
		t.Errorf("day = %d, want 4", n)
	}
	if _, err := f.s.GroundedSearchesInPeriod(bg(), "year"); err == nil {
		t.Error("unknown period accepted")
	}
	// empty case
	g := newUnitStore(t)
	if n := must(g.GroundedSearchesInPeriod(bg(), model.GroundingPeriodDay)); n != 0 {
		t.Errorf("empty = %d", n)
	}
}

func TestOverviewScoping(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("Alpha"), f.area("Beta")
	crewA, crewB := f.simpleCrew("ca", a), f.simpleCrew("cb", b)
	rA := f.run(crewA, RunMeta{AreaIDs: crewA.AreaIDs})
	rB := f.run(crewB, RunMeta{AreaIDs: crewB.AreaIDs})
	f.finish(rA.ID, model.RunSucceeded)
	_, err := f.s.FinishRun(bg(), rB.ID, RunResult{Status: model.RunSucceeded, CostUSD: ptr(2.5)})
	mustOK(t, err)
	f.workOrder("woa", crewA)
	f.admin()
	_, err = f.s.SaveSchedule(bg(), "", model.Schedule{Name: "s", CrewID: &crewB.ID, Cron: "* * * * *", Timezone: "UTC", OverlapPolicy: "skip"}, nil)
	mustOK(t, err)

	all := must(f.s.Overview(bg(), AllAreas))
	if all.Counts.Runs != 2 || all.Counts.WorkOrders != 1 || all.Counts.Schedules != 1 || all.SpendLast30dUSD != 2.5 || len(all.RecentRuns) != 2 {
		t.Errorf("all = %+v", all.Counts)
	}
	onlyA := must(f.s.Overview(bg(), Scope{AreaIDs: []string{a}}))
	if onlyA.Counts.Runs != 1 || onlyA.Counts.WorkOrders != 1 || onlyA.Counts.Schedules != 0 || onlyA.SpendLast30dUSD != 0 ||
		len(onlyA.RecentRuns) != 1 || onlyA.RecentRuns[0].ID != rA.ID || onlyA.Counts.Crews != 1 || onlyA.Counts.Agents != 1 {
		t.Errorf("A = %+v spend %v", onlyA.Counts, onlyA.SpendLast30dUSD)
	}
	onlyB := must(f.s.Overview(bg(), Scope{AreaIDs: []string{b}}))
	if onlyB.Counts.Schedules != 1 || onlyB.SpendLast30dUSD != 2.5 || onlyB.Counts.WorkOrders != 0 {
		t.Errorf("B = %+v", onlyB.Counts)
	}
	// shared catalogs are always counted
	none := must(f.s.Overview(bg(), Scope{}))
	if none.Counts.Runs != 0 || none.Counts.Crews != 0 || none.Counts.LLMs != all.Counts.LLMs || none.Counts.Pricing != all.Counts.Pricing {
		t.Errorf("none = %+v", none.Counts)
	}
	_ = fmt.Sprint
}

// Deterministic version of the concurrent-submit race: B waits on the row lock while A starts a run.
func TestLockIdleWorkOrderSeesRunStartedWhileWaiting(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	wo := f.workOrder("wo", crew)
	ctx := bg()

	txA, err := f.s.pool.Begin(ctx)
	mustOK(t, err)
	defer txA.Rollback(ctx) //nolint:errcheck
	mustOK(t, lockIdleWorkOrder(ctx, txA, wo.ID))

	done := make(chan error, 1)
	go func() {
		txB, err := f.s.pool.Begin(ctx)
		if err != nil {
			done <- err
			return
		}
		defer txB.Rollback(ctx)                    //nolint:errcheck
		done <- lockIdleWorkOrder(ctx, txB, wo.ID) // blocks until A finishes
	}()
	time.Sleep(300 * time.Millisecond)

	var runID string
	mustOK(t, txA.QueryRow(ctx, `INSERT INTO runs (crew_id, crew_name, status, budget_usd, spec, work_order_id)
		VALUES ($1, 'c', 'queued', 1, '{}', $2) RETURNING id`, crew.ID, wo.ID).Scan(&runID))
	_, err = txA.Exec(ctx, `UPDATE work_orders SET current_run_id = $2 WHERE id = $1`, wo.ID, runID)
	mustOK(t, err)
	mustOK(t, txA.Commit(ctx))

	if err := <-done; !errors.Is(err, ErrWorkOrderActive) {
		t.Errorf("lockIdleWorkOrder returned %v for a work order whose run was queued while it waited for the lock; want ErrWorkOrderActive", err)
	}
}
