// Package runner executes crews in Python subprocesses, records their events, and fans them out live.
package runner

import (
	"sync"

	"crewstudio/internal/model"
)

// subscriberBuffer is how far a slow SSE client may fall behind before it is dropped;
// a dropped client reconnects and catches up from the database.
const subscriberBuffer = 512

// Broker delivers live run events to subscribers (SSE streams).
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[chan model.RunEvent]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: map[string]map[chan model.RunEvent]struct{}{}}
}

// Subscribe returns a channel of new events for a run and a function to stop listening.
// The channel is closed when the run finishes or the subscriber falls too far behind.
func (b *Broker) Subscribe(runID string) (<-chan model.RunEvent, func()) {
	ch := make(chan model.RunEvent, subscriberBuffer)
	b.mu.Lock()
	if b.subs[runID] == nil {
		b.subs[runID] = map[chan model.RunEvent]struct{}{}
	}
	b.subs[runID][ch] = struct{}{}
	b.mu.Unlock()

	return ch, func() { b.remove(runID, ch) }
}

func (b *Broker) remove(runID string, ch chan model.RunEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[runID][ch]; ok {
		delete(b.subs[runID], ch)
		close(ch)
	}
	if len(b.subs[runID]) == 0 {
		delete(b.subs, runID)
	}
}

// Publish sends an event to every subscriber of the run without blocking.
func (b *Broker) Publish(runID string, event model.RunEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[runID] {
		select {
		case ch <- event:
		default:
			delete(b.subs[runID], ch)
			close(ch)
		}
	}
}

// CloseRun ends every subscription to a finished run.
func (b *Broker) CloseRun(runID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[runID] {
		close(ch)
	}
	delete(b.subs, runID)
}

func (b *Broker) subscriberCount(runID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[runID])
}
