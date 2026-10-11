package api

import (
	"errors"
	"net/http"
	"slices"

	"crewstudio/internal/chat"
	"crewstudio/internal/model"
)

// A chat turn's proposal tools (start_work_order, propose_note_change, and in agent chat the counterparts of changing
// tools such as propose_tracker_comment) call POST /api/run-state/proposals with the run's token. Only chat runs and
// hand-offs may propose; nothing is applied until the person confirms it in the chat. The calling agent's role says
// on the card who proposed it.

type proposeRequest struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

type proposeResponse struct {
	MessageID string          `json:"message_id"`
	Proposal  *model.Proposal `json:"proposal"`
	State     string          `json:"state"`
}

func (s *Server) runPropose(w http.ResponseWriter, r *http.Request) {
	var in proposeRequest
	if !decode(w, r, &in) {
		return
	}
	call := runCallOf(r)
	m, err := s.chat.ProposeAs(r.Context(), call.scope, call.agentRole, in.Type, in.Payload)
	if err != nil {
		var denied *chat.DeniedError
		if errors.As(err, &denied) {
			s.journal(r, "propose", "Chat: proposal refused: "+denied.Message, "")
		} else {
			s.journal(r, "propose", "Chat: proposal refused: "+problemText(err), "")
		}
		s.chatFail(w, r, err)
		return
	}
	s.journal(r, "propose", "Chat: proposed "+m.Proposal.Summary+" (waits for the person to confirm)", "")
	created(w, proposeResponse{MessageID: m.ID, Proposal: m.Proposal, State: model.ProposalPending})
}

// refuseChat stops a chat turn or hand-off from writing crew state directly; it proposes changes instead.
func (s *Server) refuseChat(tool string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if model.IsChatKind(runCallOf(r).scope.Kind) {
			s.journal(r, tool, "State: write refused, chat turns only read state and propose changes", "")
			fail(w, http.StatusForbidden, "forbidden", "Chat turns cannot change crew state directly; propose the change so the person can confirm it.")
			return
		}
		next(w, r)
	}
}

// chatTool lets a chat turn call a read endpoint only when its frozen spec gave it that tool: the tools follow the
// person chatting's permissions (no state.view, no notes or action log) and, in agent chat, the agent's connections.
// Any agent of the turn (or hand-off) may have it: specialists reach their tools through this API too.
func (s *Server) chatTool(tool string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		call := runCallOf(r)
		if model.IsChatKind(call.scope.Kind) {
			spec, err := s.store.GetRunSpec(r.Context(), call.scope.RunID)
			if err != nil {
				failErr(w, r, s.log, err)
				return
			}
			if spec.Chat == nil || !slices.Contains(turnTools(spec), tool) {
				s.journal(r, tool, "State: refused, this chat turn was not given "+tool, "")
				fail(w, http.StatusForbidden, "forbidden", "This chat turn may not use "+tool+".")
				return
			}
		}
		next(w, r)
	}
}

// turnTools are every tool a chat turn's agents were given: the front desk's or lead's, and each specialist's.
func turnTools(spec model.RunSpec) []string {
	tools := slices.Clone(spec.Chat.Tools)
	for _, a := range spec.Agents {
		tools = append(tools, a.Tools...)
	}
	return tools
}
