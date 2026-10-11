package api

import (
	"errors"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// chatSettingsRoutes hangs off /crews (see mountLibrary): whether people can chat with the crew, which agent
// answers, the chat model and the daily budget per conversation.
func (s *Server) chatSettingsRoutes(r chi.Router) {
	r.With(s.need(auth.CrewsView)).Get("/{id}/chat", s.getChatSettings)
	r.With(s.need(auth.CrewsEdit)).Put("/{id}/chat", s.putChatSettings)
}

func (s *Server) getChatSettings(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadCrew(w, r)
	if !found {
		return
	}
	settings, err := s.store.ChatSettings(r.Context(), crew.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, settings)
}

func (s *Server) putChatSettings(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadCrew(w, r)
	if !found {
		return
	}
	var in model.ChatSettings
	if !decode(w, r, &in) {
		return
	}
	in.CrewID = crew.ID
	clean, err := model.NormalizeChatSettings(in)
	if err == nil {
		err = s.checkChatRefs(r, crew, clean)
	}
	var out model.ChatSettings
	if err == nil {
		out, err = s.store.PutChatSettings(r.Context(), clean, principal(r).UserID)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, out)
}

// checkChatRefs checks the front desk is one of the crew's agents and the chat model exists.
func (s *Server) checkChatRefs(r *http.Request, crew model.Crew, in model.ChatSettings) error {
	var problems []string
	if in.FrontDeskAgentID != nil && !slices.Contains(crew.AgentIDs, *in.FrontDeskAgentID) {
		problems = append(problems, "front_desk_agent_id: must be one of the crew's agents (save the crew first if you just added it)")
	}
	if in.LLMID != nil {
		if _, err := s.store.GetLLM(r.Context(), *in.LLMID); errors.Is(err, store.ErrNotFound) {
			problems = append(problems, "llm_id: no such LLM")
		} else if err != nil {
			return err
		}
	}
	if len(problems) > 0 {
		return invalid(problems...)
	}
	return nil
}
