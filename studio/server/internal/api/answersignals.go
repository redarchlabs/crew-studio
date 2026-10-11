package api

import "sync"

// answerSignals wakes run-state requests waiting on a question (long-poll) the moment someone answers it, so the
// runner hears the answer immediately instead of on its next poll.
type answerSignals struct {
	mu      sync.Mutex
	waiters map[string]*answerWait
}

// answerWait is shared by everyone waiting on one question; it is dropped when the last of them stops waiting.
type answerWait struct {
	ch    chan struct{}
	count int
}

func newAnswerSignals() *answerSignals {
	return &answerSignals{waiters: map[string]*answerWait{}}
}

// wait returns a channel that is closed when questionID is answered. Every wait must be paired with a forget.
func (a *answerSignals) wait(questionID string) <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	w, found := a.waiters[questionID]
	if !found {
		w = &answerWait{ch: make(chan struct{})}
		a.waiters[questionID] = w
	}
	w.count++
	return w.ch
}

// signal wakes everyone waiting on questionID.
func (a *answerSignals) signal(questionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if w, found := a.waiters[questionID]; found {
		close(w.ch)
		delete(a.waiters, questionID)
	}
}

// forget ends one wait on questionID. The entry stays while others still wait on the same channel, so a later
// answer still wakes them.
func (a *answerSignals) forget(questionID string, ch <-chan struct{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w, found := a.waiters[questionID]
	if !found || (<-chan struct{})(w.ch) != ch {
		return // already signalled (and removed), or a newer wait
	}
	if w.count--; w.count <= 0 {
		delete(a.waiters, questionID)
	}
}
