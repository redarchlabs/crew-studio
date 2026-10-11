package api

import (
	"fmt"
	"net/http"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

// Compacting a running attempt's context. A person asks (POST /runs/{id}/compact); the runner reads the count of
// requests from GET /run-state/control before its LLM calls and compacts each agent's context once per new request.

const compactTool = "compact"

// runControl is what a running crew checks before its LLM calls.
type runControl struct {
	CompactRequests int `json:"compact_requests"`
}

func (s *Server) compactRun(w http.ResponseWriter, r *http.Request) {
	run, found := s.loadRun(w, r)
	if !found {
		return
	}
	p := principal(r)
	if !p.Can(auth.RunsCancel) && !(run.WorkOrderID != nil && p.Can(auth.WorkOrdersCancel)) {
		forbidden(w, auth.RunsCancel)
		return
	}
	// A hand-off compacts by itself past the context threshold; asking is only for crew runs
	if model.IsChatKind(run.Kind) {
		fail(w, http.StatusConflict, "chat_turn", "A chat reply or hand-off cannot be compacted from here; compact the conversation instead.")
		return
	}
	n, err := s.store.RequestCompaction(r.Context(), run.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	payload := map[string]any{"tool": compactTool,
		"message": fmt.Sprintf("%s asked to compact the agents' context", p.Name),
		"detail":  "Each agent's conversation so far is summarised before its next LLM call, so later calls resend fewer tokens."}
	if err := s.stateEvents.RecordStateEvent(r.Context(), run.ID, payload); err != nil {
		s.log.Warn("record compaction request", "run_id", run.ID, "err", err)
	}
	ok(w, runControl{CompactRequests: n})
}

func (s *Server) runControlState(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CompactRequests(r.Context(), runCallOf(r).scope.RunID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, runControl{CompactRequests: n})
}
