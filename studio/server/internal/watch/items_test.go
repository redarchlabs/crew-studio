package watch

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func mustDecode(t *testing.T, s string) any {
	t.Helper()
	doc, err := decodeJSON([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestItemsFromJSON(t *testing.T) {
	doc := mustDecode(t, `{"data": {"items": [
		{"key": "PRJ-1", "fields": {"status": "Open", "due": "2026-10-10", "points": 3.0}, "tags": ["a", "b"]},
		{"key": 42, "fields": {"status": "Done"}, "tags": []}
	]}}`)
	items, err := itemsFromJSON(doc, "data.items", "key", []string{"fields.status", "fields.due", "fields.points", "tags", "tags.0"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "PRJ-1" || items[1].ID != "42" {
		t.Fatalf("items = %+v", items)
	}
	f := items[0].Fields
	if f["fields.status"] != "Open" || f["fields.points"] != json.Number("3") || f["tags.0"] != "a" || items[1].Fields["fields.due"] != nil {
		t.Errorf("fields = %+v / %+v", f, items[1].Fields)
	}

	tests := []struct {
		name, doc, items, id string
		want                 string
	}{
		{"missing path", `{"a": []}`, "b", "id", "nothing was found"},
		{"not a list", `{"a": {"x": 1}}`, "a", "id", "is not a list (it is an object)"},
		{"root not a list", `{"a": 1}`, "", "id", "the document is not a list"},
		{"no id", `[{"x": 1}]`, "", "id", "there is no id"},
		{"bad id", `[{"id": {"x": 1}}]`, "", "id", "must be text or a number"},
		{"empty id", `[{"id": "  "}]`, "", "id", "the id is empty"},
		{"control id", `[{"id": "a\u0001"}]`, "", "id", "control characters"},
		{"long id", `[{"id": "` + strings.Repeat("x", 201) + `"}]`, "", "id", "longer than"},
		{"duplicate", `[{"id": 1}, {"id": "1"}]`, "", "id", "items 1 and 2 have the same id"},
		{"index path", `[[{"id": 1}]]`, "0.x", "id", "nothing was found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := itemsFromJSON(mustDecode(t, tt.doc), tt.items, tt.id, []string{"x"}, 10)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
	_, err = itemsFromJSON(mustDecode(t, `[{"id":1},{"id":2},{"id":3}]`), "", "id", []string{"x"}, 2)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Count != 3 || !strings.Contains(err.Error(), "limit of 2") {
		t.Errorf("limit err = %v", err)
	}
	if _, err := decodeJSON([]byte("{nope")); err == nil {
		t.Error("bad JSON accepted")
	}
}

func TestNormalizeValueHashesLongText(t *testing.T) {
	long := strings.Repeat("word ", 100)
	v := normalizeValue(long)
	m, ok := v.(map[string]any)
	if !ok || !isLongText(m) || m["chars"] != json.Number("500") {
		t.Fatalf("long text = %#v", v)
	}
	if normalizeValue(long) == nil || !sameValue(normalizeValue(long), normalizeValue(long)) || sameValue(normalizeValue(long), normalizeValue(long+"x")) {
		t.Error("hash comparison is wrong")
	}
	if got := display(v); !strings.Contains(got.(string), "long text, 500 characters") {
		t.Errorf("display = %v", got)
	}
	nested := map[string]any{"list": []any{strings.Repeat("a", 400)}}
	if m, ok := normalizeValue(nested).(map[string]any); !ok || !isLongText(m) {
		t.Errorf("long nested value not hashed: %#v", normalizeValue(nested))
	}
	if again := normalizeValue(v); !sameValue(again, v) {
		t.Error("a stored hash is not stable")
	}
	small := map[string]any{"n": json.Number("1.50"), "f": 2.0}
	if got, _ := json.Marshal(normalizeValue(small)); string(got) != `{"f":2,"n":1.5}` {
		t.Errorf("small nested = %s", got)
	}
	for in, want := range map[json.Number]json.Number{"1e2": "100", "007": "7", "1.25": "1.25", "1e30": "1e+30", "x": "x"} {
		if got := canonicalNumber(in); got != want {
			t.Errorf("canonicalNumber(%s) = %s", in, got)
		}
	}
	if normalizeValue(1.5) != json.Number("1.5") || normalizeValue(true) != true || normalizeValue(nil) != nil {
		t.Error("scalars")
	}
	for v, want := range map[any]string{"x": "x", json.Number("3"): "3", true: "true", nil: ""} {
		if got := scalarText(v); got != want {
			t.Errorf("scalarText(%v) = %q", v, got)
		}
	}
	if kindOf(true) != "true or false" || kindOf(nil) != "null" || kindOf("s") != "text" || kindOf(json.Number("1")) != "a number" || kindOf(1.5) != "something else" {
		t.Error("kindOf")
	}
}

func TestCompare(t *testing.T) {
	prev := Snapshot{
		"A": {"status": "Open", "due": "2026-10-10"},
		"B": {"status": "Open", "due": "2026-10-11"},
		"C": {"status": "Open", "due": nil},
	}
	items := []Item{
		{ID: "B", Fields: map[string]any{"status": "Done", "due": "2026-10-11"}},
		{ID: "A", Fields: map[string]any{"status": "Open", "due": "2026-10-10"}},
		{ID: "D", Fields: map[string]any{"status": "New", "due": normalizeValue(strings.Repeat("x", 400))}},
	}
	at := time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC)
	before := at.Add(-24 * time.Hour)
	ch := Compare(prev, items, "src", at, &before)
	if len(ch.Added) != 1 || ch.Added[0].ID != "D" || len(ch.Changed) != 1 || ch.Changed[0].ID != "B" ||
		len(ch.Removed) != 1 || ch.Removed[0].ID != "C" || ch.Removed[0].Reason != RemovedReason || ch.UnchangedCount != 1 {
		t.Fatalf("changes = %+v", ch)
	}
	if fc := ch.Changed[0].Fields["status"]; fc.Old != "Open" || fc.New != "Done" || len(ch.Changed[0].Fields) != 1 {
		t.Errorf("field change = %+v", ch.Changed[0].Fields)
	}
	if !strings.Contains(ch.Added[0].Fields["due"].(string), "long text") || ch.Count() != 3 {
		t.Errorf("added = %+v", ch.Added)
	}
	first := Compare(Snapshot{}, items, "src", at, nil)
	if len(first.Added) != 3 || first.PreviousCheckAt != nil || first.Added[0].ID != "A" {
		t.Errorf("first check = %+v", first)
	}
	data, _ := json.Marshal(first)
	if !strings.Contains(string(data), `"previous_check_at":null`) || strings.Contains(string(data), "new_comments") {
		t.Errorf("json = %s", data)
	}
	if snap := SnapshotOf(items); len(snap) != 3 || snap["B"]["status"] != "Done" {
		t.Errorf("snapshot = %+v", snap)
	}
	if slim := withoutAddedFields(first); !slim.AddedFieldsOmitted || slim.Added[0].Fields != nil || first.Added[0].Fields == nil {
		t.Errorf("slim = %+v", slim)
	}
}
