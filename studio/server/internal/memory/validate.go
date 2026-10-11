// Package memory holds what Studio-stored memory needs besides the database: scope and record validation, vector
// math (cosine ranking and near-duplicate checks), the embedding service with its per-run cache and rate limit, and
// the worker that re-embeds memories after the embedder changes. See docs/crew-state-and-pmo-design.md, section 19.
package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"

	"crewstudio/internal/model"
)

// DefaultImportance is a memory's importance when nobody gave one (CrewAI's default).
const DefaultImportance = 0.5

var (
	scopeSegment = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)
	categoryName = regexp.MustCompile(`^[a-z0-9 _-]{1,40}$`)
	uuidPattern  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// Problems collects "field: problem" strings; Err turns them into a 422.
type Problems []string

// Add records one problem with a field.
func (p *Problems) Add(field, format string, args ...any) {
	*p = append(*p, field+": "+fmt.Sprintf(format, args...))
}

// Err is nil without problems, otherwise a *model.ValidationError.
func (p Problems) Err() error {
	if len(p) == 0 {
		return nil
	}
	return &model.ValidationError{Details: p}
}

// NormalizeScope turns a scope from a run or a person into a path relative to the crew: lowercase, a leading '/',
// no empty, '.' or '..' parts, a leading /crew/<this crew's id> removed, at most 8 parts of [a-z0-9_-]{1,64} and 200
// characters. The crew itself is the boundary (memories.crew_id), so no path can reach another crew.
func NormalizeScope(field, raw, crewID string) (string, error) {
	var p Problems
	out := cleanScope(&p, field, raw, crewID)
	return out, p.Err()
}

func cleanScope(p *Problems, field, raw, crewID string) string {
	var segs []string
	for _, part := range strings.Split(strings.ToLower(strings.TrimSpace(raw)), "/") {
		if part != "" {
			segs = append(segs, part)
		}
	}
	if len(segs) >= 2 && segs[0] == "crew" && segs[1] == strings.ToLower(crewID) {
		segs = segs[2:]
	}
	for _, s := range segs {
		switch {
		case s == "." || s == "..":
			p.Add(field, "must not contain '.' or '..' parts")
			return ""
		case !scopeSegment.MatchString(s):
			p.Add(field, "each part must be 1 to %d lowercase letters, digits, '_' or '-' (such as /people/sam)", model.MaxMemoryScopeSegment)
			return ""
		}
	}
	if len(segs) > model.MaxMemoryScopeSegments {
		p.Add(field, "may have at most %d parts", model.MaxMemoryScopeSegments)
		return ""
	}
	out := "/" + strings.Join(segs, "/")
	if len(out) > model.MaxMemoryScopeLen {
		p.Add(field, "must be at most %d characters", model.MaxMemoryScopeLen)
		return ""
	}
	return out
}

// ScopeUnder reports whether scope is prefix or below it, by whole parts ("" and "/" hold everything).
func ScopeUnder(scope, prefix string) bool {
	if prefix == "" || prefix == "/" {
		return true
	}
	return scope == prefix || strings.HasPrefix(scope, prefix+"/")
}

// ChildScope is the scope one part below parent on the way to scope, or "" when scope is not below parent.
func ChildScope(scope, parent string) string {
	if parent == "" {
		parent = "/"
	}
	if scope == parent || !ScopeUnder(scope, parent) {
		return ""
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(scope, strings.TrimSuffix(parent, "/")), "/")
	next, _, _ := strings.Cut(rest, "/")
	return strings.TrimSuffix(parent, "/") + "/" + next
}

// Input is one memory as a run or a person sends it.
type Input struct {
	ID         string         `json:"id"`
	Content    string         `json:"content"`
	Scope      string         `json:"scope"`
	Categories []string       `json:"categories"`
	Metadata   map[string]any `json:"metadata"`
	Importance *float64       `json:"importance"`
}

// Clean is a checked Input with its content hash.
type Clean struct {
	ID         string
	Content    string
	Scope      string
	Categories []string
	Metadata   map[string]any
	Importance float64
	Hash       string
}

// CleanInput checks one memory, adding problems as "<prefix>.<field>: ...", and returns the cleaned copy.
// An empty prefix names the fields alone ("content: ...").
func CleanInput(p *Problems, prefix string, in Input, crewID string) Clean {
	field := func(name string) string {
		if prefix == "" {
			return name
		}
		return prefix + "." + name
	}
	out := Clean{ID: strings.TrimSpace(in.ID), Content: CleanContent(p, field("content"), in.Content)}
	if out.ID != "" && !uuidPattern.MatchString(out.ID) {
		p.Add(field("id"), "must be a UUID")
	}
	out.Scope = cleanScope(p, field("scope"), in.Scope, crewID)
	out.Categories = CleanCategories(p, field("categories"), in.Categories)
	out.Metadata = CleanMetadata(p, field("metadata"), in.Metadata)
	out.Importance = CleanImportance(p, field("importance"), in.Importance)
	out.Hash = ContentHash(out.Content)
	return out
}

