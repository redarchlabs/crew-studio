package auth

import (
	"context"
	"slices"
)

const (
	KindHuman   = "human"
	KindService = "service"

	ViaSession = "session"
	ViaAPIKey  = "api_key"
	// ViaSystem is the scheduler or a webhook acting for the person who set it up
	ViaSystem = "system"
)

// Principal is whoever a request (or a scheduled job) acts as, with permissions already resolved.
type Principal struct {
	UserID      string
	Name        string
	Email       string
	Kind        string
	Via         string
	AllAreas    bool
	AreaIDs     []string
	Permissions map[string]bool
	// APIKeyID is set when the request used an API key
	APIKeyID string
	// SessionHash identifies the session for sign-out
	SessionHash string
}

// Can reports whether the principal holds the permission.
func (p *Principal) Can(permission string) bool {
	return p != nil && p.Permissions[permission]
}

// SeesAreas reports whether an item in these areas is visible: an all-areas principal sees everything;
// anyone else needs at least one area in common. Items with no areas are visible only to all-areas principals.
func (p *Principal) SeesAreas(areaIDs []string) bool {
	if p == nil {
		return false
	}
	if p.AllAreas {
		return true
	}
	for _, id := range areaIDs {
		if slices.Contains(p.AreaIDs, id) {
			return true
		}
	}
	return false
}

// HasArea reports whether the principal may place items in this area.
func (p *Principal) HasArea(areaID string) bool {
	return p != nil && (p.AllAreas || slices.Contains(p.AreaIDs, areaID))
}

// PermissionList returns the held permissions in catalog order.
func (p *Principal) PermissionList() []string {
	out := []string{}
	for _, key := range Catalog {
		if p.Permissions[key] {
			out = append(out, key)
		}
	}
	return out
}

type principalKey struct{}

// WithPrincipal stores the principal in the context.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the principal, or nil when the request is anonymous.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}
