package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

// A crew's tracking list on its State tab (design section 12). People with state.manage add items (after looking
// them up in the tracker and confirming) and remove them; runs can never change the list.

const (
	// Lookups and people syncs call trackers on someone's behalf, so they are throttled per person
	trackerLookupsPerMinute = 20
	trackerLookupBurst      = 10
)

var trackerLookups = newKeyedLimiter(rate.Every(time.Minute/trackerLookupsPerMinute), trackerLookupBurst)

// trackingRoutes hangs off /crews/{id}/state (see crewStateRoutes).
func (s *Server) trackingRoutes(r chi.Router) {
	view, manage := s.need(auth.StateView), s.need(auth.StateManage)
	r.With(view).Get("/tracking", s.listTracking)
	r.With(manage).Post("/tracking/check", s.checkTracking)
	r.With(manage).Post("/tracking", s.addTracking)
	r.With(manage).Delete("/tracking/{itemId}", s.removeTracking)
}

type trackingView struct {
	Items []model.TrackingItem `json:"items"`
	// Connections are the tracker connections the crew may use that the caller can see (for adding items)
	Connections []tracker.ConnRef `json:"connections"`
}

func (s *Server) listTracking(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	p := principal(r)
	items, err := s.store.ListTrackingItems(r.Context(), store.TrackingFilter{CrewID: crew.ID, IncludeRemoved: r.URL.Query().Get("include_removed") == "true"})
	var conns []model.Connection
	if err == nil {
		conns, err = s.store.CrewConnections(r.Context(), crew.ID, scopeOf(p))
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	view := trackingView{Items: items, Connections: []tracker.ConnRef{}}
	for _, c := range conns {
		if c.Enabled && model.IsTrackerKind(c.Kind) && sharesArea(c.AreaIDs, crew.AreaIDs) {
			view.Connections = append(view.Connections, tracker.ConnRef{ID: c.ID, Name: c.Name, Kind: c.Kind})
		}
	}
	ok(w, view)
}

// cleanTrackingItem validates an item for the crew: its schedule must be the crew's and its connection one the
// caller can see. The tracker check (the crew may use the connection, the item exists) comes after.
func (s *Server) cleanTrackingItem(ctx context.Context, p *auth.Principal, crew model.Crew, in model.TrackingItemInput) (model.TrackingItemInput, error) {
	clean, err := model.NormalizeTrackingItem(in)
	if err != nil {
		return clean, err
	}
	if clean.ScheduleID != nil {
		owned, err := s.store.ScheduleOfCrew(ctx, *clean.ScheduleID, crew.ID)
		if err != nil {
			return clean, err
		}
		if !owned {
			return clean, invalid("schedule_id: is not one of this crew's schedules")
		}
	}
	conn, err := s.store.GetConnection(ctx, clean.ConnectionID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !p.SeesAreas(conn.AreaIDs)) {
		return clean, invalid("connection_id: no such connection")
	}
	return clean, err
}

// trackerFailure answers for a tracker problem: 422 for something the person can fix, 502 when the tracker failed.
func (s *Server) trackerFailure(w http.ResponseWriter, r *http.Request, err error) {
	var te *tracker.Error
	var limit *tracker.LimitError
	switch {
	case errors.As(err, &limit):
		fail(w, http.StatusUnprocessableEntity, "too_many", "The tracker returned too many issues; narrow the request.", limit.Error())
	case errors.As(err, &te) && (te.NotFound || (te.Status == 0 && !te.Unreachable)):
		fail(w, http.StatusUnprocessableEntity, "tracker_check_failed", te.Message)
	case errors.As(err, &te):
		fail(w, http.StatusBadGateway, "tracker_failed", "The tracker could not be read.", te.Message)
	case errors.Is(err, context.DeadlineExceeded):
		fail(w, http.StatusBadGateway, "tracker_failed", "The tracker did not answer in time.")
	default:
		failErr(w, r, s.log, err)
	}
}

func (s *Server) lookupTracking(w http.ResponseWriter, r *http.Request) (model.Crew, model.TrackingItemInput, tracker.ItemCheck, bool) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return crew, model.TrackingItemInput{}, tracker.ItemCheck{}, false
	}
	p := principal(r)
	if !trackerLookups.allow(p.UserID) {
		tooMany(w, "Tracker lookups are being made too quickly; wait a moment.")
		return crew, model.TrackingItemInput{}, tracker.ItemCheck{}, false
	}
	var in model.TrackingItemInput
	if !decode(w, r, &in) {
		return crew, in, tracker.ItemCheck{}, false
	}
	clean, err := s.cleanTrackingItem(r.Context(), p, crew, in)
	if err != nil {
		failErr(w, r, s.log, err)
		return crew, clean, tracker.ItemCheck{}, false
	}
	check, err := s.trackers.CheckItem(r.Context(), crew.ID, clean.ConnectionID, clean.ItemRef)
	if err != nil {
		s.trackerFailure(w, r, err)
		return crew, clean, check, false
	}
	if clean.Project == "" {
		clean.Project = check.Issue.Project
	}
	clean.ItemRef = check.Issue.Key
	return crew, clean, check, true
}

type trackingCheckResponse struct {
	tracker.ItemCheck
	AlreadyTracked bool `json:"already_tracked"`
}

// checkTracking looks an item up in the tracker so the person can confirm what they are about to add. It saves
// nothing.
func (s *Server) checkTracking(w http.ResponseWriter, r *http.Request) {
	crew, clean, check, found := s.lookupTracking(w, r)
	if !found {
		return
	}
	items, err := s.store.ListTrackingItems(r.Context(), store.TrackingFilter{CrewID: crew.ID})
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	tracked := slices.ContainsFunc(items, func(t model.TrackingItem) bool {
		return t.ConnectionID == clean.ConnectionID && t.ItemRef == clean.ItemRef && equalSchedule(t.ScheduleID, clean.ScheduleID)
	})
	ok(w, trackingCheckResponse{ItemCheck: check, AlreadyTracked: tracked})
}

