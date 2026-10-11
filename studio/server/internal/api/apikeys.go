package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// accountRoutes are about the signed-in person's own account.
func (s *Server) accountRoutes(r chi.Router) {
	r.Route("/account/api-keys", func(r chi.Router) {
		// Anyone may see and revoke their own keys, even after losing the right to create them
		r.Get("/", s.listMyAPIKeys)
		r.With(s.need(auth.AccountAPIKeys), s.sessionOnly).Post("/", s.createMyAPIKey)
		r.Delete("/{keyId}", s.revokeMyAPIKey)
	})
}

// sessionOnly refuses requests made with an API key. It guards what a stolen key could use to keep access after
// it is revoked: issuing credentials, changing accounts, roles or sign-in settings.
func (s *Server) sessionOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal(r).Via == auth.ViaAPIKey {
			fail(w, http.StatusForbidden, "forbidden", "This needs you signed in to Crew Studio; API keys cannot do it.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// keyMayUseREST refuses API keys that are limited to MCP. MCP tool calls reach the handlers another way, so this
// only guards /api.
func (s *Server) keyMayUseREST(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := principal(r); p != nil && p.Via == auth.ViaAPIKey && p.NoREST {
			fail(w, http.StatusForbidden, "forbidden", "This API key works only with MCP (/mcp), not the REST API.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// adminChangesNeedSession applies sessionOnly to every admin request except reads.
func (s *Server) adminChangesNeedSession(next http.Handler) http.Handler {
	guarded := s.sessionOnly(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		guarded.ServeHTTP(w, r)
	})
}

type newKeyRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at"`
	// Left out, a key works on both channels with everything its account holds
	AllowAPI    *bool    `json:"allow_api"`
	AllowMCP    *bool    `json:"allow_mcp"`
	Permissions []string `json:"permissions"`
	AreaIDs     []string `json:"area_ids"`
}

// keyAccount is what the key's account holds: the most a key's limits may ask for. name and possessive refer to
// the account in messages ("you" and "your" for your own keys).
type keyAccount struct {
	permissions      map[string]bool
	allAreas         bool
	areaIDs          []string
	name, possessive string
}

func accountOfPrincipal(p *auth.Principal) keyAccount {
	return keyAccount{permissions: p.Permissions, allAreas: p.AllAreas, areaIDs: p.AreaIDs, name: "you", possessive: "your"}
}

func accountOfUser(u model.User) keyAccount {
	perms := map[string]bool{}
	for _, k := range u.EffectivePermissions {
		perms[k] = true
	}
	return keyAccount{permissions: perms, allAreas: u.AllAreas, areaIDs: u.AreaIDs, name: "this account", possessive: "this account's"}
}

type newKeyResponse struct {
	Key    model.APIKey `json:"key"`
	Secret string       `json:"secret"`
}

var bothKinds = []string{auth.KindHuman, auth.KindService}

// keyFilter reads paging and the q, kind and status filters, answering 422 itself when they are not valid.
func (s *Server) keyFilter(w http.ResponseWriter, r *http.Request) (store.APIKeyFilter, bool) {
	q := r.URL.Query()
	f := store.APIKeyFilter{Kinds: bothKinds, Query: q.Get("q"), Kind: q.Get("kind"), Status: q.Get("status")}
	f.Limit, f.Offset = pageParams(r)
	var problems []string
	if f.Kind != "" && f.Kind != auth.KindHuman && f.Kind != auth.KindService {
		problems = append(problems, `kind: must be "human" or "service"`)
	}
	if f.Status != "" && f.Status != "active" && f.Status != "expired" && f.Status != "revoked" {
		problems = append(problems, `status: must be "active", "expired" or "revoked"`)
	}
	if utf8.RuneCountInString(f.Query) > 200 {
		problems = append(problems, "q: at most 200 characters")
	}
	if len(problems) > 0 {
		failErr(w, r, s.log, invalid(problems...))
		return f, false
	}
	return f, true
}

// revokeRule decides, for a page of keys, which ones the caller may revoke.
type revokeRule func(ctx context.Context, keys []model.AccountAPIKey) (func(model.AccountAPIKey) bool, error)

func revokeAll(allowed bool) revokeRule {
	return func(context.Context, []model.AccountAPIKey) (func(model.AccountAPIKey) bool, error) {
		return func(model.AccountAPIKey) bool { return allowed }, nil
	}
}

// listKeys answers with a page of keys, marking which ones the caller may revoke.
func (s *Server) listKeys(w http.ResponseWriter, r *http.Request, f store.APIKeyFilter, rule revokeRule) {
	keys, total, err := s.store.ListAPIKeys(r.Context(), f)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	canRevoke, err := rule(r.Context(), keys)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	out := make([]model.AccountAPIKey, len(keys))
	for i, k := range keys {
		k.CanRevoke = canRevoke(k)
		out[i] = k
	}
	page(w, out, model.Page{Total: total, Limit: f.Limit, Offset: f.Offset})
}

// auditKey records a key being created or revoked. The secret is never recorded.
func (s *Server) auditKey(r *http.Request, action string, key model.APIKey, owner model.User) {
	p := principal(r)
	s.audit(r, action, p.UserID, p.Name, map[string]any{
		"key_id": key.ID, "key_name": key.Name, "prefix": key.Prefix,
		"account_id": owner.ID, "account_name": owner.Name, "account_kind": owner.Kind,
		"allow_api": key.AllowAPI, "allow_mcp": key.AllowMCP, "permissions": key.Permissions, "area_ids": key.AreaIDs,
	})
}

// self is the signed-in principal as an account, for audit details.
func self(p *auth.Principal) model.User {
	return model.User{ID: p.UserID, Name: p.Name, Kind: p.Kind}
}

// decodeNewKey reads and checks a request for a key of this account, answering 422 itself when it is not valid.
func (s *Server) decodeNewKey(w http.ResponseWriter, r *http.Request, account keyAccount) (newKeyRequest, model.APIKeyLimits, bool) {
	var req newKeyRequest
	if !decode(w, r, &req) {
		return req, model.APIKeyLimits{}, false
	}
	req.Name = strings.TrimSpace(req.Name)
	var problems []string
	if req.Name == "" || len(req.Name) > 120 {
		problems = append(problems, "name: is required (at most 120 characters)")
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		problems = append(problems, "expires_at: must be in the future")
	}
	limits, limitProblems, err := s.keyLimits(r.Context(), req, account)
	if err != nil {
		failErr(w, r, s.log, err)
		return req, limits, false
	}
	problems = append(problems, limitProblems...)
	if len(problems) > 0 {
		failErr(w, r, s.log, invalid(problems...))
		return req, limits, false
	}
	return req, limits, true
}

// keyLimits checks the limits a request asks for. A key may be given only permissions and areas its account holds
// now; it is checked against the account again on every request, so it never outgrows it later either.
func (s *Server) keyLimits(ctx context.Context, req newKeyRequest, account keyAccount) (model.APIKeyLimits, []string, error) {
	limits := model.FullAccessKey()
	if req.AllowAPI != nil {
		limits.AllowAPI = *req.AllowAPI
	}
	if req.AllowMCP != nil {
		limits.AllowMCP = *req.AllowMCP
	}
	var problems []string
	if !limits.AllowAPI && !limits.AllowMCP {
		problems = append(problems, "allow_api, allow_mcp: a key must work on the REST API, MCP or both")
	}
	if req.Permissions != nil {
		limits.Permissions = []string{}
		if len(req.Permissions) == 0 {
			problems = append(problems, "permissions: choose at least one, or leave it out for everything "+account.name+" can do")
		}
		for _, key := range auth.Catalog {
			if slices.Contains(req.Permissions, key) {
				limits.Permissions = append(limits.Permissions, key)
			}
		}
		for _, key := range req.Permissions {
			switch {
			case !auth.Known(key):
				problems = append(problems, "permissions: "+key+" is not a permission")
			case !account.permissions[key]:
				problems = append(problems, "permissions: "+key+" is not one of "+account.possessive+" permissions, so a key cannot have it")
			}
		}
	}
	if req.AreaIDs != nil {
		areaProblems, err := s.keyAreaProblems(ctx, req.AreaIDs, account)
		if err != nil {
			return limits, nil, err
		}
		problems = append(problems, areaProblems...)
		limits.AreaIDs = []string{}
		for _, id := range req.AreaIDs {
			if !slices.Contains(limits.AreaIDs, id) {
				limits.AreaIDs = append(limits.AreaIDs, id)
			}
		}
	}
	return limits, problems, nil
}

// keyAreaProblems says which requested areas a key of the account cannot be limited to.
func (s *Server) keyAreaProblems(ctx context.Context, areaIDs []string, account keyAccount) ([]string, error) {
	if len(areaIDs) == 0 {
		return []string{"area_ids: choose at least one, or leave it out for all of " + account.possessive + " areas"}, nil
	}
	areas, err := s.store.ListAreas(ctx)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, id := range areaIDs {
		exists := slices.ContainsFunc(areas, func(a model.Area) bool { return a.ID == id })
		switch {
		case !uuidPattern.MatchString(id) || !exists:
			problems = append(problems, "area_ids: "+id+" is not a functional area")
		case !account.allAreas && !slices.Contains(account.areaIDs, id):
			problems = append(problems, "area_ids: "+id+" is not one of "+account.possessive+" areas, so a key cannot be limited to it")
		}
	}
	return problems, nil
}

// Personal keys

func (s *Server) listMyAPIKeys(w http.ResponseWriter, r *http.Request) {
	f, valid := s.keyFilter(w, r)
	if !valid {
		return
	}
	f.UserID = principal(r).UserID
	s.listKeys(w, r, f, revokeAll(true))
}

func (s *Server) createMyAPIKey(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	req, limits, valid := s.decodeNewKey(w, r, accountOfPrincipal(p))
	if !valid {
		return
	}
	key, secret, err := s.store.CreatePersonalAPIKey(r.Context(), p.UserID, req.Name, req.ExpiresAt, limits)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditKey(r, "api_key_created", key, self(p))
	created(w, newKeyResponse{Key: key, Secret: secret})
}

func (s *Server) revokeMyAPIKey(w http.ResponseWriter, r *http.Request) {
	keyID, valid := pathUUID(w, r, "keyId")
	if !valid {
		return
	}
	key, changed, err := s.store.RevokeAPIKey(r.Context(), principal(r).UserID, keyID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if changed {
		s.auditKey(r, "api_key_revoked", key, self(principal(r)))
	}
	noContent(w)
}

// Admin: every key, and keys of one account

// listAllAPIKeys returns the keys of people (admin.users) and of service accounts (admin.service_accounts).
func (s *Server) listAllAPIKeys(w http.ResponseWriter, r *http.Request) {
	f, valid := s.keyFilter(w, r)
	if !valid {
		return
	}
	p := principal(r)
	f.Kinds = nil
	if p.Can(auth.AdminUsers) {
		f.Kinds = append(f.Kinds, auth.KindHuman)
	}
	if p.Can(auth.AdminServiceAccounts) {
		f.Kinds = append(f.Kinds, auth.KindService)
	}
	// Revoking needs everything the owner holds (see outranks), so load this page's owners in one query
	s.listKeys(w, r, f, func(ctx context.Context, keys []model.AccountAPIKey) (func(model.AccountAPIKey) bool, error) {
		ids := make([]string, len(keys))
		for i, k := range keys {
			ids[i] = k.UserID
		}
		owners, err := s.store.UsersByID(ctx, ids)
		if err != nil {
			return nil, err
		}
		return func(k model.AccountAPIKey) bool {
			owner, found := owners[k.UserID]
			return found && outranks(p, owner) == ""
		}, nil
	})
}

// listAccountAPIKeys lists the keys of a person or service account.
func (s *Server) listAccountAPIKeys(w http.ResponseWriter, r *http.Request) {
	u, found := s.loadUser(w, r)
	if !found {
		return
	}
	f, valid := s.keyFilter(w, r)
	if !valid {
		return
	}
	f.UserID = u.ID
	s.listKeys(w, r, f, revokeAll(outranks(principal(r), u) == ""))
}

// createAPIKey issues a key for a service account. Admins never issue keys for people: that would let them act
// as that person.
func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	u, found := s.serviceAccount(w, r)
	if !found {
		return
	}
	req, limits, valid := s.decodeNewKey(w, r, accountOfUser(u))
	if !valid {
		return
	}
	key, secret, err := s.store.CreateAPIKey(r.Context(), u.ID, req.Name, req.ExpiresAt, limits)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditKey(r, "api_key_created", key, u)
	created(w, newKeyResponse{Key: key, Secret: secret})
}

// revokeAccountAPIKey revokes a key of a person or service account the caller may manage.
func (s *Server) revokeAccountAPIKey(w http.ResponseWriter, r *http.Request) {
	u, found := s.manageUser(w, r)
	if !found {
		return
	}
	keyID, valid := pathUUID(w, r, "keyId")
	if !valid {
		return
	}
	key, changed, err := s.store.RevokeAPIKey(r.Context(), u.ID, keyID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if changed {
		s.auditKey(r, "api_key_revoked", key, u)
	}
	noContent(w)
}
