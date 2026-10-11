package store

import (
	"context"
	"sync"
	"testing"

	"crewstudio/internal/model"
)

func km2Rec(key string) model.LogRecord {
	return model.LogRecord{Subject: "km2:ref", Action: model.ActionKM2Save, IdempotencyKey: key,
		Details: map[string]any{"connection_id": "conn-1", "external_ref": "ref"}}
}

func TestReserveKM2Save(t *testing.T) {
	f := newFixture(t)
	crew := f.simpleCrew("c")
	run := f.run(crew, RunMeta{})
	scope := StateScope{CrewID: crew.ID}
	limits := KM2Limits{PerOwner: 2, PerConnectionDay: 100, ConnectionID: "conn-1"}

	res := must(f.s.ReserveKM2Save(bg(), scope, RunKM2Owner(run.ID), km2Rec("k1"), limits))
	if res.Entry == nil || res.Prior != nil || res.Entry.Details["outcome"] != KM2Sending || res.Entry.Action != model.ActionKM2Save {
		t.Fatalf("first = %+v", res)
	}
	// The same document again: the pending entry comes back as prior
	again := must(f.s.ReserveKM2Save(bg(), scope, RunKM2Owner(run.ID), km2Rec("k1"), limits))
	if again.Entry != nil || again.Prior == nil || again.Prior.ID != res.Entry.ID {
		t.Errorf("again = %+v", again)
	}
	done := must(f.s.FinishKM2Save(bg(), res.Entry.ID, map[string]any{"connection_id": "conn-1", "outcome": "created"}))
	if done.Details["outcome"] != "created" {
		t.Errorf("done = %+v", done)
	}
	// An unknown outcome may be sent again under the same entry
	_ = must(f.s.FinishKM2Save(bg(), res.Entry.ID, map[string]any{"connection_id": "conn-1", "outcome": KM2Unknown}))
	retry := must(f.s.ReserveKM2Save(bg(), scope, RunKM2Owner(run.ID), km2Rec("k1"), limits))
	if retry.Entry == nil || retry.Entry.ID != res.Entry.ID || retry.Entry.Details["outcome"] != KM2Sending {
		t.Errorf("retry = %+v", retry)
	}

	second := must(f.s.ReserveKM2Save(bg(), scope, RunKM2Owner(run.ID), km2Rec("k2"), limits))
	capped := must(f.s.ReserveKM2Save(bg(), scope, RunKM2Owner(run.ID), km2Rec("k3"), limits))
	if second.Entry == nil || !capped.CapReached || capped.Saves != 2 || capped.Entry != nil {
		t.Errorf("cap = %+v", capped)
	}
	// A refused save is dropped and no longer counts
	mustOK(t, f.s.DropKM2Save(bg(), second.Entry.ID))
	if n := must(f.s.RunKM2Saves(bg(), run.ID)); n != 1 {
		t.Errorf("saves = %d", n)
	}

	// The daily cap counts the connection's real saves across runs; dry runs neither count nor are capped
	other := f.run(crew, RunMeta{})
	day := must(f.s.ReserveKM2Save(bg(), scope, RunKM2Owner(other.ID), km2Rec("k4"), KM2Limits{PerOwner: 5, PerConnectionDay: 1, ConnectionID: "conn-1"}))
	if !day.DayCapReached {
		t.Errorf("day cap = %+v", day)
	}
	dry := StateScope{CrewID: crew.ID, DryRun: true}
	wouldSave := must(f.s.ReserveKM2Save(bg(), dry, RunKM2Owner(other.ID), km2Rec("k5"), KM2Limits{PerOwner: 5, PerConnectionDay: 1, ConnectionID: "conn-1"}))
	if wouldSave.Entry == nil || !wouldSave.Entry.DryRun || wouldSave.Entry.Details["outcome"] != nil {
		t.Errorf("dry run = %+v", wouldSave)
	}

	// Parallel reservations of different documents never pass the cap
	third := f.run(crew, RunMeta{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	reserved := 0
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := f.s.ReserveKM2Save(bg(), scope, RunKM2Owner(third.ID), km2Rec("p"+string(rune('a'+i))), KM2Limits{PerOwner: 3, PerConnectionDay: 100, ConnectionID: "conn-2"})
			mustOK(t, err)
			if r.Entry != nil {
				mu.Lock()
				reserved++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if reserved != 3 {
		t.Errorf("%d parallel reservations passed a cap of 3", reserved)
	}

	cancelled, cancel := context.WithCancel(bg())
	cancel()
	if _, err := f.s.ReserveKM2Save(cancelled, scope, RunKM2Owner(run.ID), km2Rec("k9"), limits); err == nil {
		t.Error("a cancelled reservation succeeded")
	}
	if _, err := f.s.FinishKM2Save(cancelled, res.Entry.ID, map[string]any{}); err == nil {
		t.Error("a cancelled finish succeeded")
	}
	if err := f.s.DropKM2Save(cancelled, res.Entry.ID); err == nil {
		t.Error("a cancelled drop succeeded")
	}
	if _, err := f.s.RunKM2Saves(cancelled, run.ID); err == nil {
		t.Error("a cancelled count succeeded")
	}
}
