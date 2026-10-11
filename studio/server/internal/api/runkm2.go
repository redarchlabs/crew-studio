package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"crewstudio/internal/km2"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// KM2 tools during a run (design section 9; section 11, option B): agents call these with the run token and Studio
// calls KM2 itself, so the KM2 key never leaves the server. Studio, not the agent, picks the connection (and so the
// KM2 profile the key acts as): the one KM2 connection the run may use. Every call is journalled.

const (
	// askCallTimeout bounds a chat answer once started; KM2's model can take longer than the run-state timeout
	askCallTimeout = 100 * time.Second
	// km2TestsPerMinute bounds "Test" on km2 connections per person
	km2TestsPerMinute = 20
	km2TestBurst      = 5
)

// Per run, so one runaway (or prompt-injected) agent cannot spend a key other crews share: searches, and questions,
// which KM2 answers with a model and so cost money
const (
	km2SearchesPerMinute = 30
	km2SearchBurst       = 30
	km2AsksPerMinute     = 6
	km2AskBurst          = 6
	maxOpenKM2Asks       = 2
)

var (
	km2Tests    = newKeyedLimiter(rate.Every(time.Minute/km2TestsPerMinute), km2TestBurst)
	km2Searches = newKeyedLimiter(rate.Every(time.Minute/km2SearchesPerMinute), km2SearchBurst)
	km2Asks     = newKeyedLimiter(rate.Every(time.Minute/km2AsksPerMinute), km2AskBurst)
	km2AsksOpen = &counter{counts: map[string]int{}, max: maxOpenKM2Asks}
)

// km2ReadAllowed answers 429 itself when the run has made too many KM2 reads of this kind just now.
func (s *Server) km2ReadAllowed(w http.ResponseWriter, r *http.Request, tool string, limit *keyedLimiter) bool {
	if limit.allow(runCallOf(r).scope.RunID) {
		return true
	}
	s.journal(r, tool, "KM2: refused, this run is calling "+tool+" too often", "")
	tooMany(w, "This run is calling "+tool+" too often; use what you already found, or wait a minute.")
	return false
}

// km2RunRoutes hangs off /run-state (see runStateRoutes).
func (s *Server) km2RunRoutes(r chi.Router) {
	r.Post("/km2/search", s.chatTool("km_search", s.runKM2Search))
	r.Post("/km2/ask", s.chatTool("km_ask", s.runKM2Ask))
	r.Post("/km2/documents", s.refuseChat("km_save_document", s.runKM2Save))
}

// runKM2 is the run's KM2 connection with its client.
type runKM2 struct {
	conn   model.Connection
	client *km2.Client
}

// km2Of picks the run's KM2 connection: the one km2 connection its crew may use and that shares an area with the
// run (in agent chat: the one its spec froze). Agents cannot choose; with none or more than one the call is refused.
// It answers itself on failure.
func (s *Server) km2Of(w http.ResponseWriter, r *http.Request, tool string) (runKM2, bool) {
	call := runCallOf(r)
	// A chat turn acts as the person chatting: only connections they can still see count, before the ambiguity check,
	// so a refusal never names a connection they cannot see
	visible, err := s.runConnections(r.Context(), call.scope)
	if err != nil {
		failErr(w, r, s.log, err)
		return runKM2{}, false
	}
	var matches []store.RunConnection
	for _, c := range visible {
		if c.Kind == model.KindKM2 {
			matches = append(matches, c)
		}
	}
	switch {
	case len(matches) == 0:
		s.journal(r, tool, "KM2: refused, the run has no KM2 connection", "")
		fail(w, http.StatusConflict, "no_km2", noConnectionText(call.scope, "KM2 connection"))
		return runKM2{}, false
	case len(matches) > 1:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		s.journal(r, tool, "KM2: refused, the run may use more than one KM2 connection", "")
		fail(w, http.StatusConflict, "km2_ambiguous", km2AmbiguousText(call.scope, names))
		return runKM2{}, false
	}
	if !s.km2.Allow(matches[0].ID) {
		s.journal(r, tool, "KM2: refused, too many calls to "+matches[0].Name+" just now", "")
		tooMany(w, "Too many KM2 calls through "+matches[0].Name+" just now; slow down.")
		return runKM2{}, false
	}
	conn, client, err := s.runKM2Client(r.Context(), call.scope, matches[0].ID)
	if err != nil {
		s.km2CallFailed(w, r, tool, "use "+matches[0].Name, err)
		return runKM2{}, false
	}
	return runKM2{conn: conn, client: client}, true
}

// km2CallFailed journals and answers a KM2 failure.
func (s *Server) km2CallFailed(w http.ResponseWriter, r *http.Request, tool, what string, err error) {
	s.journal(r, tool, "KM2: "+what+" failed: "+km2Problem(err), "")
	s.km2Failure(w, r, err)
}

func km2Problem(err error) string {
	var fe *km2.FolderError
	if errors.As(err, &fe) {
		return fe.Message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "KM2 did not answer in time"
	}
	return km2.SafeMessage(err)
}

