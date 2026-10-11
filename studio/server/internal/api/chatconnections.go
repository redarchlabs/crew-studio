package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"crewstudio/internal/auth"
	"crewstudio/internal/km2"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

// A chat turn runs as the person chatting (design section 8). Its run scope holds the connections the crew may use
// in the crew's areas, which can be more than that person may see. These helpers narrow a chat turn to the
// connections the person can see now, checked on every call, so a turn cannot reach a tracker through a
// connection outside the person's areas (or after they lost access).

// runConnections are the connections a run-state call may use: the run's own for crew runs, and for chat turns
// only those the person chatting can still see (in agent chat, also only those still sharing an area with the agent).
func (s *Server) runConnections(ctx context.Context, scope store.RunScope) ([]store.RunConnection, error) {
	if !model.IsChatKind(scope.Kind) {
		return scope.Connections, nil
	}
	visible, err := s.chatterConnectionIDs(ctx, scope)
	if err != nil {
		return nil, err
	}
	out := []store.RunConnection{}
	for _, c := range scope.Connections {
		if visible[c.ID] {
			out = append(out, c)
		}
	}
	return out, nil
}

// chatterConnectionIDs are the ids of the connections a chat turn may still use: for crew chat, the crew's connections
// that the person who started the turn can see; for agent chat (no crew), the connections that person can see and that
// still share an area with the agent as it is now. A person who no longer exists or was disabled sees none, and so does
// an agent that was deleted.
func (s *Server) chatterConnectionIDs(ctx context.Context, scope store.RunScope) (map[string]bool, error) {
	out := map[string]bool{}
	run, err := s.store.GetRun(ctx, scope.RunID)
	if err != nil {
		return nil, err
	}
	if run.CreatedBy == nil {
		return out, nil
	}
	p, err := s.store.LoadPrincipal(ctx, *run.CreatedBy)
	if err == nil && run.APIKeyID != nil {
		// A turn started with an API key keeps the key's limits, and loses its connections if the key stops working
		p, err = s.store.WithKeyLimits(ctx, p, *run.APIKeyID)
	}
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var conns []model.Connection
	if scope.CrewID == "" {
		conns, err = s.agentChatConnections(ctx, scope, p)
	} else {
		conns, err = s.store.CrewConnections(ctx, scope.CrewID, scopeOf(p))
	}
	if err != nil {
		return nil, err
	}
	for _, c := range conns {
		out[c.ID] = true
	}
	return out, nil
}

// visibleTrackingItems leaves out, for chat turns, the tracking items behind connections the person cannot see.
func (s *Server) visibleTrackingItems(w http.ResponseWriter, r *http.Request, items []model.TrackingItem) ([]model.TrackingItem, bool) {
	call := runCallOf(r)
	if !model.IsChatKind(call.scope.Kind) {
		return items, true
	}
	visible, err := s.chatterConnectionIDs(r.Context(), call.scope)
	if err != nil {
		failErr(w, r, s.log, err)
		return nil, false
	}
	out := []model.TrackingItem{}
	for _, it := range items {
		if visible[it.ConnectionID] {
			out = append(out, it)
		}
	}
	return out, true
}

// agentChatConnections are the connections an agent-chat turn may still read through, besides being frozen and enabled:
// those p can see that share an area with the agent's current areas. Both are checked on every call, so taking the
// person or the agent out of an area stops the reads mid-turn.
func (s *Server) agentChatConnections(ctx context.Context, scope store.RunScope, p *auth.Principal) ([]model.Connection, error) {
	agent, err := s.store.GetAgent(ctx, scope.ChatAgentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	visible, err := s.store.ListConnections(ctx, scopeOf(p))
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(visible, func(c model.Connection) bool {
		return !slices.ContainsFunc(c.AreaIDs, func(area string) bool { return slices.Contains(agent.AreaIDs, area) })
	}), nil
}

// runTrackerClient loads a tracker connection a run-state call may use: for an agent-chat turn (no crew) the one its
// spec froze, otherwise one the run's crew may use.
func (s *Server) runTrackerClient(ctx context.Context, scope store.RunScope, connectionID string) (model.Connection, tracker.Client, error) {
	if scope.CrewID == "" {
		return s.trackers.ForAgentChat(ctx, connectionID)
	}
	return s.trackers.ForCrew(ctx, connectionID, scope.CrewID)
}

// runKM2Client is runTrackerClient for KM2.
func (s *Server) runKM2Client(ctx context.Context, scope store.RunScope, connectionID string) (model.Connection, *km2.Client, error) {
	if scope.CrewID == "" {
		return s.km2.ForAgentChat(ctx, connectionID)
	}
	return s.km2.ForCrew(ctx, connectionID, scope.CrewID)
}

// km2AmbiguousText tells a run that it may use several KM2 connections (names, only ones the person can see) and so
// cannot tell which KM2 profile to act as.
func km2AmbiguousText(scope store.RunScope, names []string) string {
	list := strings.Join(names, ", ")
	if scope.CrewID == "" && scope.ChatAgentID != "" {
		return "This agent may read more than one KM2 connection (" + list + "), so Crew Studio cannot tell which KM2 profile to " +
			"act as. Someone who can edit the agent should list only one under Connections it may read."
	}
	return "This crew may use more than one KM2 connection (" + list + "), so Crew Studio cannot tell which KM2 profile to act as. " +
		"Ask an administrator to give the crew only one."
}

// noConnectionText tells a run that it has no connection of a kind, and who can give it one.
func noConnectionText(scope store.RunScope, what string) string {
	if scope.CrewID == "" && scope.ChatAgentID != "" {
		return "This agent has no " + what + " it may read in chat that you can see. Someone who can edit the agent can list one under " +
			"Connections it may read."
	}
	return "This crew has no " + what + " it may use. Ask an administrator to give it one."
}
