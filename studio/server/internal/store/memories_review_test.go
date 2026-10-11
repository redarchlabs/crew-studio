package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"crewstudio/internal/model"
)

func pendingWrite(scope, content, hash string, runID *string) MemoryWrite {
	w := memWrite(scope, content, hash, []float32{1, 0}, runID)
	w.Status = model.MemoryPending
	return w
}

// A run's exact duplicate of a memory it cannot see (another run's pending one) is skipped without saying which.
func TestSaveMemoriesHidesInvisibleDuplicates(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	writer, other := f.run(crew, RunMeta{}), f.run(crew, RunMeta{})
	first := must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{pendingWrite("/p", "Sam is late", "h1", &writer.ID)},
		MemoryCaps{RunID: writer.ID, PerRun: 5, PerCrew: 100}))
	hidden := first.Saved[0].ID

	res := must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{pendingWrite("/p", "sam is LATE", "h1", &other.ID),
		pendingWrite("/p", "new lesson", "h2", &other.ID)}, MemoryCaps{RunID: other.ID, PerRun: 5, PerCrew: 100}))
	if len(res.Saved) != 2 || res.Saved[0] != (MemorySaved{Skipped: SkipNotSaved}) || res.Skipped != 1 || len(res.Inserted) != 1 || res.Remaining != 4 {
		t.Fatalf("other run = %+v", res)
	}
	if res.Saved[1].ID == hidden || res.Saved[1].Status != model.MemoryPending {
		t.Errorf("new = %+v", res.Saved[1])
	}
	// A near-duplicate hint pointing at an invisible row is ignored, not touched
	near := pendingWrite("/p", "Sam is late!", "h3", &other.ID)
	near.DuplicateOf = hidden
	res = must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{near}, MemoryCaps{RunID: other.ID, PerRun: 5, PerCrew: 100}))
	if res.Saved[0].Duplicate || res.Saved[0].ID == hidden {
		t.Errorf("near = %+v", res)
	}
	// The writer itself, and people, still find it
	res = must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{pendingWrite("/p", "Sam is late", "h1", &writer.ID)}, MemoryCaps{RunID: writer.ID, PerRun: 5, PerCrew: 100}))
	if !res.Saved[0].Duplicate || res.Saved[0].ID != hidden {
		t.Errorf("writer = %+v", res)
	}
	res = must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{memWrite("/p", "Sam is late", "h1", []float32{1}, nil)}, MemoryCaps{PerCrew: 100}))
	if !res.Saved[0].Duplicate || res.Saved[0].ID != hidden {
		t.Errorf("person = %+v", res)
	}
}

func TestSaveMemoriesPendingCap(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	run := f.run(crew, RunMeta{})
	caps := MemoryCaps{RunID: run.ID, PerRun: 10, PerCrew: 100, PendingPerCrew: 2}
	res := must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{pendingWrite("/", "a", "a", &run.ID), pendingWrite("/", "b", "b", &run.ID),
		pendingWrite("/", "c", "c", &run.ID), memWrite("/", "d", "d", []float32{1}, &run.ID)}, caps))
	if res.Skipped != 1 || !res.PendingFull || res.Saved[2].Skipped != SkipPendingFull || res.Saved[2].ID != "" || res.Saved[3].Status != model.MemoryActive {
		t.Fatalf("pending cap = %+v", res)
	}
	// An exact duplicate is still found when the queue is full; people are not held by it
	res = must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{pendingWrite("/", "a", "a", &run.ID)}, caps))
	if !res.Saved[0].Duplicate {
		t.Errorf("duplicate = %+v", res)
	}
	res = must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{pendingWrite("/", "by hand", "p", nil)}, MemoryCaps{PerCrew: 100}))
	if res.Saved[0].Skipped != "" {
		t.Errorf("person = %+v", res)
	}
}