// km2Failure answers a KM2 failure. KM2's own reasons (conflicts, validation) go in details, which the runner
// hands to the agent fenced as untrusted data.
func (s *Server) km2Failure(w http.ResponseWriter, r *http.Request, err error) {
	var fe *km2.FolderError
	var ke *km2.Error
	switch {
	case errors.As(err, &fe):
		fail(w, fe.Status, fe.Code, fe.Message)
	case errors.As(err, &ke) && ke.RateLimited:
		fail(w, http.StatusTooManyRequests, "km2_rate_limited", ke.Message+".")
	case errors.As(err, &ke):
		status, code := km2Status(ke.Status)
		details := []string{}
		if ke.Detail != "" {
			details = append(details, ke.Detail)
		}
		fail(w, status, code, ke.Message+".", details...)
	case errors.Is(err, context.DeadlineExceeded):
		fail(w, http.StatusBadGateway, "km2_failed", "KM2 did not answer in time.")
	default:
		failErr(w, r, s.log, err)
	}
}

// km2Status maps KM2's status to Studio's answer to the agent.
func km2Status(status int) (int, string) {
	switch status {
	case http.StatusForbidden:
		return http.StatusForbidden, "km2_forbidden"
	case http.StatusNotFound:
		return http.StatusNotFound, "km2_not_found"
	case http.StatusConflict:
		return http.StatusConflict, "km2_conflict"
	case http.StatusRequestEntityTooLarge:
		return http.StatusRequestEntityTooLarge, "too_large"
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return http.StatusUnprocessableEntity, "km2_invalid"
	}
	return http.StatusBadGateway, "km2_failed"
}

type km2SearchView struct {
	Connection string    `json:"connection"`
	Query      string    `json:"query"`
	Hits       []km2.Hit `json:"hits"`
	Total      int       `json:"total"`
}

func (s *Server) runKM2Search(w http.ResponseWriter, r *http.Request) {
	const tool = "km_search"
	var in model.KM2Query
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeKM2Query(in)
	if err != nil {
		s.journal(r, tool, "KM2: search refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	if !s.km2ReadAllowed(w, r, tool, km2Searches) {
		return
	}
	k, found := s.km2Of(w, r, tool)
	if !found {
		return
	}
	res, err := k.client.Search(r.Context(), clean.Query, clean.TopK)
	if err != nil {
		s.km2CallFailed(w, r, tool, "search in "+k.conn.Name, err)
		return
	}
	s.journal(r, tool, fmt.Sprintf("KM2: searched %s, %d passages", k.conn.Name, len(res.Hits)), "Query: "+km2Clip(clean.Query, 200))
	ok(w, km2SearchView{Connection: k.conn.Name, Query: clean.Query, Hits: res.Hits, Total: res.Total})
}

type km2AskView struct {
	Connection string           `json:"connection"`
	Question   string           `json:"question"`
	Answer     string           `json:"answer"`
	Sources    []map[string]any `json:"sources"`
}

func (s *Server) runKM2Ask(w http.ResponseWriter, r *http.Request) {
	const tool = "km_ask"
	var in model.KM2Question
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeKM2Question(in)
	if err != nil {
		s.journal(r, tool, "KM2: question refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	runID := runCallOf(r).scope.RunID
	if !s.km2ReadAllowed(w, r, tool, km2Asks) {
		return
	}
	if !km2AsksOpen.acquire(runID) {
		s.journal(r, tool, "KM2: refused, this run already has questions open", "")
		tooMany(w, "This run already has questions to KM2 open; wait for their answers.")
		return
	}
	defer km2AsksOpen.release(runID)
	k, found := s.km2Of(w, r, tool)
	if !found {
		return
	}
	// KM2's model may take longer than the run-state request timeout; the agent's own timeout still applies
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), askCallTimeout)
	defer cancel()
	answer, err := k.client.Ask(ctx, clean.Question)
	if err != nil {
		s.km2CallFailed(w, r, tool, "ask "+k.conn.Name, err)
		return
	}
	s.journal(r, tool, fmt.Sprintf("KM2: asked %s, answer with %d sources", k.conn.Name, len(answer.Sources)), "Question: "+km2Clip(clean.Question, 200))
	ok(w, km2AskView{Connection: k.conn.Name, Question: clean.Question, Answer: answer.Answer, Sources: answer.Sources})
}

// testKM2Connection lists the folders the connection's key can see.
func (s *Server) testKM2Connection(w http.ResponseWriter, r *http.Request, c model.Connection) {
	if !km2Tests.allow(principal(r).UserID) {
		tooMany(w, "KM2 connections are being tested too quickly; wait a moment.")
		return
	}
	ok(w, s.km2.Test(r.Context(), c))
}

// km2ConnectionFolders lists the folders a KM2 connection's key can see, with the folders it allows, for pickers.
// Anyone who may see the connection's areas and either view connections or manage schedules may ask (report
// destinations are chosen on schedules). It spends the same budget as Test; the key never leaves the server.
func (s *Server) km2ConnectionFolders(w http.ResponseWriter, r *http.Request) {
	c, found := s.loadConnection(w, r)
	if !found {
		return
	}
	if c.Kind != model.KindKM2 {
		fail(w, http.StatusUnprocessableEntity, "not_km2", c.Name+" is not a KM2 connection, so it has no folders.")
		return
	}
	s.answerKM2Folders(w, r, c)
}

// promoteFolders lists the folders of the KM2 connection that promoting this crew's memories saves through.
func (s *Server) promoteFolders(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	c, found := s.promotionConnection(w, r, crew)
	if !found {
		return
	}
	s.answerKM2Folders(w, r, c)
}

func (s *Server) answerKM2Folders(w http.ResponseWriter, r *http.Request, c model.Connection) {
	if !km2Tests.allow(principal(r).UserID) {
		tooMany(w, "KM2 folders are being listed too quickly; wait a moment.")
		return
	}
	ok(w, s.km2.FolderList(r.Context(), c))
}
