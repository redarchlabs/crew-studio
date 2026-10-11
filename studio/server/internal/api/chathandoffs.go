package api

import (
	"context"
	"net/http"

	"crewstudio/internal/auth"
	"crewstudio/internal/chat"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// Hand-offs (docs/agent-chat-handoffs-design.md, slice 3b): background runs started from an agent chat. Only the
// conversation's owner sees them here; cancelling also needs them signed in, as deciding a card does.

// maxHandoffsShown caps the hand-offs a conversation view lists, newest first
const maxHandoffsShown = 20

var errHandoffFinished = &store.ConflictError{Code: "handoff_finished", Message: "This hand-off has already finished."}

func (s *Server) getHandoff(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	h, err := s.chat.OwnHandoff(r.Context(), principal(r), id)
	if err != nil {
		s.chatFail(w, r, err)
		return
	}
	ok(w, h)
}

// cancelHandoff stops a hand-off's run as the Runs page does; the result message then says it was cancelled.
func (s *Server) cancelHandoff(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	p := principal(r)
	h, err := s.chat.OwnHandoff(r.Context(), p, id)
	if err == nil && p.Via != auth.ViaSession {
		err = &chat.DeniedError{Message: "Hand-offs can only be cancelled by signing in to Crew Studio."}
	}
	// A hand-off whose run was deleted is long over
	if err == nil && (h.RunID == nil || h.RunStatus == nil || h.RunStatus.Terminal()) {
		err = errHandoffFinished
	}
	if err == nil {
		_, err = s.runs.Cancel(r.Context(), *h.RunID)
	}
	if err == nil {
		h, err = s.store.GetHandoff(r.Context(), h.ID)
	}
	if err != nil {
		s.chatFail(w, r, err)
		return
	}
	// A question the hand-off was waiting on is closed with it; the owner's badge drops it at once
	s.questions.notify(&p.UserID)
	ok(w, h)
}

// conversationHandoffs are the hand-offs a conversation view lists: none for a conversation with a crew.
func (s *Server) conversationHandoffs(ctx context.Context, conv model.Conversation) ([]model.Handoff, error) {
	if conv.AgentID == nil {
		return []model.Handoff{}, nil
	}
	return s.store.ListHandoffs(ctx, conv.ID, maxHandoffsShown)
}
