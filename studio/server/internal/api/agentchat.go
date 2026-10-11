package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// Agent chat (docs/agent-chat-design.md): GET /chat/agents lists the agents the caller can chat with, and
// /agents/{id}/chat holds an agent's chat settings with the specialists it may ask and the crews it may start.

// agentChatRoutes hangs off /agents (see mountLibrary).
func (s *Server) agentChatRoutes(r chi.Router) {
	r.With(s.need(auth.AgentsView)).Get("/{id}/chat", s.getAgentChatSettings)
	r.With(s.need(auth.AgentsEdit)).Put("/{id}/chat", s.putAgentChatSettings)
}

func (s *Server) listChatAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ChatAgents(r.Context(), scopeOf(principal(r)))
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, agents)
}

// loadAgent fetches an agent in the caller's areas, or answers 404.
func (s *Server) loadAgent(w http.ResponseWriter, r *http.Request) (model.Agent, bool) {
	id, valid := pathID(w, r)
	if !valid {
		return model.Agent{}, false
	}
	agent, err := s.store.GetAgent(r.Context(), id)
	if err == nil && !principal(r).SeesAreas(agent.AreaIDs) {
		err = store.ErrNotFound
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return agent, false
	}
	return agent, true
}

func (s *Server) getAgentChatSettings(w http.ResponseWriter, r *http.Request) {
	agent, found := s.loadAgent(w, r)
	if !found {
		return
	}
	settings, err := s.store.AgentChatSettings(r.Context(), agent.ID)
	if err == nil {
		settings, err = s.agentChatFor(r.Context(), principal(r), settings)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, settings)
}

func (s *Server) putAgentChatSettings(w http.ResponseWriter, r *http.Request) {
	agent, found := s.loadAgent(w, r)
	if !found {
		return
	}
	var in model.AgentChatSettings
	if !decode(w, r, &in) {
		return
	}
	in.AgentID = agent.ID
	ctx, p := r.Context(), principal(r)
	before, err := s.store.AgentChatSettings(ctx, agent.ID)
	if err == nil {
		in, err = s.keepHiddenChatRefs(ctx, p, in, before)
	}
	var clean, out model.AgentChatSettings
	if err == nil {
		clean, err = model.NormalizeAgentChatSettings(in)
	}
	if err == nil {
		err = s.checkAgentChatRefs(ctx, p, agent, clean, before)
	}
	if err == nil {
		out, err = s.store.PutAgentChatSettings(ctx, clean, p.UserID)
	}
	if err == nil {
		out, err = s.agentChatFor(ctx, p, out)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, out)
}

// chatRefsSeen says which of the settings' specialists, crews and connections the caller can see (their areas).
type chatRefsSeen struct{ agents, crews, connections map[string]bool }

func (s *Server) visibleChatRefs(ctx context.Context, p *auth.Principal, settings model.AgentChatSettings) (chatRefsSeen, error) {
	var seen chatRefsSeen
	var err error
	if seen.agents, err = s.store.VisibleIDs(ctx, "agent", settings.SpecialistIDs, scopeOf(p)); err != nil {
		return seen, err
	}
	if seen.crews, err = s.store.VisibleIDs(ctx, "crew", settings.CrewIDs, scopeOf(p)); err != nil {
		return seen, err
	}
	seen.connections, err = s.store.VisibleIDs(ctx, "connection", settings.ConnectionIDs, scopeOf(p))
	return seen, err
}

// agentChatFor is an agent's chat settings as the caller sees them: specialists, crews and connections outside their
// areas are left out of the lists and only counted, so their IDs do not show.
func (s *Server) agentChatFor(ctx context.Context, p *auth.Principal, settings model.AgentChatSettings) (model.AgentChatSettings, error) {
	if p.AllAreas {
		return settings, nil
	}
	seen, err := s.visibleChatRefs(ctx, p, settings)
	if err != nil {
		return settings, err
	}
	out := settings
	out.SpecialistIDs = onlyVisible(settings.SpecialistIDs, seen.agents)
	out.CrewIDs = onlyVisible(settings.CrewIDs, seen.crews)
	out.ConnectionIDs = onlyVisible(settings.ConnectionIDs, seen.connections)
	out.HiddenSpecialists = len(settings.SpecialistIDs) - len(out.SpecialistIDs)
	out.HiddenCrews = len(settings.CrewIDs) - len(out.CrewIDs)
	out.HiddenConnections = len(settings.ConnectionIDs) - len(out.ConnectionIDs)
	return out, nil
}

func onlyVisible(ids []string, visible map[string]bool) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if visible[id] {
			out = append(out, id)
		}
	}
	return out
}

// keepHiddenChatRefs puts back the saved specialists, crews and connections the editor cannot see. The editor never
// got them (agentChatFor leaves them out), so saving what they see must not drop them.
func (s *Server) keepHiddenChatRefs(ctx context.Context, p *auth.Principal, in, before model.AgentChatSettings) (model.AgentChatSettings, error) {
	if p.AllAreas {
		return in, nil
	}
	seen, err := s.visibleChatRefs(ctx, p, before)
	if err != nil {
		return in, err
	}
	out := in
	out.SpecialistIDs = keepHiddenRefs(in.SpecialistIDs, before.SpecialistIDs, seen.agents)
	out.CrewIDs = keepHiddenRefs(in.CrewIDs, before.CrewIDs, seen.crews)
	out.ConnectionIDs = keepHiddenRefs(in.ConnectionIDs, before.ConnectionIDs, seen.connections)
	return out, nil
}

