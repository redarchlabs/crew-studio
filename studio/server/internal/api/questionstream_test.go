package api

import (
	"testing"
	"time"

	"crewstudio/internal/model"
)

func TestQuestionHubWakesOnlyThePeopleConcerned(t *testing.T) {
	h := newQuestionHub()
	sam, unsubSam := h.subscribe("sam")
	samTab2, unsubSamTab2 := h.subscribe("sam")
	vic, unsubVic := h.subscribe("vic")
	woken := func(ch <-chan struct{}) bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}
	who := "sam"
	h.notify(&who)
	h.notify(&who) // coalesced: one pending wake is enough
	if !woken(sam) || !woken(samTab2) || woken(sam) {
		t.Error("sam's streams should each wake once")
	}
	if woken(vic) {
		t.Error("vic was woken for sam's question")
	}
	// An unaddressed question wakes everyone
	h.notify(nil)
	if !woken(sam) || !woken(samTab2) || !woken(vic) {
		t.Error("an unaddressed question should wake every stream")
	}
	unsubSam()
	unsubSamTab2()
	unsubVic()
	if len(h.subs) != 0 {
		t.Errorf("subs left = %d", len(h.subs))
	}
}

func TestNextQuestionCheck(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	if got := nextQuestionCheck(nil, now); got != questionRefresh {
		t.Errorf("empty = %v", got)
	}
	list := []model.HumanQuestion{{DeadlineAt: now.Add(10 * time.Minute)}, {DeadlineAt: now.Add(20 * time.Second)}, {DeadlineAt: now.Add(-time.Minute)}}
	if got := nextQuestionCheck(list, now); got != 21*time.Second {
		t.Errorf("soonest = %v", got)
	}
}
