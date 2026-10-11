package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

const (
	// maxQuestionStreamsPerPerson bounds the "waiting for you" streams one person holds open (one per tab)
	maxQuestionStreamsPerPerson = 6
	// maxQuestionStreams bounds them for everyone together
	maxQuestionStreams = 500
	allQuestionStreams = "*"
	// questionRefresh re-reads the list now and then, for questions closed by their run ending (which sends no signal)
	questionRefresh = time.Minute
)

// questionHub tells open question streams that a question of theirs was asked or answered, so each re-reads its
// own list. Streams are keyed by person: a question addressed to someone wakes only their streams; an unaddressed one
// (any work order operator may answer it) wakes everyone's.
type questionHub struct {
	mu   sync.Mutex
	subs map[chan struct{}]string
}

func newQuestionHub() *questionHub {
	return &questionHub{subs: map[chan struct{}]string{}}
}

func (h *questionHub) subscribe(userID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.subs[ch] = userID
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// notify wakes the streams of assigneeID, or every stream when assigneeID is nil. One pending wake per stream is
// enough, so a full channel is skipped.
func (h *questionHub) notify(assigneeID *string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, userID := range h.subs {
		if assigneeID != nil && *assigneeID != userID {
			continue
		}
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// streamQuestions pushes the questions waiting on the signed-in person (event "questions", the full list) when it
// connects and whenever one is asked, answered or times out.
func (s *Server) streamQuestions(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !s.questionStreamsAll.acquire(allQuestionStreams) {
		fail(w, http.StatusServiceUnavailable, "too_many_streams", "Too many people are listening for questions right now; try again shortly.")
		return
	}
	defer s.questionStreamsAll.release(allQuestionStreams)
	if !s.questionStreams.acquire(p.UserID) {
		fail(w, http.StatusServiceUnavailable, "too_many_streams", "Too many open tabs are listening for questions; close some.")
		return
	}
	defer s.questionStreams.release(p.UserID)
	changed, unsubscribe := s.questions.subscribe(p.UserID)
	defer unsubscribe()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	sw := &sseWriter{w: w, rc: http.NewResponseController(w)}
	if _, err := fmt.Fprintf(w, "retry: %d\n\n", streamRetryMS); err != nil {
		return
	}

	ctx := r.Context()
	ping := time.NewTicker(streamPingInterval)
	defer ping.Stop()
	refresh := time.NewTimer(0)
	defer refresh.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			fresh, err := s.store.StillValid(ctx, p)
			if err != nil || fresh == nil {
				return
			}
			p = fresh
			if err := sw.comment("ping"); err != nil {
				return
			}
			continue
		case <-changed:
		case <-refresh.C:
		}
		next, err := s.sendQuestions(sw, r, p)
		if err != nil {
			return
		}
		refresh.Reset(next)
	}
}

// sendQuestions writes the person's open questions and says when to look again (the next deadline, or a minute).
func (s *Server) sendQuestions(sw *sseWriter, r *http.Request, p *auth.Principal) (time.Duration, error) {
	list, err := s.store.OpenQuestionsFor(r.Context(), p.UserID, scopeOf(p), p.Can(auth.WorkOrdersCancel))
	if err != nil {
		return 0, err
	}
	list = readableQuestions(p, list)
	data, err := json.Marshal(list)
	if err != nil {
		return 0, err
	}
	if _, err := fmt.Fprintf(sw.w, "event: questions\ndata: %s\n\n", data); err != nil {
		return 0, err
	}
	return nextQuestionCheck(list, time.Now()), sw.rc.Flush()
}

// nextQuestionCheck is how long until the soonest open question times out (plus a second), at most questionRefresh.
func nextQuestionCheck(list []model.HumanQuestion, now time.Time) time.Duration {
	next := questionRefresh
	for _, q := range list {
		if until := q.DeadlineAt.Sub(now) + time.Second; until > 0 && until < next {
			next = until
		}
	}
	return next
}
