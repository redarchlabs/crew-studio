package watch

import (
	"slices"
	"strings"
	"time"
)

// Snapshot is what the last check saw: item id to tracked fields.
type Snapshot map[string]map[string]any

// RemovedReason is given for items that are no longer returned.
const RemovedReason = "no longer matches the query"

// Changes is the change list a run receives as its "changes" input (design section 5).
type Changes struct {
	Source    string    `json:"source"`
	CheckedAt time.Time `json:"checked_at"`
	// PreviousCheckAt is null on the first check, when everything counts as added
	PreviousCheckAt *time.Time    `json:"previous_check_at"`
	Added           []AddedItem   `json:"added"`
	Changed         []ChangedItem `json:"changed"`
	Removed         []RemovedItem `json:"removed"`
	// NewComments come from tracker sources that track comments; other sources never set them
	NewComments []Comment `json:"new_comments,omitempty"`
	// NewCommentsOmitted counts new comments past the per-tick cap (read them with tracker_get_issue)
	NewCommentsOmitted int `json:"new_comments_omitted,omitempty"`
	UnchangedCount     int `json:"unchanged_count"`
	// AddedFieldsOmitted is set when the added items were too many to include with their fields
	AddedFieldsOmitted bool `json:"added_fields_omitted,omitempty"`
}

type AddedItem struct {
	ID     string         `json:"id"`
	Fields map[string]any `json:"fields,omitempty"`
}

type FieldChange struct {
	Old any `json:"old"`
	New any `json:"new"`
}

type ChangedItem struct {
	ID     string                 `json:"id"`
	Fields map[string]FieldChange `json:"fields"`
}

type RemovedItem struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// Comment is a new comment on a tracked item (tracker sources). ID is the item's id, such as jira:PRJ-15.
type Comment struct {
	ID      string    `json:"id"`
	Author  string    `json:"author"`
	At      time.Time `json:"at"`
	Excerpt string    `json:"excerpt"`
	// Truncated is set when the excerpt was cut at ExcerptChars
	Truncated bool `json:"truncated,omitempty"`
}

// Count is how many items were added, changed or removed (plus new comments).
func (c Changes) Count() int {
	return len(c.Added) + len(c.Changed) + len(c.Removed) + len(c.NewComments)
}

// Compare works out what changed between the previous snapshot and the items just fetched. Results are sorted
// by id so the same data always gives the same list.
func Compare(prev Snapshot, items []Item, label string, checkedAt time.Time, previousAt *time.Time) Changes {
	out := Changes{Source: label, CheckedAt: checkedAt, PreviousCheckAt: previousAt,
		Added: []AddedItem{}, Changed: []ChangedItem{}, Removed: []RemovedItem{}}
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		seen[it.ID] = true
		old, existed := prev[it.ID]
		if !existed {
			out.Added = append(out.Added, AddedItem{ID: it.ID, Fields: displayFields(it.Fields)})
			continue
		}
		if diff := fieldChanges(old, it.Fields); len(diff) > 0 {
			out.Changed = append(out.Changed, ChangedItem{ID: it.ID, Fields: diff})
		} else {
			out.UnchangedCount++
		}
	}
	for id := range prev {
		if !seen[id] {
			out.Removed = append(out.Removed, RemovedItem{ID: id, Reason: RemovedReason})
		}
	}
	slices.SortFunc(out.Added, func(a, b AddedItem) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(out.Changed, func(a, b ChangedItem) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(out.Removed, func(a, b RemovedItem) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func fieldChanges(old, current map[string]any) map[string]FieldChange {
	diff := map[string]FieldChange{}
	for field, value := range current {
		before := old[field]
		if !sameValue(before, value) {
			diff[field] = FieldChange{Old: display(before), New: display(value)}
		}
	}
	return diff
}

func displayFields(fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		out[k] = display(v)
	}
	return out
}

// SnapshotOf is the snapshot to store after a check: each item's tracked fields, plus what tracker sources must
// remember about it (such as the newest comment seen) under MetaKey.
func SnapshotOf(items []Item) map[string]map[string]any {
	out := make(map[string]map[string]any, len(items))
	for _, it := range items {
		out[it.ID] = withMeta(it.Fields, it.Meta)
	}
	return out
}

// withoutAddedFields drops the fields of added items, keeping their ids, when the list is too long to pass on.
func withoutAddedFields(c Changes) Changes {
	out := c
	out.Added = make([]AddedItem, len(c.Added))
	for i, a := range c.Added {
		out.Added[i] = AddedItem{ID: a.ID}
	}
	out.AddedFieldsOmitted = true
	return out
}
