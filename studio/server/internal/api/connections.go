package api

import (
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/delivery"
	"crewstudio/internal/mcpclient"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

// Connections to outside systems. They belong to functional areas like crews do; their secrets are write-only.

func (s *Server) connectionRoutes(r chi.Router) {
	r.Route("/connections", func(r chi.Router) {
		manage := s.need(auth.ConnectionsManage)
		r.With(s.need(auth.ConnectionsView)).Get("/", s.listConnections)
		r.With(s.need(auth.ConnectionsView)).Get("/kinds", s.connectionKinds)
		r.With(manage).Post("/", s.createConnection)
		r.With(s.need(auth.ConnectionsView)).Get("/{id}", s.getConnection)
		r.With(manage).Put("/{id}", s.updateConnection)
		r.With(manage).Put("/{id}/secret", s.replaceConnectionSecret)
		r.With(manage).Post("/{id}/test", s.testConnection)
		// Folder pickers: the connection form, and report destinations on schedules and in the PMO wizard
		r.With(s.needAny(auth.ConnectionsView, auth.SchedulesManage)).Get("/{id}/km2/folders", s.km2ConnectionFolders)
		r.With(manage).Delete("/{id}", s.deleteConnection)
		s.trackerPeopleRoutes(r)
		s.connectionToolRoutes(r)
	})
}

type connectionKindsResponse struct {
	Kinds []model.ConnectionKind `json:"kinds"`
	// SecretsReady is false when the server has no STUDIO_SECRETS_KEY, so no connection can be added
	SecretsReady bool `json:"secrets_ready"`
}

func (s *Server) connectionKinds(w http.ResponseWriter, _ *http.Request) {
	ok(w, connectionKindsResponse{Kinds: model.ConnectionKinds, SecretsReady: s.conns.Ready()})
}

func (s *Server) listConnections(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListConnections(r.Context(), scopeOf(principal(r)))
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, items)
}

// loadConnection reads {id} and answers 404 itself when it is missing or outside the caller's areas.
func (s *Server) loadConnection(w http.ResponseWriter, r *http.Request) (model.Connection, bool) {
	id, valid := pathID(w, r)
	if !valid {
		return model.Connection{}, false
	}
	c, err := s.store.GetConnection(r.Context(), id)
	if err == nil && !principal(r).SeesAreas(c.AreaIDs) {
		err = store.ErrNotFound
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return c, false
	}
	return c, true
}

// loadOwnedConnection is loadConnection for changes: the caller must be in every area of the connection, so a
// manager in one area cannot retarget, re-key or delete a connection another area also relies on.
func (s *Server) loadOwnedConnection(w http.ResponseWriter, r *http.Request) (model.Connection, bool) {
	c, found := s.loadConnection(w, r)
	if !found {
		return c, false
	}
	p := principal(r)
	for _, area := range c.AreaIDs {
		if !p.HasArea(area) {
			fail(w, http.StatusForbidden, "forbidden", "This connection is shared with functional areas you are not in, so you cannot change it.")
			return c, false
		}
	}
	return c, true
}

func (s *Server) getConnection(w http.ResponseWriter, r *http.Request) {
	if c, found := s.loadConnection(w, r); found {
		ok(w, c)
	}
}