func equalSchedule(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *Server) addTracking(w http.ResponseWriter, r *http.Request) {
	crew, clean, _, found := s.lookupTracking(w, r)
	if !found {
		return
	}
	p := principal(r)
	item, err := s.store.AddTrackingItem(r.Context(), crew.ID, clean, &p.UserID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditTracking(r, "tracking_item_added", crew, item)
	created(w, item)
}

func (s *Server) removeTracking(w http.ResponseWriter, r *http.Request) {
	crew, found := s.loadStateCrew(w, r)
	if !found {
		return
	}
	itemID, valid := pathUUID(w, r, "itemId")
	if !valid {
		return
	}
	p := principal(r)
	item, err := s.store.RemoveTrackingItem(r.Context(), crew.ID, itemID, &p.UserID, time.Now())
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditTracking(r, "tracking_item_removed", crew, item)
	ok(w, item)
}

func (s *Server) auditTracking(r *http.Request, action string, crew model.Crew, t model.TrackingItem) {
	p := principal(r)
	s.audit(r, action, p.UserID, p.Name, map[string]any{"crew_id": crew.ID, "crew_name": crew.Name, "tracking_item_id": t.ID,
		"connection_id": t.ConnectionID, "item_ref": t.ItemRef, "project": t.Project, "include_children": t.IncludeChildren,
		"schedule_id": t.ScheduleID})
}

// People map of a tracker connection

func (s *Server) trackerPeopleRoutes(r chi.Router) {
	r.With(s.need(auth.ConnectionsView)).Get("/{id}/people", s.listTrackerPeople)
	r.With(s.need(auth.ConnectionsManage)).Post("/{id}/people/sync", s.syncTrackerPeople)
	r.With(s.need(auth.ConnectionsManage)).Put("/{id}/people/{accountId}", s.linkTrackerPerson)
}

type peopleView struct {
	People []model.TrackerPerson `json:"people"`
	// Users are the Studio users accounts can be linked to: active people in the connection's areas
	Users []model.LinkableUser `json:"users"`
}

func (s *Server) loadTrackerConnection(w http.ResponseWriter, r *http.Request, owned bool) (model.Connection, bool) {
	load := s.loadConnection
	if owned {
		load = s.loadOwnedConnection
	}
	c, found := load(w, r)
	if found && !model.IsTrackerKind(c.Kind) {
		fail(w, http.StatusNotFound, "not_found", "Only Jira and Linear connections have a people map.")
		return c, false
	}
	return c, found
}

func (s *Server) peopleView(ctx context.Context, c model.Connection) (peopleView, error) {
	people, err := s.store.ListTrackerPeople(ctx, c.ID)
	if err != nil {
		return peopleView{}, err
	}
	users, err := s.store.LinkableUsers(ctx, c.AreaIDs)
	return peopleView{People: people, Users: users}, err
}

func (s *Server) listTrackerPeople(w http.ResponseWriter, r *http.Request) {
	c, found := s.loadTrackerConnection(w, r, false)
	if !found {
		return
	}
	view, err := s.peopleView(r.Context(), c)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, view)
}

type peopleSyncResponse struct {
	peopleView
	Read   int `json:"read"`
	Linked int `json:"linked"`
}

// syncTrackerPeople reads the tracker's people and links them to Studio users by email.
func (s *Server) syncTrackerPeople(w http.ResponseWriter, r *http.Request) {
	c, found := s.loadTrackerConnection(w, r, true)
	if !found {
		return
	}
	p := principal(r)
	if !trackerLookups.allow(p.UserID) {
		tooMany(w, "Tracker lookups are being made too quickly; wait a moment.")
		return
	}
	read, linked, err := s.trackers.SyncPeople(r.Context(), c)
	if err != nil {
		s.trackerFailure(w, r, err)
		return
	}
	view, err := s.peopleView(r.Context(), c)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditConnection(r, "tracker_people_synced", c, map[string]any{"read": read, "linked": linked})
	ok(w, peopleSyncResponse{peopleView: view, Read: read, Linked: linked})
}

type linkPersonRequest struct {
	// UserID links the account to a Studio user; null clears the link (and stops automatic linking)
	UserID *string `json:"user_id"`
}

func (s *Server) linkTrackerPerson(w http.ResponseWriter, r *http.Request) {
	c, found := s.loadTrackerConnection(w, r, true)
	if !found {
		return
	}
	accountID := chi.URLParam(r, "accountId")
	if !model.ValidAccountID(accountID) {
		fail(w, http.StatusNotFound, "not_found", "No such tracker account.")
		return
	}
	var req linkPersonRequest
	if !decode(w, r, &req) {
		return
	}
	var err error
	if req.UserID != nil {
		var users []model.LinkableUser
		users, err = s.store.LinkableUsers(r.Context(), c.AreaIDs)
		if err == nil && !slices.ContainsFunc(users, func(u model.LinkableUser) bool { return u.ID == *req.UserID }) {
			err = invalid("user_id: pick an active person in the connection's functional areas")
		}
	}
	var person model.TrackerPerson
	if err == nil {
		person, err = s.store.LinkTrackerPerson(r.Context(), c.ID, accountID, req.UserID)
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.auditConnection(r, "tracker_person_linked", c, map[string]any{"account_id": accountID, "user_id": req.UserID})
	ok(w, person)
}
