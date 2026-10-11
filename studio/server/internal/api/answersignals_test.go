package api

import (
	"testing"
	"time"
)

func woken(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	case <-time.After(100 * time.Millisecond):
		return false
	}
}

func TestAnswerSignalsWakeEveryWaiter(t *testing.T) {
	a := newAnswerSignals()
	first, second := a.wait("q1"), a.wait("q1")
	other := a.wait("q2")
	a.signal("q1")
	if !woken(first) || !woken(second) {
		t.Fatal("waiters not woken")
	}
	if woken(other) {
		t.Fatal("another question's waiter was woken")
	}
	a.forget("q1", first) // after the signal: nothing to do
	a.signal("q-nobody")  // no waiters: nothing happens
	a.forget("q2", other)
	if len(a.waiters) != 0 {
		t.Errorf("waiters left = %d", len(a.waiters))
	}
	if fresh := a.wait("q2"); fresh == other {
		t.Error("forgotten channel reused")
	}
}

func TestAnswerSignalsKeepWaitingWhileOthersWait(t *testing.T) {
	a := newAnswerSignals()
	early, late := a.wait("q1"), a.wait("q1")
	// The first request times out and stops waiting; the second must still hear the answer
	a.forget("q1", early)
	a.signal("q1")
	if !woken(late) {
		t.Fatal("the remaining waiter missed the answer")
	}
	if len(a.waiters) != 0 {
		t.Errorf("waiters left = %d", len(a.waiters))
	}
}
