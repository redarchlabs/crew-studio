package store

import (
	"errors"
	"slices"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

func attachment(woID, name string, size int64) model.Attachment {
	id := auth.NewUUID()
	return model.Attachment{ID: id, WorkOrderID: &woID, Kind: model.AttachmentInput, Filename: name, ContentType: "text/plain",
		SizeBytes: size, SHA256: "deadbeef", StorageKey: "work-orders/" + woID + "/" + id}
}

func TestWorkOrderCRUD(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	crew, lone := f.simpleCrew("c", a), f.simpleCrew("lone", a)
	uid := f.admin()
	ctx := WithActor(bg(), Actor{ID: uid, Name: "Admin"})

	wo, err := f.s.CreateWorkOrder(ctx, model.WorkOrderInput{Title: "T1", Brief: "do it", CrewID: &lone.ID,
		Inputs: map[string]string{"k": "v"}, BudgetUSD: ptr(1.5)},
		WorkOrderMeta{TargetName: lone.Name, AreaIDs: lone.AreaIDs, CreatedBy: uid, CreatedByName: "Admin", Source: model.SourceAPI})
	mustOK(t, err)
	if wo.Source != model.SourceAPI || wo.Inputs["k"] != "v" || *wo.BudgetUSD != 1.5 || wo.TargetName != "lone" || wo.CreatedBy == nil ||
		!slices.Equal(wo.AreaIDs, []string{a}) || wo.Status != "draft" {
		t.Errorf("wo = %+v", wo)
	}
	// defaults: manual source, empty inputs
	wo2, err := f.s.CreateWorkOrder(ctx, model.WorkOrderInput{Title: "T2", CrewID: &crew.ID}, WorkOrderMeta{TargetName: crew.Name})
	mustOK(t, err)
	if wo2.Source != model.SourceManual || wo2.Inputs == nil || len(wo2.AreaIDs) != 0 || wo2.Number <= wo.Number {
		t.Errorf("wo2 = %+v", wo2)
	}
	// FK failures surface as conflicts
	_, err = f.s.CreateWorkOrder(ctx, model.WorkOrderInput{Title: "bad", CrewID: ptr(missingID)}, WorkOrderMeta{})
	wantConflict(t, err, "does not exist")

	up, err := f.s.UpdateWorkOrder(ctx, wo.ID, model.WorkOrderInput{Title: "T1b", Brief: "x", CrewID: &crew.ID},
		WorkOrderMeta{TargetName: crew.Name, AreaIDs: crew.AreaIDs})
	mustOK(t, err)
	if up.Title != "T1b" || *up.CrewID != crew.ID || len(up.Inputs) != 0 || up.BudgetUSD != nil {
		t.Errorf("up = %+v", up)
	}
	_, err = f.s.UpdateWorkOrder(ctx, missingID, model.WorkOrderInput{Title: "x", CrewID: &crew.ID}, WorkOrderMeta{})
	wantNotFound(t, err)
	_, err = f.s.GetWorkOrder(ctx, missingID)
	wantNotFound(t, err)
	wantNotFound(t, f.s.DeleteWorkOrder(ctx, missingID))

	revs, total, _ := f.s.ListRevisions(ctx, RevisionFilter{EntityType: "work_order", EntityID: wo.ID, Types: []string{"work_order"}, Scope: AllAreas, Limit: 10})
	if total != 2 || revs[0].Action != "update" {
		t.Errorf("revisions = %d", total)
	}
	mustOK(t, f.s.DeleteWorkOrder(ctx, wo.ID))
	_, _, err = f.s.ListRevisions(ctx, RevisionFilter{Scope: AllAreas, Limit: 1})
	mustOK(t, err)
}

func TestWorkOrderActiveBlocksEditAndDelete(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	wo := f.workOrder("wo", crew)
	run := f.run(crew, RunMeta{WorkOrderID: wo.ID})
	in := model.WorkOrderInput{Title: "new", CrewID: &crew.ID}

	_, err := f.s.UpdateWorkOrder(bg(), wo.ID, in, WorkOrderMeta{})
	if !errors.Is(err, ErrWorkOrderActive) {
		t.Errorf("update queued: %v", err)
	}
	if err := f.s.DeleteWorkOrder(bg(), wo.ID); !errors.Is(err, ErrWorkOrderActive) {
		t.Errorf("delete queued: %v", err)
	}
	if _, err := f.s.AddInputAttachment(bg(), attachment(wo.ID, "a.txt", 1), 5, 100); !errors.Is(err, ErrWorkOrderActive) {
		t.Errorf("attach while queued: %v", err)
	}
	if ok, _ := f.s.StartRun(bg(), run.ID); !ok {
		t.Fatal("start")
	}
	if err := f.s.DeleteWorkOrder(bg(), wo.ID); !errors.Is(err, ErrWorkOrderActive) {
		t.Errorf("delete running: %v", err)
	}
	if active := must(f.s.SourceHasActiveWorkOrder(bg(), "manual", missingID)); active {
		t.Error("no source id match expected")
	}
	f.finish(run.ID, model.RunSucceeded)
	_, err = f.s.UpdateWorkOrder(bg(), wo.ID, in, WorkOrderMeta{})
	mustOK(t, err)
	mustOK(t, f.s.DeleteWorkOrder(bg(), wo.ID))
	var n int
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT count(*) FROM runs`).Scan(&n))
	if n != 0 {
		t.Errorf("runs left after cascade: %d", n)
	}
}

func TestListWorkOrdersFilters(t *testing.T) {
	f := newFixture(t)
	a, b := f.area("Alpha"), f.area("Beta")
	crewA, crewB := f.simpleCrew("ca", a), f.simpleCrew("cb", b)
	crewA2 := f.simpleCrew("ca2", a)
	mk := func(title string, crew model.Crew, src string, srcID string) model.WorkOrder {
		wo, err := f.s.CreateWorkOrder(bg(), model.WorkOrderInput{Title: title, CrewID: &crew.ID},
			WorkOrderMeta{TargetName: crew.Name, AreaIDs: crew.AreaIDs, Source: src, SourceID: srcID})
		mustOK(t, err)
		return wo
	}
	sid := auth.NewUUID()
	w1 := mk("Quarterly report", crewA, model.SourceManual, "")
	w2 := mk("100% done_ish", crewB, model.SourceSchedule, sid)
	w3 := mk("Another", crewA, model.SourceSchedule, sid)
	wOther := mk("Other crew", crewA2, model.SourceManual, "")
	f.run(crewA, RunMeta{WorkOrderID: w3.ID})

	count := func(flt WorkOrderFilter) []string {
		flt.Limit = 50
		if !flt.Scope.All && len(flt.Scope.AreaIDs) == 0 {
			flt.Scope = AllAreas
		}
		items, total, err := f.s.ListWorkOrders(bg(), flt)
		mustOK(t, err)
		if total != len(items) {
			t.Errorf("total %d vs %d items", total, len(items))
		}
		return sortedIDs(items, func(w model.WorkOrder) string { return w.ID })
	}
	if got := count(WorkOrderFilter{}); len(got) != 4 {
		t.Errorf("all = %d", len(got))
	}
	if got := count(WorkOrderFilter{Scope: Scope{AreaIDs: []string{a}}}); !sameSet(got, []string{w1.ID, w3.ID, wOther.ID}) {
		t.Errorf("scope A = %v", got)
	}
	if got := count(WorkOrderFilter{Scope: Scope{AreaIDs: []string{b}}}); !sameSet(got, []string{w2.ID}) {
		t.Errorf("scope B = %v", got)
	}
	if got := count(WorkOrderFilter{Status: "draft"}); len(got) != 3 {
		t.Errorf("draft = %v", got)
	}
	if got := count(WorkOrderFilter{Status: "queued"}); !sameSet(got, []string{w3.ID}) {
		t.Errorf("queued = %v", got)
	}
	if got := count(WorkOrderFilter{Source: model.SourceSchedule, SourceID: sid}); !sameSet(got, []string{w2.ID, w3.ID}) {
		t.Errorf("source = %v", got)
	}
	if got := count(WorkOrderFilter{CrewID: crewA.ID}); !sameSet(got, []string{w1.ID, w3.ID}) {
		t.Errorf("crew = %v", got)
	}
	if got := count(WorkOrderFilter{Query: "quarterly"}); !sameSet(got, []string{w1.ID}) {
		t.Errorf("title query = %v", got)
	}
	// LIKE wildcards are literal
	if got := count(WorkOrderFilter{Query: "100%"}); !sameSet(got, []string{w2.ID}) {
		t.Errorf("percent query = %v", got)
	}
	if got := count(WorkOrderFilter{Query: "%"}); !sameSet(got, []string{w2.ID}) {
		t.Errorf("bare percent matched %v", got)
	}
	if got := count(WorkOrderFilter{Query: "_"}); !sameSet(got, []string{w2.ID}) {
		t.Errorf("underscore matched %v", got)
	}
	// by number, with or without the WO- prefix, case-insensitively
	num := w3.Number
	for _, q := range []string{itoa(num), "WO-" + itoa(num), "wo-" + itoa(num)} {
		if got := count(WorkOrderFilter{Query: q}); !slices.Contains(got, w3.ID) {
			t.Errorf("number query %q = %v", q, got)
		}
	}
	if got := count(WorkOrderFilter{Query: "no such thing"}); len(got) != 0 {
		t.Errorf("miss = %v", got)
	}
	// SQL metacharacters are inert
	if got := count(WorkOrderFilter{Query: "'; DROP TABLE work_orders; --"}); len(got) != 0 {
		t.Errorf("injection = %v", got)
	}
	if n := must(f.s.Pool().Exec(bg(), `SELECT 1 FROM work_orders`)).RowsAffected(); n != 4 {
		t.Error("work orders table damaged")
	}
	// paging
	items, total, err := f.s.ListWorkOrders(bg(), WorkOrderFilter{Scope: AllAreas, Limit: 2, Offset: 3})
	mustOK(t, err)
	if len(items) != 1 || total != 4 {
		t.Errorf("page = %d/%d", len(items), total)
	}
	// active counts
	if n := must(f.s.ActiveWorkOrders(bg(), model.SourceSchedule, sid)); n != 1 {
		t.Errorf("active = %d", n)
	}
	if active := must(f.s.SourceHasActiveWorkOrder(bg(), model.SourceSchedule, sid)); !active {
		t.Error("source should be active")
	}
	if active := must(f.s.SourceHasActiveWorkOrder(bg(), model.SourceManual, sid)); active {
		t.Error("wrong source matched")
	}
}

func itoa(n int64) string { return string(appendInt(nil, n)) }

func appendInt(b []byte, n int64) []byte {
	if n >= 10 {
		b = appendInt(b, n/10)
	}
	return append(b, byte('0'+n%10))
}

func TestWorkOrderDetailIncludesFilesAndRuns(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	wo := f.workOrder("wo", crew)
	_, err := f.s.AddInputAttachment(bg(), attachment(wo.ID, "a.txt", 10), 5, 100)
	mustOK(t, err)
	r1 := f.run(crew, RunMeta{WorkOrderID: wo.ID})
	f.finish(r1.ID, model.RunFailed)
	r2 := f.run(crew, RunMeta{WorkOrderID: wo.ID})
	// artifacts belong to the run, so they do not show as inputs
	_, err = f.s.AddAttachment(bg(), model.Attachment{ID: auth.NewUUID(), WorkOrderID: &wo.ID, RunID: &r2.ID, Kind: model.AttachmentArtifact,
		Filename: "out.md", ContentType: "text/markdown", SizeBytes: 1, SHA256: "x", StorageKey: "k-" + r2.ID})
	mustOK(t, err)

	d := must(f.s.GetWorkOrderDetail(bg(), wo.ID))
	if d.Attachments == nil || len(*d.Attachments) != 1 || (*d.Attachments)[0].Filename != "a.txt" {
		t.Errorf("attachments = %+v", d.Attachments)
	}
	if d.Runs == nil || len(*d.Runs) != 2 || (*d.Runs)[0].ID != r2.ID {
		t.Errorf("runs = %+v", d.Runs)
	}
	_, err = f.s.GetWorkOrderDetail(bg(), missingID)
	wantNotFound(t, err)
	// filter variants
	if got := must(f.s.ListAttachments(bg(), AttachmentFilter{RunID: r2.ID})); len(got) != 1 || got[0].Kind != model.AttachmentArtifact {
		t.Errorf("by run = %+v", got)
	}
	if got := must(f.s.ListAttachments(bg(), AttachmentFilter{WorkOrderID: wo.ID})); len(got) != 2 {
		t.Errorf("by wo = %d", len(got))
	}
	if got := must(f.s.ListAttachments(bg(), AttachmentFilter{Kind: "transcript"})); got == nil || len(got) != 0 {
		t.Errorf("by kind = %#v", got)
	}
}

func TestAddInputAttachmentQuota(t *testing.T) {
	f := newFixture(t)
	wo := f.workOrder("wo", f.simpleCrew("c"))
	other := f.workOrder("other", f.simpleCrew("c2"))
	const maxFiles, maxBytes = 3, 100

	_, err := f.s.AddInputAttachment(bg(), attachment(wo.ID, "a", 40), maxFiles, maxBytes)
	mustOK(t, err)
	_, err = f.s.AddInputAttachment(bg(), attachment(wo.ID, "b", 60), maxFiles, maxBytes) // exactly at the byte limit
	mustOK(t, err)
	if _, err := f.s.AddInputAttachment(bg(), attachment(wo.ID, "c", 1), maxFiles, maxBytes); !errors.Is(err, ErrInputQuota) {
		t.Errorf("one byte over: %v", err)
	}
	// quota is per work order
	_, err = f.s.AddInputAttachment(bg(), attachment(other.ID, "x", 100), maxFiles, maxBytes)
	mustOK(t, err)
	// file-count limit
	_, err = f.s.AddInputAttachment(bg(), attachment(other.ID, "y", 0), 1, maxBytes)
	if !errors.Is(err, ErrInputQuota) {
		t.Errorf("count limit: %v", err)
	}
	count, size, err := f.s.InputAttachmentTotals(bg(), wo.ID)
	mustOK(t, err)
	if count != 2 || size != 100 {
		t.Errorf("totals = %d, %d", count, size)
	}
	if c, s, _ := f.s.InputAttachmentTotals(bg(), missingID); c != 0 || s != 0 {
		t.Errorf("empty totals = %d %d", c, s)
	}
	// unknown work order
	_, err = f.s.AddInputAttachment(bg(), attachment(missingID, "z", 1), 5, 100)
	wantNotFound(t, err)
	// duplicate storage key conflicts
	dup := attachment(wo.ID, "d", 0)
	dup2 := dup
	dup2.ID = auth.NewUUID()
	_, err = f.s.AddInputAttachment(bg(), dup, 10, 1000)
	mustOK(t, err)
	_, err = f.s.AddInputAttachment(bg(), dup2, 10, 1000)
	wantConflict(t, err, "already exists")
}

func TestAddInputAttachmentRecordsActor(t *testing.T) {
	f := newFixture(t)
	wo := f.workOrder("wo", f.simpleCrew("c"))
	uid := f.admin()
	ctx := WithActor(bg(), Actor{ID: uid, Name: "Admin"})
	got, err := f.s.AddInputAttachment(ctx, attachment(wo.ID, "w", 5), 10, 1000)
	mustOK(t, err)
	if got.CreatedByName != "Admin" || got.WorkOrderID == nil || got.RunID != nil || got.Kind != model.AttachmentInput {
		t.Errorf("attachment = %+v", got)
	}
	// forced to input kind even if the caller says otherwise
	a := attachment(wo.ID, "x", 1)
	a.Kind = model.AttachmentArtifact
	got, err = f.s.AddInputAttachment(ctx, a, 10, 1000)
	mustOK(t, err)
	if got.Kind != model.AttachmentInput {
		t.Errorf("kind = %s", got.Kind)
	}
}

func TestAddInputAttachmentConcurrentQuota(t *testing.T) {
	f := newFixture(t)
	wo := f.workOrder("wo", f.simpleCrew("c"))
	errs := parallel(10, func(i int) error {
		_, err := f.s.AddInputAttachment(bg(), attachment(wo.ID, "f", 10), 3, 1000)
		return err
	})
	ok := 0
	for _, e := range errs {
		switch {
		case e == nil:
			ok++
		case errors.Is(e, ErrInputQuota):
		default:
			t.Errorf("unexpected %v", e)
		}
	}
	if ok != 3 {
		t.Errorf("%d uploads accepted, want exactly 3", ok)
	}
	if c, _, _ := f.s.InputAttachmentTotals(bg(), wo.ID); c != 3 {
		t.Errorf("stored %d", c)
	}
}

func TestAttachmentConstraintsAndDeletionQueue(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	wo := f.workOrder("wo", crew)
	run := f.run(crew, RunMeta{WorkOrderID: wo.ID})

	// input with a run, or an artifact without one, violates the schema
	bad := attachment(wo.ID, "x", 1)
	bad.RunID = &run.ID
	if _, err := f.s.AddAttachment(bg(), bad); err == nil {
		t.Error("input attached to a run accepted")
	}
	orphan := model.Attachment{ID: auth.NewUUID(), Kind: model.AttachmentArtifact, Filename: "o", StorageKey: "k-orphan", SHA256: "x", ContentType: "x"}
	if _, err := f.s.AddAttachment(bg(), orphan); err == nil {
		t.Error("attachment with no owner accepted")
	}

	in, err := f.s.AddAttachment(bg(), attachment(wo.ID, "in.txt", 3))
	mustOK(t, err)
	got := must(f.s.GetAttachment(bg(), in.ID))
	if got.StorageKey != in.StorageKey || got.SHA256 != "deadbeef" {
		t.Errorf("get = %+v", got)
	}
	_, err = f.s.GetAttachment(bg(), missingID)
	wantNotFound(t, err)
	wantNotFound(t, f.s.DeleteAttachment(bg(), missingID))

	mustOK(t, f.s.DeleteAttachment(bg(), in.ID))
	keys := must(f.s.PendingStorageDeletions(bg(), 10))
	if !slices.Equal(keys, []string{in.StorageKey}) {
		t.Fatalf("queue = %v", keys)
	}
	// failures count up; ten strikes and it is skipped
	for i := 0; i < 10; i++ {
		mustOK(t, f.s.FinishStorageDeletion(bg(), in.StorageKey, errors.New("s3 down")))
	}
	if keys := must(f.s.PendingStorageDeletions(bg(), 10)); len(keys) != 0 {
		t.Errorf("exhausted key still pending: %v", keys)
	}
	var attempts int
	var lastErr string
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT attempts, last_error FROM storage_deletions WHERE storage_key = $1`, in.StorageKey).Scan(&attempts, &lastErr))
	if attempts != 10 || lastErr != "s3 down" {
		t.Errorf("attempts = %d %q", attempts, lastErr)
	}
	mustOK(t, f.s.FinishStorageDeletion(bg(), in.StorageKey, nil))
	var n int
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT count(*) FROM storage_deletions`).Scan(&n))
	if n != 0 {
		t.Errorf("queue not cleared: %d", n)
	}
	// deleting a work order cascades into the queue, once per key
	a1, _ := f.s.AddAttachment(bg(), attachment(wo.ID, "1", 1))
	a2, _ := f.s.AddAttachment(bg(), attachment(wo.ID, "2", 1))
	f.finish(run.ID, model.RunFailed)
	mustOK(t, f.s.DeleteWorkOrder(bg(), wo.ID))
	keys = must(f.s.PendingStorageDeletions(bg(), 1))
	if len(keys) != 1 {
		t.Errorf("limit = %d", len(keys))
	}
	keys = must(f.s.PendingStorageDeletions(bg(), 10))
	if !sameSet(keys, []string{a1.StorageKey, a2.StorageKey}) {
		t.Errorf("cascade queue = %v", keys)
	}
}
