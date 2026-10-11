package chat

import (
	"context"
	"slices"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// KM2 in chat (design section 9): the front desk may search KM2 and ask it questions, read-only, when the person
// chatting holds state.view and the crew may use a KM2 connection that person can see. Saving to KM2 is not offered
// in chat; scheduled and manual runs save with km_save_document, which Studio logs and caps.

// KM2ToolKeys lists the KM2 tools p's front desk gets when the crew has a KM2 connection p can see.
func KM2ToolKeys(p *auth.Principal) []string {
	keys := []string{}
	for _, t := range Tools {
		if t.KM2 && holdsAll(p, t.Needs) {
			keys = append(keys, t.Key)
		}
	}
	return keys
}

// withKM2Tools adds the KM2 tools to keys when the crew may use an enabled KM2 connection that shares an area with
// it and that p can see.
func (s *Service) withKM2Tools(ctx context.Context, p *auth.Principal, crew model.Crew, keys []string) ([]string, error) {
	extra := KM2ToolKeys(p)
	if len(extra) == 0 {
		return keys, nil
	}
	has, err := hasKM2Connection(ctx, s.store, p, crew)
	if err != nil || !has {
		return keys, err
	}
	return append(slices.Clone(keys), extra...), nil
}

func hasKM2Connection(ctx context.Context, st *store.Store, p *auth.Principal, crew model.Crew) (bool, error) {
	conns, err := st.CrewConnections(ctx, crew.ID, store.Scope{All: p.AllAreas, AreaIDs: p.AreaIDs})
	if err != nil {
		return false, err
	}
	for _, c := range conns {
		shared := slices.ContainsFunc(c.AreaIDs, func(a string) bool { return slices.Contains(crew.AreaIDs, a) })
		if c.Enabled && c.Kind == model.KindKM2 && shared {
			return true, nil
		}
	}
	return false, nil
}
