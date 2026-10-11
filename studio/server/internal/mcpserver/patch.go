package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"time"
)

// applyChanges returns a copy of current with each field in changes replaced. The API's PUT replaces the whole
// item, so the tools send the merged item and Claude only has to name what it wants changed. null clears a field.
func (k kind) applyChanges(current, changes map[string]any) (map[string]any, error) {
	if len(changes) == 0 {
		return nil, errors.New("changes is empty: name at least one field to change")
	}
	if err := k.checkFields(changes); err != nil {
		return nil, err
	}
	merged := maps.Clone(current)
	maps.Copy(merged, changes)
	return merged, nil
}

// FieldChange is one field that differs between two versions of an item.
type FieldChange struct {
	Field  string `json:"field"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

// diff lists the fields that differ, by name. updated_at is left out: it changes on every save.
func diff(before, after map[string]any) []FieldChange {
	names := map[string]bool{}
	for name := range before {
		names[name] = true
	}
	for name := range after {
		names[name] = true
	}
	delete(names, "updated_at")
	changes := []FieldChange{}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		if !sameJSON(before[name], after[name]) {
			changes = append(changes, FieldChange{Field: name, Before: before[name], After: after[name]})
		}
	}
	return changes
}

// sameJSON compares values by their JSON, so 5 and 5.0 are equal.
func sameJSON(a, b any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// sameInstant compares two timestamps as instants, falling back to text when either is not RFC 3339.
func sameInstant(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339Nano, a)
	tb, errB := time.Parse(time.RFC3339Nano, b)
	if errA != nil || errB != nil {
		return a == b
	}
	return ta.Equal(tb)
}

func sortedKeys(m map[string]any) []string {
	return slices.Sorted(maps.Keys(m))
}