// Concurrent saves of one crew are serialised: the run's cap, the crew's cap and the pending cap all hold.
func TestSaveMemoriesConcurrentCaps(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	run := f.run(crew, RunMeta{})
	const workers, each = 8, 5
	var wg sync.WaitGroup
	errs := make(chan error, workers*2)
	save := func(runID *string, caps MemoryCaps, prefix string, pending bool) {
		defer wg.Done()
		writes := make([]MemoryWrite, each)
		for i := range writes {
			name := fmt.Sprintf("%s-%d", prefix, i)
			writes[i] = memWrite("/", name, name, []float32{1}, runID)
			if pending {
				writes[i].Status = model.MemoryPending
			}
		}
		if _, err := f.s.SaveMemories(bg(), crew.ID, writes, caps); err != nil {
			errs <- err
		}
	}
	for g := range workers {
		wg.Add(1)
		go save(&run.ID, MemoryCaps{RunID: run.ID, PerRun: 12, PerCrew: 1000, PendingPerCrew: 1000}, fmt.Sprintf("run%d", g), false)
	}
	wg.Wait()
	if n := must(f.s.RunMemorySaves(bg(), run.ID)); n != 12 {
		t.Errorf("run saves = %d, want 12", n)
	}
	if n := must(f.s.CountMemories(bg(), MemoryFilter{CrewID: crew.ID})); n != 12 {
		t.Errorf("rows = %d, want 12", n)
	}
	for g := range workers {
		wg.Add(1)
		other := f.run(crew, RunMeta{})
		go save(&other.ID, MemoryCaps{RunID: other.ID, PerRun: 100, PerCrew: 1000, PendingPerCrew: 9}, fmt.Sprintf("pend%d", g), true)
	}
	wg.Wait()
	if n := must(f.s.CountMemories(bg(), MemoryFilter{CrewID: crew.ID, Status: model.MemoryPending})); n != 9 {
		t.Errorf("pending = %d, want 9", n)
	}
	for g := range workers {
		wg.Add(1)
		go save(nil, MemoryCaps{PerCrew: 30}, fmt.Sprintf("crew%d", g), false)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := must(f.s.CountMemories(bg(), MemoryFilter{CrewID: crew.ID})); n != 30 {
		t.Errorf("crew rows = %d, want 30", n)
	}
}

// A run may not change a memory a person pinned, or one a person approved while the run's saves are reviewed.
func TestUpdateMemoryLockedForRuns(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	run := f.run(crew, RunMeta{})
	caps := MemoryCaps{RunID: run.ID, PerRun: 10, PerCrew: 100}
	res := must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{pendingWrite("/", "a", "a", &run.ID), pendingWrite("/", "b", "b", &run.ID),
		memWrite("/", "c", "c", []float32{1}, &run.ID)}, caps))
	approvedID, pinnedID, activeID := res.Saved[0].ID, res.Saved[1].ID, res.Saved[2].ID
	active, yes := model.MemoryActive, true
	must(f.s.UpdateMemory(bg(), crew.ID, approvedID, MemoryUpdate{Status: &active}))
	must(f.s.UpdateMemory(bg(), crew.ID, pinnedID, MemoryUpdate{Pinned: &yes}))
	imp := 0.9
	for name, tc := range map[string]struct {
		id     string
		review bool
	}{"approved": {approvedID, true}, "pinned": {pinnedID, false}, "pinned reviewed": {pinnedID, true}, "active reviewed": {activeID, true}} {
		if _, err := f.s.UpdateMemory(bg(), crew.ID, tc.id, MemoryUpdate{Importance: &imp, OnlyRunID: run.ID, RunReview: tc.review}); !errors.Is(err, ErrMemoryLocked) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// Without review the run keeps changing its own active memory; people change anything
	if got := must(f.s.UpdateMemory(bg(), crew.ID, activeID, MemoryUpdate{Importance: &imp, OnlyRunID: run.ID})); got.Importance != 0.9 {
		t.Errorf("own active = %+v", got)
	}
	must(f.s.UpdateMemory(bg(), crew.ID, approvedID, MemoryUpdate{Importance: &imp}))
}

func TestMemoryEmbedRefundAndReembedBacklog(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	run := f.run(crew, RunMeta{})
	if !must(f.s.AddMemoryEmbeds(bg(), run.ID, 4, 5)) {
		t.Fatal("reserve")
	}
	mustOK(t, f.s.RefundMemoryEmbeds(bg(), run.ID, 3))
	mustOK(t, f.s.RefundMemoryEmbeds(bg(), run.ID, 0))
	if !must(f.s.AddMemoryEmbeds(bg(), run.ID, 4, 5)) {
		t.Error("refunded embeds still counted")
	}
	mustOK(t, f.s.RefundMemoryEmbeds(bg(), run.ID, 99))
	var n int
	mustOK(t, f.s.Pool().QueryRow(bg(), `SELECT embed_texts FROM run_memory_usage WHERE run_id = $1`, run.ID).Scan(&n))
	if n != 0 {
		t.Errorf("embed_texts = %d after refunding more than counted", n)
	}

	old := memWrite("/", "old model", "o", []float32{1}, nil)
	old.EmbedModel = "old:1"
	res := must(f.s.SaveMemories(bg(), crew.ID, []MemoryWrite{old, memWrite("/", "new model", "n", []float32{1}, nil)}, MemoryCaps{PerCrew: 100}))
	if n := must(f.s.CountMemories(bg(), MemoryFilter{CrewID: crew.ID, OtherEmbedModel: "m:2"})); n != 1 {
		t.Errorf("re-embed backlog = %d", n)
	}
	// The re-embed worker loses to a person editing the text meanwhile: the new text's vector stays
	content := "edited text"
	must(f.s.UpdateMemory(bg(), crew.ID, res.Saved[0].ID, MemoryUpdate{Content: &content, ContentHash: "e", Vector: []float32{7}, EmbedModel: "m:2"}))
	mustOK(t, f.s.SetMemoryVector(bg(), res.Saved[0].ID, "old model", "m:2", []float32{9}))
	rows := must(f.s.MemoryRows(bg(), MemoryFilter{CrewID: crew.ID, IDs: []string{res.Saved[0].ID}}))
	if rows[0].Content != content || rows[0].Vector[0] != 7 {
		t.Errorf("row = %q %v", rows[0].Content, rows[0].Vector)
	}
}
