package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// Tracking-list proposals (design sections 8 and 12): the front desk proposes to track or stop tracking an item and
// the person confirms. Only then is the connection checked (the crew may use it and the person can see it) and the
// item looked up in the tracker. Adding undoes as a removal; removing undoes by putting the item back.

const maxConnectionRef = 200

type trackingAdd struct {
	Connection      string `json:"connection"`
	ItemRef         string `json:"item_ref"`
	Project         string `json:"project"`
	IncludeChildren string `json:"include_children"`
	Note            string `json:"note"`
	LeadAccountID   string `json:"lead_account_id"`
}

type trackingRemove struct {
	Connection string `json:"connection"`
	ItemRef    string `json:"item_ref"`
}

var childrenText = map[string]string{
	model.IncludeChildrenAll: "including all its child tickets", model.IncludeChildrenDirect: "with its direct children",
	model.IncludeChildrenNone: "without its children",
}

func trackingAddKind() Kind {
	return Kind{Type: TypeTrackingAdd, Normalize: normalizeTrackingAdd, Apply: applyTrackingAdd, Undo: undoTrackingAdd,
		Needs: []string{auth.StateView, auth.StateManage}}
}

func trackingRemoveKind() Kind {
	return Kind{Type: TypeTrackingRemove, Normalize: normalizeTrackingRemove, Apply: applyTrackingRemove, Undo: undoTrackingRemove,
		Needs: []string{auth.StateView, auth.StateManage}}
}

// cleanConnectionRef checks the connection a proposal names: an id or a name, one short line.
func cleanConnectionRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if len(ref) > maxConnectionRef || strings.IndexFunc(ref, unicode.IsControl) >= 0 {
		return "", &model.ValidationError{Details: []string{"connection: give the tracker connection's name"}}
	}
	return ref, nil
}

func normalizeTrackingAdd(payload map[string]any) (map[string]any, string, error) {
	in, err := decodePayload[trackingAdd](payload)
	if err != nil {
		return nil, "", err
	}
	conn, err := cleanConnectionRef(in.Connection)
	if err != nil {
		return nil, "", err
	}
	// The connection id is only known when applying; a placeholder lets the rest be checked now
	clean, err := model.NormalizeTrackingItem(model.TrackingItemInput{ConnectionID: "00000000-0000-4000-8000-000000000000", ItemRef: in.ItemRef,
		Project: in.Project, IncludeChildren: in.IncludeChildren, Note: in.Note, LeadAccountID: in.LeadAccountID})
	if err != nil {
		return nil, "", err
	}
	out := trackingAdd{Connection: conn, ItemRef: clean.ItemRef, Project: clean.Project, IncludeChildren: clean.IncludeChildren,
		Note: clean.Note, LeadAccountID: clean.LeadAccountID}
	summary := "Track " + out.ItemRef
	if out.Project != "" {
		summary += " (" + out.Project + ")"
	}
	summary += ", " + childrenText[out.IncludeChildren]
	if conn != "" {
		summary += ", in " + conn
	}
	m, err := asMap(out)
	return m, summary, err
}

func normalizeTrackingRemove(payload map[string]any) (map[string]any, string, error) {
	in, err := decodePayload[trackingRemove](payload)
	if err != nil {
		return nil, "", err
	}
	conn, err := cleanConnectionRef(in.Connection)
	if err != nil {
		return nil, "", err
	}
	ref, ok := model.NormalizeItemRef(in.ItemRef)
	if !ok {
		return nil, "", &model.ValidationError{Details: []string{"item_ref: use the tracked item's key, such as PRJ-10"}}
	}
	m, err := asMap(trackingRemove{Connection: conn, ItemRef: ref})
	return m, "Stop tracking " + ref, err
}

// connectionFor resolves a proposal's connection among the crew's tracker connections the person can see.
func connectionFor(ctx context.Context, env Env, req Request, wanted string) (model.Connection, error) {
	conns, err := trackerConnections(ctx, env.Store, req.Principal, req.Crew)
	if err != nil {
		return model.Connection{}, err
	}
	return pickConnection(conns, wanted)
}

func applyTrackingAdd(ctx context.Context, env Env, req Request) (map[string]any, error) {
	in, err := decodePayload[trackingAdd](req.Payload)
	if err != nil {
		return nil, err
	}
	if env.Trackers == nil {
		return nil, &DeniedError{Message: "Trackers are not set up on this server."}
	}
	conn, err := connectionFor(ctx, env, req, in.Connection)
	if err != nil {
		return nil, err
	}
	check, err := env.Trackers.CheckItem(ctx, req.Crew.ID, conn.ID, in.ItemRef)
	if err != nil {
		return nil, err
	}
	project := in.Project
	if project == "" {
		project = check.Issue.Project
	}
	item, err := env.Store.AddTrackingItem(ctx, req.Crew.ID, model.TrackingItemInput{ConnectionID: conn.ID, ItemRef: check.Issue.Key,
		Project: project, IncludeChildren: in.IncludeChildren, Note: in.Note, LeadAccountID: in.LeadAccountID}, &req.Principal.UserID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"tracking_item_id": item.ID, "item_ref": item.ItemRef, "connection": conn.Name, "undoable": true}, nil
}

func undoTrackingAdd(ctx context.Context, env Env, req Request) error {
	id, _ := req.Result["tracking_item_id"].(string)
	_, err := env.Store.RemoveTrackingItem(ctx, req.Crew.ID, id, &req.Principal.UserID, time.Now())
	if errors.Is(err, store.ErrNotFound) {
		return &store.ConflictError{Code: "not_undoable", Message: "The item is no longer on the tracking list."}
	}
	return err
}

func applyTrackingRemove(ctx context.Context, env Env, req Request) (map[string]any, error) {
	in, err := decodePayload[trackingRemove](req.Payload)
	if err != nil {
		return nil, err
	}
	items, err := env.Store.ListTrackingItems(ctx, store.TrackingFilter{CrewID: req.Crew.ID})
	if err != nil {
		return nil, err
	}
	var matches []model.TrackingItem
	for _, t := range items {
		if t.ItemRef != in.ItemRef || (in.Connection != "" && t.ConnectionID != in.Connection && !strings.EqualFold(t.ConnectionName, in.Connection)) {
			continue
		}
		// Only items tracked through a connection the person can see
		conn, err := env.Store.GetConnection(ctx, t.ConnectionID)
		if err != nil {
			return nil, err
		}
		if req.Principal.SeesAreas(conn.AreaIDs) {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 0:
		return nil, &store.ConflictError{Code: "not_tracked", Message: fmt.Sprintf("%s is not on the tracking list.", in.ItemRef)}
	case 1:
	default:
		return nil, &model.ValidationError{Details: []string{"connection: " + in.ItemRef + " is tracked more than once; say which connection"}}
	}
	item, err := env.Store.RemoveTrackingItem(ctx, req.Crew.ID, matches[0].ID, &req.Principal.UserID, time.Now())
	if err != nil {
		return nil, err
	}
	return map[string]any{"tracking_item_id": item.ID, "item_ref": item.ItemRef, "undoable": true}, nil
}

func undoTrackingRemove(ctx context.Context, env Env, req Request) error {
	id, _ := req.Result["tracking_item_id"].(string)
	_, err := env.Store.RestoreTrackingItem(ctx, req.Crew.ID, id)
	return err
}
