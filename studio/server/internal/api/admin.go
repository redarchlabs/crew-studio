package api

import (
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

func (s *Server) areaRoutes(r chi.Router) {
	r.Route("/areas", func(r chi.Router) {
		// Area names are not secret; every signed-in principal needs them to label items
		r.Get("/", s.listAreas)
		r.With(s.need(auth.AdminAreas)).Post("/", s.createArea)
		r.With(s.need(auth.AdminAreas)).Put("/{id}", s.updateArea)
		r.With(s.need(auth.AdminAreas)).Delete("/{id}", s.deleteArea)
	})
}

func (s *Server) listAreas(w http.ResponseWriter, r *http.Request) {
	areas, err := s.store.ListAreas(r.Context())
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if !principal(r).Can(auth.AdminAreas) {
		for i := range areas {
			areas[i].Usage = nil
		}
	}
	ok(w, areas)
}

func (s *Server) createArea(w http.ResponseWriter, r *http.Request) {
	var in model.Area
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeArea(in)
	if err == nil {
		clean, err = s.store.CreateArea(r.Context(), clean)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	created(w, clean)
}

func (s *Server) updateArea(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	var in model.Area
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeArea(in)
	if err == nil {
		clean, err = s.store.UpdateArea(r.Context(), id, clean)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, clean)
}

func (s *Server) deleteArea(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	if err := s.store.DeleteArea(r.Context(), id); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	noContent(w)
}

func (s *Server) adminRoutes(r chi.Router) {
	r.Use(s.adminChangesNeedSession)
	// The catalog is not secret; people who make their own keys need it to choose a key's permissions
	r.With(s.needAny(auth.AdminRoles, auth.AdminUsers, auth.AdminServiceAccounts, auth.AccountAPIKeys)).Get("/permissions", s.listPermissions)
	r.Route("/roles", func(r chi.Router) {
		r.With(s.needAny(auth.AdminRoles, auth.AdminUsers, auth.AdminServiceAccounts)).Get("/", s.listRoles)
		r.With(s.needAny(auth.AdminRoles, auth.AdminUsers, auth.AdminServiceAccounts)).Get("/{id}", s.getRole)
		r.With(s.need(auth.AdminRoles)).Post("/", s.createRole)
		r.With(s.need(auth.AdminRoles)).Put("/{id}", s.updateRole)
		r.With(s.need(auth.AdminRoles)).Delete("/{id}", s.deleteRole)
	})
	r.Route("/users", func(r chi.Router) {
		r.Use(s.needAny(auth.AdminUsers, auth.AdminServiceAccounts))
		r.Get("/", s.listUsers)
		r.Post("/", s.createUser)
		r.Get("/{id}", s.getUser)
		r.Put("/{id}", s.updateUser)
		r.Delete("/{id}", s.deleteUser)
		r.Put("/{id}/password", s.setPassword)
		r.Delete("/{id}/password", s.clearPassword)
		r.Post("/{id}/sessions/revoke", s.revokeSessions)
		r.Get("/{id}/api-keys", s.listAccountAPIKeys)
		r.Post("/{id}/api-keys", s.createAPIKey)
		r.Delete("/{id}/api-keys/{keyId}", s.revokeAccountAPIKey)
		// The secret is a credential: reading it is as sensitive as rotating it
		r.With(s.sessionOnly).Get("/{id}/signing-secret", s.getSigningSecret)
		r.Post("/{id}/signing-secret/rotate", s.rotateSigningSecret)
	})
	r.With(s.needAny(auth.AdminUsers, auth.AdminServiceAccounts)).Get("/api-keys", s.listAllAPIKeys)
	r.With(s.need(auth.AdminAccess)).Get("/access", s.getAccess)
	r.With(s.need(auth.AdminAccess)).Put("/access", s.putAccess)
	r.With(s.need(auth.AdminAudit)).Get("/audit", s.listAudit)
}

func (s *Server) listPermissions(w http.ResponseWriter, r *http.Request) {
	perms, err := s.store.ListPermissions(r.Context())
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, perms)
}

// Roles

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.store.ListRoles(r.Context())
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, roles)
}

func (s *Server) getRole(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	role, err := s.store.GetRole(r.Context(), id)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, role)
}

// noEscalation: nobody may grant permissions they do not hold themselves. Permissions already granted
// (in before) may stay even if the editor lacks them.
func noEscalation(p *auth.Principal, field string, granted, before []string) []string {
	var problems []string
	for _, perm := range granted {
		if !p.Can(perm) && !slices.Contains(before, perm) {
			problems = append(problems, field+": you cannot grant "+perm+" because you do not hold it")
		}
	}
	return problems
}

func (s *Server) createRole(w http.ResponseWriter, r *http.Request) {
	var in model.Role
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeRole(in, auth.Known)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if problems := noEscalation(principal(r), "permissions", clean.Permissions, nil); len(problems) > 0 {
		failErr(w, r, s.log, invalid(problems...))
		return
	}
	out, err := s.store.CreateRole(r.Context(), clean)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	created(w, out)
}