func (s *Server) createConnection(w http.ResponseWriter, r *http.Request) {
	var in model.ConnectionInput
	if !decode(w, r, &in) {
		return
	}
	clean, err := model.NormalizeConnection(in)
	if err == nil {
		err = s.checkTrackerSite(clean)
	}
	if err == nil {
		clean.AreaIDs, err = mergeAreas(principal(r), nil, clean.AreaIDs)
	}
	var out model.Connection
	if err == nil {
		out, err = s.conns.Create(r.Context(), clean)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditConnection(r, "connection_created", out, nil)
	created(w, out)
}

func (s *Server) updateConnection(w http.ResponseWriter, r *http.Request) {
	old, found := s.loadOwnedConnection(w, r)
	if !found {
		return
	}
	var in model.ConnectionInput
	if !decode(w, r, &in) {
		return
	}
	if in.Secrets != nil {
		failErr(w, r, s.log, invalid("secrets: replace secrets with PUT /api/connections/{id}/secret"))
		return
	}
	if in.Kind == "" {
		in.Kind = old.Kind
	}
	if in.Enabled == nil {
		in.Enabled = &old.Enabled
	}
	clean, err := model.NormalizeConnection(in)
	if err == nil && clean.Kind != old.Kind {
		err = invalid("kind: cannot be changed; create a new connection instead")
	}
	if err == nil {
		err = s.checkTrackerSite(clean)
	}
	if err == nil {
		clean.AreaIDs, err = mergeAreas(principal(r), old.AreaIDs, clean.AreaIDs)
	}
	// Secrets saved for one address must never be sent to another: a new URL, host, port or header clears them
	clearSecrets := err == nil && len(old.SecretKeys) > 0 && model.DestinationChanged(old.Kind, old.Config, clean.Config)
	// An MCP connection's tools (and the effects given them) describe the server at its old address: forget them before
	// the new address is saved, so no tool vetted on one server is ever called on another
	if err == nil && old.Kind == model.KindMCP && old.Config[model.ConfigMCPURL] != clean.Config[model.ConfigMCPURL] {
		err = s.store.DeleteConnectionTools(r.Context(), old.ID)
	}
	var out model.Connection
	if err == nil {
		out, err = s.store.UpdateConnection(r.Context(), old.ID, clean, clearSecrets)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.trackers.Forget(out.ID)
	s.auditConnection(r, "connection_updated", out, map[string]any{"secrets_cleared": clearSecrets})
	ok(w, out)
}

type secretRequest struct {
	Secrets map[string]string `json:"secrets"`
}

// replaceConnectionSecret swaps all of a connection's secret fields. The values are never returned.
func (s *Server) replaceConnectionSecret(w http.ResponseWriter, r *http.Request) {
	c, found := s.loadOwnedConnection(w, r)
	if !found {
		return
	}
	var req secretRequest
	if !decode(w, r, &req) {
		return
	}
	values, err := model.NormalizeSecrets(c.Kind, req.Secrets)
	var out model.Connection
	if err == nil {
		out, err = s.conns.ReplaceSecrets(r.Context(), c.ID, values)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.trackers.Forget(out.ID)
	s.auditConnection(r, "connection_secret_replaced", out, map[string]any{"secret_keys": out.SecretKeys})
	ok(w, out)
}

func (s *Server) testConnection(w http.ResponseWriter, r *http.Request) {
	c, found := s.loadOwnedConnection(w, r)
	if !found {
		return
	}
	if model.IsTrackerKind(c.Kind) {
		if !trackerLookups.allow(principal(r).UserID) {
			tooMany(w, "Tracker lookups are being made too quickly; wait a moment.")
			return
		}
		ok(w, s.trackers.Test(r.Context(), c))
		return
	}
	if c.Kind == model.KindKM2 {
		s.testKM2Connection(w, r, c)
		return
	}
	if c.Kind == model.KindMCP {
		s.testMCPConnection(w, r, c)
		return
	}
	if slices.Contains(delivery.Kinds, c.Kind) {
		s.testDeliveryConnection(w, r, c)
		return
	}
	ok(w, s.conns.Test(r.Context(), c))
}

func (s *Server) deleteConnection(w http.ResponseWriter, r *http.Request) {
	c, found := s.loadOwnedConnection(w, r)
	if !found {
		return
	}
	if err := s.store.DeleteConnection(r.Context(), c.ID); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditConnection(r, "connection_deleted", c, nil)
	noContent(w)
}

// auditConnection records a change with the connection's name and kind (never secret values).
func (s *Server) auditConnection(r *http.Request, action string, c model.Connection, extra map[string]any) {
	p := principal(r)
	details := map[string]any{"connection_id": c.ID, "connection_name": c.Name, "kind": c.Kind}
	for k, v := range extra {
		details[k] = v
	}
	s.audit(r, action, p.UserID, p.Name, details)
}

// Which connections a crew's tools may use

type crewConnectionsRequest struct {
	ConnectionIDs []string `json:"connection_ids"`
}

func (s *Server) listCrewConnections(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	items, err := s.store.CrewConnections(r.Context(), crew.ID, scopeOf(principal(r)))
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, items)
}

// setCrewConnections replaces the crew's connections among those the caller can see; links to connections
// outside the caller's areas stay as they are.
func (s *Server) setCrewConnections(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	var req crewConnectionsRequest
	if !decode(w, r, &req) {
		return
	}
	p := principal(r)
	visible, err := s.store.ListConnections(r.Context(), scopeOf(p))
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ids := make([]string, 0, len(req.ConnectionIDs))
	var problems []string
	for _, id := range req.ConnectionIDs {
		switch {
		case !slices.ContainsFunc(visible, func(c model.Connection) bool { return c.ID == id }):
			problems = append(problems, "connection_ids: "+id+" is not a connection you can see")
		case !slices.Contains(ids, id):
			ids = append(ids, id)
		}
	}
	if len(problems) > 0 {
		failErr(w, r, s.log, invalid(problems...))
		return
	}
	if err := s.store.SetCrewConnections(r.Context(), crew.ID, ids, scopeOf(p)); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.audit(r, "crew_connections_set", p.UserID, p.Name, map[string]any{"crew_id": crew.ID, "crew_name": crew.Name, "connection_ids": ids})
	s.listCrewConnections(w, r)
}

// checkTrackerSite keeps a Jira connection's API token for Atlassian sites (or the sites an administrator allowed),
// a KM2 tracker connection's key for an https KM2 API address, and an MCP connection's secret for an https MCP server
// (plain http only to this machine).
func (s *Server) checkTrackerSite(in model.ConnectionInput) error {
	if in.Kind == model.KindMCP {
		if err := mcpclient.CheckURL(in.Config[model.ConfigMCPURL]); err != nil {
			return invalid("config.url: " + err.Error())
		}
		return nil
	}
	if in.Kind == model.TrackerKM2 {
		if err := tracker.CheckKM2Base(in.Config["base_url"], s.trackers.Options().KM2TrackerSites); err != nil {
			return invalid("config.base_url: " + err.Error())
		}
		return nil
	}
	if in.Kind != model.TrackerJira {
		return nil
	}
	if err := tracker.CheckJiraSite(in.Config["site_url"], s.trackers.Options().JiraSites); err != nil {
		return invalid("config.site_url: " + err.Error())
	}
	return nil
}