// invisible reports whether r is a control character other than a newline or tab, a line or paragraph separator
// (U+2028, U+2029), or a format character (bidi controls, zero-width characters): text that can hide or reorder what
// a memory appears to say when it is quoted back to an agent or shown to a person.
func invisible(r rune) bool {
	return (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.In(r, unicode.Zl, unicode.Zp, unicode.Cf)
}

// StripInvisible removes what invisible matches, for names Studio stores itself (such as an agent's role).
func StripInvisible(s string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if invisible(r) {
			return ' '
		}
		return r
	}, s)), " ")
}

// CleanContent trims a memory's text and checks it: 1 to 2000 characters, no control, separator or format
// characters apart from newlines and tabs.
func CleanContent(p *Problems, field, content string) string {
	content = strings.TrimSpace(content)
	n := len([]rune(content))
	switch {
	case n == 0:
		p.Add(field, "is required")
	case n > model.MaxMemoryContent:
		p.Add(field, "must be at most %d characters", model.MaxMemoryContent)
	case strings.IndexFunc(content, invisible) >= 0:
		p.Add(field, "must not contain control characters")
	}
	return content
}

// CleanCategories lowercases, trims and de-duplicates categories: at most 10 of [a-z0-9 _-]{1,40}.
func CleanCategories(p *Problems, field string, in []string) []string {
	out := []string{}
	for _, c := range in {
		c = strings.ToLower(strings.TrimSpace(c))
		if !categoryName.MatchString(c) {
			p.Add(field, "%q must be 1 to %d lowercase letters, digits, spaces, '_' or '-'", c, model.MaxMemoryCategoryLen)
			return out
		}
		if !contains(out, c) {
			out = append(out, c)
		}
	}
	if len(out) > model.MaxMemoryCategories {
		p.Add(field, "at most %d", model.MaxMemoryCategories)
	}
	return out
}

// CleanMetadata checks metadata: a flat object of at most 20 keys and 4 KB whose values are strings, numbers,
// booleans, null or lists of those.
func CleanMetadata(p *Problems, field string, in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	if len(in) > model.MaxMemoryMetadataKeys {
		p.Add(field, "at most %d keys", model.MaxMemoryMetadataKeys)
		return in
	}
	for k, v := range in {
		if k == "" || len(k) > 64 || strings.ContainsRune(k, 0) {
			p.Add(field, "keys must be 1 to 64 characters")
			return in
		}
		if !flatValue(v, true) {
			p.Add(fmt.Sprintf("%s[%q]", field, k), "must be a string, number, boolean, null or a list of those")
			return in
		}
		if hasInvisible(v) {
			p.Add(fmt.Sprintf("%s[%q]", field, k), "must not contain control characters")
			return in
		}
	}
	if data, err := json.Marshal(in); err != nil || len(data) > model.MaxMemoryMetadataBytes {
		p.Add(field, "at most %d bytes as JSON", model.MaxMemoryMetadataBytes)
	}
	return in
}

// hasInvisible reports whether a flat metadata value (a string, or a list holding strings) has invisible characters.
func hasInvisible(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.IndexFunc(x, invisible) >= 0
	case []any:
		for _, item := range x {
			if hasInvisible(item) {
				return true
			}
		}
	}
	return false
}

func flatValue(v any, listOK bool) bool {
	switch x := v.(type) {
	case nil, bool, float64, string:
		return true
	case []any:
		if !listOK {
			return false
		}
		for _, item := range x {
			if !flatValue(item, false) {
				return false
			}
		}
		return true
	}
	return false
}

// CleanImportance is the importance to store: DefaultImportance when not given, otherwise 0 to 1.
func CleanImportance(p *Problems, field string, v *float64) float64 {
	if v == nil {
		return DefaultImportance
	}
	if math.IsNaN(*v) || *v < 0 || *v > 1 {
		p.Add(field, "must be between 0 and 1")
		return DefaultImportance
	}
	return *v
}

// ContentHash identifies a memory's text regardless of case and spacing, for exact duplicates within a scope.
func ContentHash(content string) string {
	norm := strings.Join(strings.Fields(strings.ToLower(content)), " ")
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
