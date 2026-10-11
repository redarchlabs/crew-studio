package api

import (
	"context"
	"net/http"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

// Questions during a hand-off (docs/agent-chat-handoffs-design.md, section 6, slice 3c): a specialist asks the lead,
// and the lead may bring the question to the person, as a crew's agent asks a person. Those questions belong to the
// conversation's owner, who answers them through POST /questions/{id}/answer, signed in; here they are listed with
// their conversation.

// conversationQuestions lists the questions of a conversation's hand-offs, open ones first (owner only).
func (s *Server) conversationQuestions(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	conv, _, err := s.chat.OwnConversation(r.Context(), principal(r), id)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	list, err := s.store.ConversationQuestions(r.Context(), conv.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, readableQuestions(principal(r), list))
}

// openConversationQuestions are the questions a conversation view lists: the open ones of its hand-offs (none for a
// conversation with a crew, and none for a caller who is not signed in; see readableQuestions).
func (s *Server) openConversationQuestions(ctx context.Context, p *auth.Principal, conv model.Conversation) ([]model.HumanQuestion, error) {
	if conv.AgentID == nil {
		return []model.HumanQuestion{}, nil
	}
	list, err := s.store.OpenConversationQuestions(ctx, conv.ID)
	return readableQuestions(p, list), err
}