func (s *Server) updateRole(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	var in model.Role
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeRole(in, auth.Known)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	old, err := s.store.GetRole(r.Context(), id)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if problems := noEscalation(principal(r), "permissions", clean.Permissions, old.Permissions); len(problems) > 0 {
		failErr(w, r, s.log, invalid(problems...))
		return
	}
	out, err := s.store.UpdateRole(r.Context(), id, clean)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, out)
}

func (s *Server) deleteRole(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	if err := s.store.DeleteRole(r.Context(), id); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	noContent(w)
}

// Users and service accounts

// kindPermission is what managing people or service accounts needs.
func kindPermission(kind string) string {
	if kind == auth.KindService {
		return auth.AdminServiceAccounts
	}
	return auth.AdminUsers
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = auth.KindHuman
	}
	if kind != auth.KindHuman && kind != auth.KindService {
		failErr(w, r, s.log, invalid(`kind: must be "human" or "service"`))
		return
	}
	if !principal(r).Can(kindPermission(kind)) {
		forbidden(w, kindPermission(kind))
		return
	}
	users, err := s.store.ListUsers(r.Context(), kind)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, users)
}

// loadUser fetches a person or service account the caller may see.
func (s *Server) loadUser(w http.ResponseWriter, r *http.Request) (model.User, bool) {
	id, valid := pathID(w, r)
	if !valid {
		return model.User{}, false
	}
	u, err := s.store.GetUser(r.Context(), id)
	if err != nil {
		failErr(w, r, s.log, err)
		return u, false
	}
	if !principal(r).Can(kindPermission(u.Kind)) {
		forbidden(w, kindPermission(u.Kind))
		return u, false
	}
	return u, true
}

// outranks reports what the target holds that the caller does not: a permission, an area, or all areas.
// Managing such an account (its password, email, keys, secrets or status) would let the caller take it over.
func outranks(p *auth.Principal, u model.User) string {
	if p.UserID == u.ID {
		return ""
	}
	for _, perm := range u.EffectivePermissions {
		if !p.Can(perm) {
			return "they hold " + perm + ", which you do not"
		}
	}
	if u.AllAreas && !p.AllAreas {
		return "they have all areas, which you do not"
	}
	for _, area := range u.AreaIDs {
		if !p.HasArea(area) {
			return "they belong to a functional area you are not in"
		}
	}
	return ""
}

// manageUser fetches an account the caller may change: they must hold everything the account holds.
func (s *Server) manageUser(w http.ResponseWriter, r *http.Request) (model.User, bool) {
	u, found := s.loadUser(w, r)
	if !found {
		return u, false
	}
	if why := outranks(principal(r), u); why != "" {
		fail(w, http.StatusForbidden, "forbidden", "You cannot change this account: "+why+".")
		return u, false
	}
	return u, true
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	if u, found := s.loadUser(w, r); found {
		ok(w, u)
	}
}

