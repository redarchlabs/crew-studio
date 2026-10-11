package watch

import (
	"encoding/json"
	"time"
)

// MetaKey holds, inside a stored snapshot item, what a tracker source remembers about the item besides its tracked
// fields. It is never compared, and it is split off before the fields are.
const MetaKey = "$studio"

// ItemMeta is what tracker sources know about an item besides its tracked fields.
type ItemMeta struct {
	// CommentsSeenAt is the newest comment time already reported (or the first check's time); stored
	CommentsSeenAt *time.Time `json:"comments_seen_at,omitempty"`
	// TrackedBy are the tracking-list items the item belongs to (tracking_list sources); stored, so a child that
	// disappears still counts as a change under its tracked item
	TrackedBy []string `json:"tracked_by,omitempty"`
	// Owner and Lead are who follow-up steps notify as "owner" and "lead"; not stored
	Owner *NotifyPerson `json:"-"`
	Lead  *NotifyPerson `json:"-"`
}

// NotifyPerson is someone a follow-up step should mention: their tracker account and, when the connection's people
// map links them, their Studio user.
type NotifyPerson struct {
	AccountID  string `json:"account_id"`
	Name       string `json:"name"`
	StudioUser string `json:"studio_user,omitempty"`
}

// stored is the part of the meta that is kept in the snapshot, or nil when there is nothing to keep.
func (m *ItemMeta) stored() *ItemMeta {
	if m == nil || (m.CommentsSeenAt == nil && len(m.TrackedBy) == 0) {
		return nil
	}
	return &ItemMeta{CommentsSeenAt: m.CommentsSeenAt, TrackedBy: m.TrackedBy}
}

// withMeta adds the stored meta to a copy of fields (fields itself is not changed).
func withMeta(fields map[string]any, meta *ItemMeta) map[string]any {
	keep := meta.stored()
	if keep == nil {
		return fields
	}
	out := make(map[string]any, len(fields)+1)
	for k, v := range fields {
		out[k] = v
	}
	out[MetaKey] = keep
	return out
}

// splitMeta takes the stored meta out of a snapshot item, returning the fields without it.
func splitMeta(fields map[string]any) (map[string]any, *ItemMeta) {
	raw, ok := fields[MetaKey]
	if !ok {
		return fields, nil
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if k != MetaKey {
			out[k] = v
		}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return out, nil
	}
	var meta ItemMeta
	if json.Unmarshal(data, &meta) != nil {
		return out, nil
	}
	return out, &meta
}

// seenMoved reports whether any item's stored comment time differs from the previous snapshot's, so a check that
// starts no run still saves it (otherwise every check would read the same comments again).
func seenMoved(items []Item, prev map[string]ItemMeta) bool {
	for _, it := range items {
		if it.Meta == nil || it.Meta.CommentsSeenAt == nil {
			continue
		}
		old := prev[it.ID].CommentsSeenAt
		if old == nil || !old.Equal(*it.Meta.CommentsSeenAt) {
			return true
		}
	}
	return false
}
