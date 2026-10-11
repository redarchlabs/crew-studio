// Package watch fetches a watch's source, compares it with the last snapshot and works out which follow-up
// steps are due. Code decides what changed and who is due a reminder; no AI is involved
// (docs/crew-state-and-pmo-design.md, sections 5 and 6).
package watch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// LongTextChars is the length past which text is compared by hash and never copied into changes
	LongTextChars = 300
	maxItemIDLen  = 200
	longTextKey   = "$long_text"
)

// Item is one thing a source returned: its id and its tracked fields, already normalized (long text hashed).
type Item struct {
	ID     string         `json:"id"`
	Fields map[string]any `json:"fields"`
	// Meta is what tracker sources know besides the tracked fields; it is never compared
	Meta *ItemMeta `json:"-"`
}

// LimitError means the source returned more items than the watch allows. Checks fail rather than cut the list.
type LimitError struct {
	Count int
	Max   int
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("the source returned %d items, more than this watch's limit of %d; narrow the query or raise the limit", e.Count, e.Max)
}

// SourceError is a problem reading a source that is safe to show to people (no secrets, no response bodies).
type SourceError struct{ Message string }

func (e *SourceError) Error() string { return e.Message }

func sourceErrorf(format string, args ...any) error {
	return &SourceError{Message: fmt.Sprintf(format, args...)}
}

// decodeJSON reads a document keeping numbers exact.
func decodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		// The decoder's message can quote the data, so it is not passed on
		return nil, sourceErrorf("the data is not valid JSON")
	}
	return doc, nil
}

// lookup follows a dotted path (object keys, or array indexes as numbers). "" is the value itself.
func lookup(v any, path string) (any, bool) {
	if path == "" {
		return v, true
	}
	for _, part := range strings.Split(path, ".") {
		switch node := v.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return nil, false
			}
			v = next
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			v = node[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// itemsFromJSON turns a JSON document into items: the array at itemsPath, each with an id and tracked fields.
func itemsFromJSON(doc any, itemsPath, idField string, tracked []string, maxItems int) ([]Item, error) {
	found, ok := lookup(doc, itemsPath)
	if !ok {
		return nil, sourceErrorf("nothing was found at the items path %q", itemsPath)
	}
	list, ok := found.([]any)
	if !ok {
		where := "the document"
		if itemsPath != "" {
			where = fmt.Sprintf("the items path %q", itemsPath)
		}
		return nil, sourceErrorf("%s is not a list (it is %s)", where, kindOf(found))
	}
	if len(list) > maxItems {
		return nil, &LimitError{Count: len(list), Max: maxItems}
	}
	items := make([]Item, 0, len(list))
	for i, raw := range list {
		idValue, _ := lookup(raw, idField)
		id, err := itemID(idValue)
		if err != nil {
			return nil, sourceErrorf("item %d: %v at the id field %q", i+1, err, idField)
		}
		fields := make(map[string]any, len(tracked))
		for _, f := range tracked {
			v, _ := lookup(raw, f)
			fields[f] = normalizeValue(v)
		}
		items = append(items, Item{ID: id, Fields: fields})
	}
	return items, checkUniqueIDs(items)
}

func kindOf(v any) string {
	switch v.(type) {
	case map[string]any:
		return "an object"
	case string:
		return "text"
	case json.Number:
		return "a number"
	case bool:
		return "true or false"
	case nil:
		return "null"
	}
	return "something else"
}

// itemID turns an id value (text or a number) into a subject-safe string.
func itemID(v any) (string, error) {
	var id string
	switch x := v.(type) {
	case string:
		id = strings.TrimSpace(x)
	case json.Number:
		id = x.String()
	case nil:
		return "", fmt.Errorf("there is no id")
	default:
		return "", fmt.Errorf("the id must be text or a number, not %s", kindOf(v))
	}
	return checkItemID(id)
}

func checkItemID(id string) (string, error) {
	switch {
	case id == "":
		return "", fmt.Errorf("the id is empty")
	case utf8.RuneCountInString(id) > maxItemIDLen:
		return "", fmt.Errorf("the id is longer than %d characters", maxItemIDLen)
	case strings.IndexFunc(id, unicode.IsControl) >= 0:
		return "", fmt.Errorf("the id contains control characters")
	}
	return id, nil
}

func checkUniqueIDs(items []Item) error {
	seen := make(map[string]int, len(items))
	for i, it := range items {
		if first, dup := seen[it.ID]; dup {
			return sourceErrorf("items %d and %d have the same id; the id field must be unique", first+1, i+1)
		}
		seen[it.ID] = i
	}
	return nil
}

// normalizeValue makes a value comparable and safe to keep: numbers in one canonical form, and text (or nested
// data) longer than LongTextChars replaced by its hash and length.
func normalizeValue(v any) any {
	switch x := v.(type) {
	case nil, bool:
		return x
	case json.Number:
		return canonicalNumber(x)
	case float64:
		return canonicalNumber(json.Number(strconv.FormatFloat(x, 'g', -1, 64)))
	case string:
		if utf8.RuneCountInString(x) > LongTextChars {
			return longText(x)
		}
		return x
	case map[string]any:
		if isLongText(x) {
			return x
		}
	}
	data, err := json.Marshal(normalizeNested(v))
	if err != nil {
		return nil
	}
	if utf8.RuneCount(data) > LongTextChars {
		return longText(string(data))
	}
	return normalizeNested(v)
}

func normalizeNested(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalizeNested(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalizeNested(val)
		}
		return out
	case json.Number:
		return canonicalNumber(x)
	case float64:
		return canonicalNumber(json.Number(strconv.FormatFloat(x, 'g', -1, 64)))
	}
	return v
}

// canonicalNumber writes a number the same way whether it came from the source or back from the database.
func canonicalNumber(n json.Number) json.Number {
	if i, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
		return json.Number(strconv.FormatInt(i, 10))
	}
	f, err := strconv.ParseFloat(n.String(), 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return n
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return json.Number(strconv.FormatInt(int64(f), 10))
	}
	return json.Number(strconv.FormatFloat(f, 'g', -1, 64))
}

func longText(s string) map[string]any {
	sum := sha256.Sum256([]byte(s))
	return map[string]any{longTextKey: true, "sha256": hex.EncodeToString(sum[:]), "chars": json.Number(strconv.Itoa(utf8.RuneCountInString(s)))}
}

func isLongText(m map[string]any) bool {
	flag, ok := m[longTextKey].(bool)
	return ok && flag
}

// sameValue compares two normalized values.
func sameValue(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(x, y)
}

// display is how a value appears in changes: long text as "(long text, N characters)".
func display(v any) any {
	if m, ok := v.(map[string]any); ok && isLongText(m) {
		return fmt.Sprintf("(long text, %v characters; compared by hash)", m["chars"])
	}
	return v
}

// scalarText is the plain text of a value, for skip_when matches and dates; "" for nested or missing values.
func scalarText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}