// keepHiddenRefs is the list an editor sent with the saved IDs they cannot see put back where they were: each hidden
// ID keeps its old position and the sent IDs fill the other places in order. A hidden ID the editor sent anyway is
// left where they put it.
func keepHiddenRefs(sent, before []string, visible map[string]bool) []string {
	out := make([]string, 0, len(sent)+len(before))
	next := 0
	for _, id := range before {
		switch {
		case !visible[id] && !slices.Contains(sent, id):
			out = append(out, id)
		case next < len(sent):
			out = append(out, sent[next])
			next++
		}
	}
	return append(out, sent[next:]...)
}

// checkAgentChatRefs checks the chat model, specialists, crews and connections exist, that the editor may add the
// specialists, crews and connections they add (their areas; ones already saved in before are kept), that every
// specialist has a role of its own (CrewAI picks a coworker by role), and that every connection is of a kind the
// agent's read tools use.
func (s *Server) checkAgentChatRefs(ctx context.Context, p *auth.Principal, agent model.Agent, in, before model.AgentChatSettings) error {
	if err := checkMayAddConnections(p, in.ConnectionIDs, before.ConnectionIDs); err != nil {
		return err
	}
	if err := s.checkRefs(ctx, p, "agent", "specialist_ids", in.SpecialistIDs, before.SpecialistIDs); err != nil {
		return err
	}
	if err := s.checkRefs(ctx, p, "crew", "crew_ids", in.CrewIDs, before.CrewIDs); err != nil {
		return err
	}
	if err := s.checkRefs(ctx, p, "connection", "connection_ids", in.ConnectionIDs, before.ConnectionIDs); err != nil {
		return err
	}
	seen, err := s.store.VisibleIDs(ctx, "connection", in.ConnectionIDs, scopeOf(p))
	if err != nil {
		return err
	}
	problems, err := s.chatConnectionProblems(ctx, in.ConnectionIDs, seen)
	if err != nil {
		return err
	}
	if in.LLMID != nil {
		if _, err := s.store.GetLLM(ctx, *in.LLMID); errors.Is(err, store.ErrNotFound) {
			problems = append(problems, "llm_id: no such LLM")
		} else if err != nil {
			return err
		}
	}
	crews, err := s.store.VisibleIDs(ctx, "crew", in.CrewIDs, store.AllAreas)
	if err != nil {
		return err
	}
	for _, id := range in.CrewIDs {
		if !crews[id] {
			problems = append(problems, "crew_ids: "+id+" does not exist")
		}
	}
	roles := map[string]string{model.ChatRoleKey(agent.Role): agent.Name}
	for _, id := range in.SpecialistIDs {
		specialist, err := s.store.GetAgent(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			problems = append(problems, "specialist_ids: "+id+" does not exist")
			continue
		}
		if err != nil {
			return err
		}
		if other, taken := roles[model.ChatRoleKey(specialist.Role)]; taken {
			problems = append(problems, fmt.Sprintf("specialist_ids: %s has the same role as %s; give each a role of its own", specialist.Name, other))
			continue
		}
		roles[model.ChatRoleKey(specialist.Role)] = specialist.Name
	}
	if len(problems) > 0 {
		return invalid(problems...)
	}
	return nil
}

// checkMayAddConnections refuses an editor without View connections who adds a connection to the list; the ones
// already saved are kept whoever saves.
func checkMayAddConnections(p *auth.Principal, ids, before []string) error {
	if p.Can(auth.ConnectionsView) {
		return nil
	}
	for _, id := range ids {
		if !slices.Contains(before, id) {
			return &launch.ForbiddenError{Permission: auth.ConnectionsView}
		}
	}
	return nil
}

// chatConnectionProblems names the connections that do not exist or are of a kind no read tool in chat uses, and
// refuses a second KM2 connection (KM2 reads cannot choose between them). Connections the editor cannot see (seen
// false: saved by someone else and kept) are never named.
func (s *Server) chatConnectionProblems(ctx context.Context, ids []string, seen map[string]bool) ([]string, error) {
	var problems []string
	km2s := 0
	for _, id := range ids {
		c, err := s.store.GetConnection(ctx, id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			problems = append(problems, "connection_ids: "+id+" does not exist")
		case err != nil:
			return nil, err
		case model.ChatNeedOf(c.Kind) == "" && seen[id]:
			problems = append(problems, fmt.Sprintf("connection_ids: %s is a %s connection; an agent can only read Jira, Linear, KM2 issues, "+
				"KM2 and MCP connections in chat", c.Name, c.Kind))
		case model.ChatNeedOf(c.Kind) == "":
			problems = append(problems, "connection_ids: a connection outside your areas cannot be read in chat; ask someone in its areas to remove it")
		case c.Kind == model.KindKM2:
			km2s++
		}
	}
	if km2s > 1 {
		problems = append(problems, "connection_ids: list at most one KM2 connection; KM2 reads cannot choose between them")
	}
	return problems, nil
}