// grantProblems stops an admin giving anyone more than they hold themselves: roles whose permissions they
// lack, permissions they lack, areas they are not in, or all areas without having it.
func (s *Server) grantProblems(r *http.Request, in model.UserInput, old *model.User) ([]string, error) {
	p := principal(r)
	var before model.User
	if old != nil {
		before = *old
	}
	roles, err := s.store.ListRoles(r.Context())
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, roleID := range in.RoleIDs {
		if slices.Contains(before.RoleIDs, roleID) {
			continue
		}
		i := slices.IndexFunc(roles, func(role model.Role) bool { return role.ID == roleID })
		if i < 0 {
			problems = append(problems, "role_ids: "+roleID+" is not a role")
			continue
		}
		for _, perm := range roles[i].Permissions {
			if !p.Can(perm) {
				problems = append(problems, "role_ids: you cannot grant "+roles[i].Name+" because it includes "+perm+", which you do not hold")
				break
			}
		}
	}
	problems = append(problems, noEscalation(p, "permissions", in.Permissions, before.Permissions)...)
	if in.AllAreas && !before.AllAreas && !p.AllAreas {
		problems = append(problems, "all_areas: only someone with all areas can grant all areas")
	}
	existing, err := s.store.ExistingAreaIDs(r.Context(), in.AreaIDs)
	if err != nil {
		return nil, err
	}
	for _, areaID := range in.AreaIDs {
		switch {
		case !existing[areaID]:
			problems = append(problems, "area_ids: "+areaID+" is not a functional area")
		case !p.HasArea(areaID) && !slices.Contains(before.AreaIDs, areaID):
			problems = append(problems, "area_ids: you can only grant functional areas you belong to")
		}
	}
	return problems, nil
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in model.UserInput
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeUser(in, auth.Known)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if !principal(r).Can(kindPermission(clean.Kind)) {
		forbidden(w, kindPermission(clean.Kind))
		return
	}
	problems, err := s.grantProblems(r, clean, nil)
	if err == nil && len(problems) > 0 {
		err = invalid(problems...)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	out, err := s.store.CreateUser(r.Context(), clean)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	created(w, out)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	old, found := s.manageUser(w, r)
	if !found {
		return
	}
	var in model.UserInput
	if !decode(w, r, &in) {
		return
	}
	in.Kind = old.Kind // an account cannot switch between person and service account
	clean, err := model.NormalizeUser(in, auth.Known)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if old.Email != nil && clean.Email != *old.Email && len(old.Identities) > 0 && old.ID != principal(r).UserID {
		// SSO links accounts by email on first sign-in; moving the email of a linked account is a takeover risk
		failErr(w, r, s.log, invalid("email: this person already signs in with SSO; their email cannot be changed here"))
		return
	}
	problems, err := s.grantProblems(r, clean, &old)
	if err == nil && len(problems) > 0 {
		err = invalid(problems...)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	out, err := s.store.UpdateUser(r.Context(), old.ID, clean)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, out)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	u, found := s.manageUser(w, r)
	if !found {
		return
	}
	if u.ID == principal(r).UserID {
		failErr(w, r, s.log, invalid("id: you cannot delete your own account"))
		return
	}
	if err := s.store.DeleteUser(r.Context(), u.ID); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	noContent(w)
}

type passwordRequest struct {
	Password string `json:"password"`
}

func (s *Server) setPassword(w http.ResponseWriter, r *http.Request) {
	u, found := s.manageUser(w, r)
	if !found {
		return
	}
	var req passwordRequest
	if !decode(w, r, &req) {
		return
	}
	if u.Kind != auth.KindHuman || !slices.Contains(u.EffectivePermissions, auth.AdminUsers) || !principal(r).Can(auth.AdminUsers) {
		failErr(w, r, s.log, invalid("password: only administrators (people with Manage users) can have a password; everyone else signs in with SSO"))
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		failErr(w, r, s.log, invalid(err.Error()))
		return
	}
	if err := s.store.SetPassword(r.Context(), u.ID, &hash); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	noContent(w)
}

func (s *Server) clearPassword(w http.ResponseWriter, r *http.Request) {
	u, found := s.manageUser(w, r)
	if !found {
		return
	}
	if err := s.store.SetPassword(r.Context(), u.ID, nil); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	noContent(w)
}

func (s *Server) revokeSessions(w http.ResponseWriter, r *http.Request) {
	u, found := s.manageUser(w, r)
	if !found {
		return
	}
	n, err := s.store.DeleteUserSessions(r.Context(), u.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	p := principal(r)
	s.audit(r, "sessions_revoked", p.UserID, p.Name, map[string]any{"user_id": u.ID, "ended": n})
	ok(w, map[string]int64{"ended": n})
}

// serviceAccount loads a service account for key and secret management.
func (s *Server) serviceAccount(w http.ResponseWriter, r *http.Request) (model.User, bool) {
	u, found := s.manageUser(w, r)
	if !found {
		return u, false
	}
	if u.Kind != auth.KindService {
		fail(w, http.StatusNotFound, "not_found", "API keys and signing secrets belong to service accounts.")
		return u, false
	}
	return u, true
}

type signingSecret struct {
	SigningSecret string `json:"signing_secret"`
}

func (s *Server) getSigningSecret(w http.ResponseWriter, r *http.Request) {
	u, found := s.serviceAccount(w, r)
	if !found {
		return
	}
	secret, err := s.store.SigningSecret(r.Context(), u.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, signingSecret{SigningSecret: secret})
}

func (s *Server) rotateSigningSecret(w http.ResponseWriter, r *http.Request) {
	u, found := s.serviceAccount(w, r)
	if !found {
		return
	}
	secret, err := s.store.RotateSigningSecret(r.Context(), u.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, signingSecret{SigningSecret: secret})
}

// Access settings and audit log

func (s *Server) getAccess(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetAccessSettings(r.Context())
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, a)
}

func (s *Server) putAccess(w http.ResponseWriter, r *http.Request) {
	var in model.AccessSettings
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeAccessSettings(in)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	if clean.AutoProvisionRoleID != nil {
		role, err := s.store.GetRole(r.Context(), *clean.AutoProvisionRoleID)
		if err != nil {
			failErr(w, r, s.log, invalid("auto_provision_role_id: no such role"))
			return
		}
		if problems := noEscalation(principal(r), "auto_provision_role_id", role.Permissions, nil); len(problems) > 0 {
			failErr(w, r, s.log, invalid(problems...))
			return
		}
	}
	out, err := s.store.UpdateAccessSettings(r.Context(), clean)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, out)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	events, total, err := s.store.ListAudit(r.Context(), limit, offset)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	page(w, events, model.Page{Total: total, Limit: limit, Offset: offset})
}
