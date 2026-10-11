package chat

import (
	"context"
	"fmt"
	"strings"

	"crewstudio/internal/auth"
	"crewstudio/internal/km2"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

// UseTrackers turns on the tracker tools and tracking-list proposals (when the service has none yet).
func (s *Service) UseTrackers(ts *tracker.Service) {
	if s.trackers == nil {
		s.trackers = ts
	}
}

// UseKM2 turns on KM2 save cards from agent chat (when the service has no KM2 yet).
func (s *Service) UseKM2(k *km2.Service) {
	if s.km2 == nil {
		s.km2 = k
	}
}

func (s *Service) env() Env {
	return Env{Store: s.store, Launcher: s.launcher, Trackers: s.trackers, Chat: s, KM2: s.km2}
}

// toolKeys are the tools the front desk gets when p chats with crew: by p's permissions, and the tracker tools only
// when the crew may use a Jira or Linear connection that p can see.
func (s *Service) toolKeys(ctx context.Context, p *auth.Principal, crew model.Crew) ([]string, error) {
	if s.trackers == nil {
		return ToolKeysFor(p), nil
	}
	conns, err := trackerConnections(ctx, s.store, p, crew)
	if err != nil {
		return nil, err
	}
	return ToolKeysForCrew(p, len(conns) > 0), nil
}

// trackerConnections are the crew's enabled Jira and Linear connections that share an area with it and that p can
// see.
func trackerConnections(ctx context.Context, st *store.Store, p *auth.Principal, crew model.Crew) ([]model.Connection, error) {
	all, err := st.CrewConnections(ctx, crew.ID, store.Scope{All: p.AllAreas, AreaIDs: p.AreaIDs})
	if err != nil {
		return nil, err
	}
	out := []model.Connection{}
	for _, c := range all {
		if c.Enabled && model.IsTrackerKind(c.Kind) && sharesArea(c.AreaIDs, crew.AreaIDs) {
			out = append(out, c)
		}
	}
	return out, nil
}

func sharesArea(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// pickConnection finds the connection a proposal names (by id or name, ignoring case), or the only one when it names
// none.
func pickConnection(conns []model.Connection, wanted string) (model.Connection, error) {
	var matches []model.Connection
	for _, c := range conns {
		if wanted == "" || c.ID == wanted || strings.EqualFold(c.Name, wanted) {
			matches = append(matches, c)
		}
	}
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) == 0 && wanted == "":
		return model.Connection{}, &DeniedError{Message: "This crew has no tracker connection (Jira, Linear or KM2 issues) you can use."}
	case len(matches) == 0:
		return model.Connection{}, &model.ValidationError{Details: []string{fmt.Sprintf("connection: the crew has no tracker connection named %q", wanted)}}
	}
	return model.Connection{}, &model.ValidationError{Details: []string{"connection: say which tracker connection to use"}}
}
