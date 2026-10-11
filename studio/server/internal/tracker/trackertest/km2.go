package trackertest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// KM2 serves the KM2 v1 catalog and records endpoints the km2_tracker client uses (KM2's routers/v1/entities.py and
// records.py), over the same Data as the Jira and Linear fakes: people are <prefix>person records, Data.Projects
// <prefix>project records, issues <prefix>issue records and their comments <prefix>comment records. It follows
// KM2's rules: filter=<field>:<op>[:<value>] (split at most twice), order_by any field or base column with id
// descending as the tie-break, opaque keyset cursors tied to their sort, at most 200 records a page, relationship
// values as bare record ids, 400 for unknown fields and 404 for unknown entities. The key goes in
// "Authorization: Bearer km2_...".
type KM2 struct {
	Data *Data
	Key  string
	// Prefix is the entity slug prefix (default trk_)
	Prefix string
	// Hidden hides fields from the catalog and the records (entity name such as "issue" -> field slugs); a nil list
	// hides the whole entity
	Hidden map[string][]string
	// Collide makes that many issue creates find their identifier just taken (as when two writers race), answering
	// 409 as KM2 2.6.2 does
	Collide int
	// Fail500 answers that many issue creates with HTTP 500, as KM2 before 2.6.2 answers a unique violation
	Fail500 int
	// Fail500AfterWrite stores that many issue creates and then answers HTTP 500 (a failure after the write)
	Fail500AfterWrite int
	// CatalogEdit, when set, changes each catalog field (as JSON) before it is served
	CatalogEdit func(entity string, field map[string]any)
	// BareComments leaves the copied issue_key and project_key out of comment records (as a comment made in KM2's
	// own screens may), so only the issue relationship links them
	BareComments bool
	// BadCommentTimes gives these comment ids a created_at that is not a time
	BadCommentTimes map[string]bool
	// RealUpdated is every record's updated_at; activity_at carries the issue's Updated (a simulated time)
	RealUpdated time.Time
	nextID      int
}

// KM2Time is how KM2 (Python's isoformat) writes timestamps.
const KM2Time = "2006-01-02T15:04:05.999999-07:00"

// NewKM2 builds a fake KM2 organisation over d with the default prefix.
func NewKM2(d *Data, key string) *KM2 {
	return &KM2{Data: d, Key: key, Prefix: "trk_", Hidden: map[string][]string{}, RealUpdated: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
}

type km2Field struct {
	slug     string
	typ      string
	required bool
	options  []string
}

// km2Schema is the tracker entity set (docs/trackers.md, "KM2 issues"); rels are to-one relationship slugs.
var km2Schema = map[string]struct {
	fields []km2Field
	rels   []string
}{
	"person": {fields: []km2Field{{"name", "text", true, nil}, {"email", "text", true, nil}, {"role", "text", false, nil},
		{"active", "boolean", false, nil}, {"is_bot", "boolean", false, nil}}},
	"project": {fields: []km2Field{{"key", "text", true, nil}, {"name", "text", false, nil}, {"description", "long_text", false, nil},
		{"status", "picklist", false, []string{"planned", "active", "paused", "completed", "canceled"}}}, rels: []string{"lead"}},
	"issue": {fields: []km2Field{{"identifier", "text", true, nil}, {"number", "integer", false, nil}, {"project_key", "text", true, nil},
		{"title", "text", false, nil}, {"description", "long_text", false, nil}, {"issue_type", "picklist", false, []string{"Epic", "Story", "Task", "Bug"}},
		{"status", "picklist", false, []string{"Backlog", "Todo", "In Progress", "In Review", "Done", "Canceled", "Duplicate", "To Do", "Won't Do"}},
		{"status_category", "picklist", true, []string{"to_do", "in_progress", "done", "canceled"}},
		{"priority", "picklist", false, []string{"Urgent", "High", "Medium", "Low", "None", ""}}, {"labels", "text", false, nil},
		{"estimate", "integer", false, nil}, {"due_date", "date", false, nil}, {"parent_key", "text", false, nil},
		{"activity_at", "timestamptz", false, nil}}, rels: []string{"project", "parent", "assignee", "creator"}},
	"comment": {fields: []km2Field{{"body", "long_text", true, nil}, {"issue_key", "text", false, nil}, {"project_key", "text", false, nil},
		{"author_name", "text", false, nil}, {"mentions", "text", false, nil}, {"sim_posted_at", "timestamptz", false, nil}},
		rels: []string{"issue", "author"}},
}

// Handler serves the fake.
func (k *KM2) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/entities/{slug}", func(w http.ResponseWriter, r *http.Request) {
		name, ok := k.entity(r.PathValue("slug"))
		if !ok {
			km2Detail(w, 404, "entity not found")
			return
		}
		fields := []any{}
		for i, f := range k.fields(name) {
			field := map[string]any{"id": fmt.Sprintf("f-%s-%d", name, i), "name": f.slug, "slug": f.slug, "field_type": f.typ,
				"picklist_options": f.options, "is_required": f.required, "is_unique": f.slug == "identifier" || f.slug == "email" || f.slug == "key",
				"default_value": nil, "order": i, "read_access": "member"}
			if k.CatalogEdit != nil {
				k.CatalogEdit(name, field)
			}
			fields = append(fields, field)
		}
		writeJSON(w, 200, map[string]any{"id": "e-" + name, "name": name, "slug": r.PathValue("slug"), "description": nil, "is_active": true,
			"write_access": "member", "fields": fields})
	})
	mux.HandleFunc("GET /api/v1/entities/{slug}/records", k.list)
	mux.HandleFunc("GET /api/v1/entities/{slug}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		name, ok := k.entity(r.PathValue("slug"))
		if !ok {
			km2Detail(w, 404, "entity not found")
			return
		}
		for _, rec := range k.records(name) {
			if rec["id"] == r.PathValue("id") {
				writeJSON(w, 200, rec)
				return
			}
		}
		km2Detail(w, 404, "record not found")
	})
	mux.HandleFunc("POST /api/v1/entities/{slug}/records", k.create)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d := k.Data
		d.mu.Lock()
		defer d.mu.Unlock()
		d.Requests = append(d.Requests, r.Method+" "+r.URL.RequestURI())
		if d.failing(w) {
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+k.Key {
			w.Header().Set("WWW-Authenticate", "Bearer")
			km2Detail(w, 401, "Invalid or missing API key")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func km2Detail(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]any{"detail": detail})
}

func (k *KM2) entity(slug string) (string, bool) {
	name, ok := strings.CutPrefix(slug, k.Prefix)
	if _, known := km2Schema[name]; !ok || !known {
		return "", false
	}
	if hidden, listed := k.Hidden[name]; listed && hidden == nil {
		return "", false
	}
	return name, true
}

func (k *KM2) fields(name string) []km2Field {
	return slices.DeleteFunc(slices.Clone(km2Schema[name].fields), func(f km2Field) bool { return slices.Contains(k.Hidden[name], f.slug) })
}

// columns are the slugs a record of the entity has: base columns, fields and relationships.
func (k *KM2) columns(name string) map[string]km2Field {
	out := map[string]km2Field{"id": {"id", "uuid", false, nil}, "created_at": {"created_at", "timestamptz", false, nil},
		"updated_at": {"updated_at", "timestamptz", false, nil}}
	for _, f := range k.fields(name) {
		out[f.slug] = f
	}
	for _, rel := range km2Schema[name].rels {
		out[rel] = km2Field{rel, "uuid", false, nil}
	}
	return out
}

func km2At(t time.Time) string { return t.UTC().Format(KM2Time) }

func (k *KM2) people() []User {
	out := slices.Clone(k.Data.Users)
	seen := map[string]bool{k.Data.Me.ID: true}
	for _, u := range out {
		seen[u.ID] = true
	}
	for _, key := range sortedKeys(k.Data.Projects) {
		if lead := k.Data.Projects[key]; lead != nil && !seen[lead.ID] {
			seen[lead.ID] = true
			out = append(out, *lead)
		}
	}
	return append(out, k.Data.Me)
}

func km2Category(i *Issue) string {
	switch {
	case i.Category == "indeterminate":
		return "in_progress"
	case i.Category == "done" && slices.Contains([]string{"canceled", "won't do", "duplicate"}, strings.ToLower(i.Status)):
		return "canceled"
	case i.Category == "done":
		return "done"
	}
	return "to_do"
}

func idOf(u *User) any {
	if u == nil {
		return nil
	}
	return u.ID
}

// records builds the entity's records from Data, oldest first, without hidden fields.
func (k *KM2) records(name string) []map[string]any {
	d := k.Data
	base := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	var out []map[string]any
	add := func(rec map[string]any) {
		if rec["created_at"] == nil {
			rec["created_at"] = km2At(base.Add(time.Duration(len(out)) * time.Minute))
		}
		if rec["updated_at"] == nil {
			rec["updated_at"] = km2At(k.RealUpdated)
		}
		out = append(out, rec)
	}
	switch name {
	case "person":
		for _, u := range k.people() {
			add(map[string]any{"id": u.ID, "name": u.Name, "email": u.Email, "role": nil, "active": true, "is_bot": u.ID == d.Me.ID})
		}
	case "project":
		for _, key := range sortedKeys(d.Projects) {
			add(map[string]any{"id": "proj-" + key, "key": key, "name": "Project " + key, "description": nil, "status": "active", "lead": idOf(d.Projects[key])})
		}
	case "issue":
		for _, i := range d.sortedIssues(func(*Issue) bool { return true }) {
			add(k.issueRecord(i))
		}
	case "comment":
		for _, i := range d.sortedIssues(func(*Issue) bool { return true }) {
			for _, c := range i.Comments {
				rec := map[string]any{"id": c.ID, "body": c.Body, "issue": i.ID, "issue_key": i.Key, "project_key": i.Project,
					"author": c.Author.ID, "author_name": c.Author.Name, "mentions": nil, "sim_posted_at": nil,
					"created_at": km2At(c.At), "updated_at": km2At(c.At)}
				if k.BareComments {
					rec["issue_key"], rec["project_key"] = nil, nil
				}
				if k.BadCommentTimes[c.ID] {
					rec["created_at"] = "not a time"
				}
				out = append(out, rec)
			}
		}
	}
	cols := k.columns(name)
	for _, rec := range out {
		for key := range rec {
			if _, ok := cols[key]; !ok {
				delete(rec, key)
			}
		}
	}
	return out
}

func (k *KM2) issueRecord(i *Issue) map[string]any {
	number, _ := strconv.Atoi(i.Key[strings.LastIndex(i.Key, "-")+1:])
	rec := map[string]any{"id": i.ID, "identifier": i.Key, "number": number, "project_key": i.Project, "title": i.Title,
		"description": "About " + i.Title, "issue_type": "Task", "status": i.Status, "status_category": km2Category(i), "priority": nilIfEmpty(i.Priority),
		"labels": nilIfEmpty(strings.Join(i.Labels, ", ")), "estimate": nil, "due_date": nilIfEmpty(i.Due), "parent_key": nilIfEmpty(i.Parent),
		"activity_at": km2At(i.Updated), "project": "proj-" + i.Project, "parent": nil, "assignee": idOf(i.Assignee), "creator": nilIfEmpty(i.Creator)}
	if p := k.Data.Issues[i.Parent]; p != nil {
		rec["parent"] = p.ID
	}
	if !i.Created.IsZero() {
		rec["created_at"] = km2At(i.Created)
	}
	return rec
}

type km2Cursor struct {
	Slug string `json:"s"`
	Dir  string `json:"d"`
	V    any    `json:"v"`
	ID   string `json:"id"`
}

func (k *KM2) list(w http.ResponseWriter, r *http.Request) {
	name, ok := k.entity(r.PathValue("slug"))
	if !ok {
		km2Detail(w, 404, "entity not found")
		return
	}
	q := r.URL.Query()
	cols := k.columns(name)
	recs := k.records(name)
	for _, raw := range q["filter"] {
		parts := strings.SplitN(raw, ":", 3)
		if len(parts) < 2 || parts[0] == "" {
			km2Detail(w, 400, fmt.Sprintf("invalid filter %q; expected '<field>:<op>[:<value>]'", raw))
			return
		}
		col, known := cols[parts[0]]
		if !known {
			km2Detail(w, 400, fmt.Sprintf("unknown field: '%s'", parts[0]))
			return
		}
		value := ""
		if len(parts) == 3 {
			value = parts[2]
		}
		match, err := km2Matcher(col, parts[1], value)
		if err != "" {
			km2Detail(w, 400, err)
			return
		}
		recs = slices.DeleteFunc(recs, func(rec map[string]any) bool { return !match(rec[col.slug]) })
	}
	order := q.Get("order_by")
	if order == "" {
		order = "created_at"
	}
	col, known := cols[order]
	if !known {
		km2Detail(w, 400, fmt.Sprintf("unknown field: '%s'", order))
		return
	}
	desc := q.Get("order_dir") != "asc"
	compare := func(a, b map[string]any) int {
		c := km2Compare(col, a[order], b[order], desc)
		if c == 0 {
			return strings.Compare(fmt.Sprint(b["id"]), fmt.Sprint(a["id"]))
		}
		return c
	}
	slices.SortStableFunc(recs, compare)
	if cur := q.Get("cursor"); cur != "" {
		var c km2Cursor
		raw, err := base64.URLEncoding.DecodeString(cur)
		if err != nil || json.Unmarshal(raw, &c) != nil {
			km2Detail(w, 400, "invalid cursor")
			return
		}
		if c.Slug != order || (c.Dir == "desc") != desc {
			km2Detail(w, 400, "cursor does not match the requested sort order")
			return
		}
		after := map[string]any{order: c.V, "id": c.ID}
		recs = slices.DeleteFunc(recs, func(rec map[string]any) bool { return compare(rec, after) <= 0 })
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if q.Get("limit") == "" {
		limit = 50
	} else if err != nil || limit < 1 || limit > 200 {
		writeJSON(w, 422, map[string]any{"detail": []any{map[string]any{"loc": []string{"query", "limit"}, "msg": "Input should be between 1 and 200"}}})
		return
	}
	if k.Data.PageSize > 0 && limit > k.Data.PageSize {
		limit = k.Data.PageSize
	}
	var next any
	if len(recs) > limit {
		last := recs[limit-1]
		dir := "asc"
		if desc {
			dir = "desc"
		}
		raw, _ := json.Marshal(km2Cursor{Slug: order, Dir: dir, V: last[order], ID: fmt.Sprint(last["id"])})
		next = base64.URLEncoding.EncodeToString(raw)
		recs = recs[:limit]
	}
	writeJSON(w, 200, map[string]any{"items": recs, "next_cursor": next, "limit": limit})
}

// km2Compare orders two values of a column ascending; nulls sort last ascending and first descending.
func km2Compare(col km2Field, a, b any, desc bool) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		if desc {
			return -1
		}
		return 1
	case b == nil:
		if desc {
			return 1
		}
		return -1
	}
	c := km2CompareValues(col, a, fmt.Sprint(b))
	if desc {
		return -c
	}
	return c
}

// km2CompareValues compares a record value with a filter value as the column's type.
func km2CompareValues(col km2Field, v any, want string) int {
	switch col.typ {
	case "integer":
		a, _ := strconv.ParseFloat(fmt.Sprint(v), 64)
		b, _ := strconv.ParseFloat(want, 64)
		switch {
		case a < b:
			return -1
		case a > b:
			return 1
		}
		return 0
	case "timestamptz":
		a, errA := time.Parse(time.RFC3339Nano, fmt.Sprint(v))
		b, errB := time.Parse(time.RFC3339Nano, want)
		if errA == nil && errB == nil {
			return a.Compare(b)
		}
	}
	return strings.Compare(fmt.Sprint(v), want)
}

// km2Matcher is one filter clause as a test on a record value, or KM2's error for it.
func km2Matcher(col km2Field, op, value string) (func(any) bool, string) {
	if col.typ == "timestamptz" && op != "isnull" && op != "in" {
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return nil, fmt.Sprintf("'%s' must be a valid date-time", col.slug)
		}
	}
	cmp := func(v any) int { return km2CompareValues(col, v, value) }
	switch op {
	case "eq":
		return func(v any) bool { return v != nil && cmp(v) == 0 }, ""
	case "ne":
		return func(v any) bool { return v != nil && cmp(v) != 0 }, ""
	case "gt":
		return func(v any) bool { return v != nil && cmp(v) > 0 }, ""
	case "gte":
		return func(v any) bool { return v != nil && cmp(v) >= 0 }, ""
	case "lt":
		return func(v any) bool { return v != nil && cmp(v) < 0 }, ""
	case "lte":
		return func(v any) bool { return v != nil && cmp(v) <= 0 }, ""
	case "in":
		values := strings.Split(value, ",")
		if len(values) > 200 {
			return nil, "'in' filter accepts at most 200 values"
		}
		return func(v any) bool { return v != nil && slices.Contains(values, fmt.Sprint(v)) }, ""
	case "contains":
		return func(v any) bool {
			return v != nil && strings.Contains(strings.ToLower(fmt.Sprint(v)), strings.ToLower(value))
		}, ""
	case "isnull":
		want := value == "" || slices.Contains([]string{"true", "1", "yes", "y"}, strings.ToLower(value))
		return func(v any) bool { return (v == nil) == want }, ""
	}
	return nil, fmt.Sprintf("unknown filter operator '%s'", op)
}

func (k *KM2) create(w http.ResponseWriter, r *http.Request) {
	name, ok := k.entity(r.PathValue("slug"))
	if !ok {
		km2Detail(w, 404, "entity not found")
		return
	}
	var body map[string]any
	raw, _ := io.ReadAll(r.Body)
	if json.Unmarshal(raw, &body) != nil {
		km2Detail(w, 422, "body must be a JSON object")
		return
	}
	cols := k.columns(name)
	var unknown []string
	for key := range body {
		if _, ok := cols[key]; !ok || key == "id" || key == "created_at" || key == "updated_at" {
			unknown = append(unknown, "'"+key+"'")
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		km2Detail(w, 400, "unknown fields: ["+strings.Join(unknown, ", ")+"]")
		return
	}
	for _, f := range k.fields(name) {
		if f.required && body[f.slug] == nil {
			km2Detail(w, 400, fmt.Sprintf("'%s' is required", f.slug))
			return
		}
		if s, isText := body[f.slug].(string); isText && f.options != nil && !slices.Contains(f.options, s) {
			km2Detail(w, 400, fmt.Sprintf("'%s' must be one of %v", f.slug, f.options))
			return
		}
	}
	switch name {
	case "comment":
		k.createComment(w, body)
	case "issue":
		k.createIssue(w, body)
	default:
		km2Detail(w, 403, "the fake only creates comments and issues")
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func (k *KM2) issueByID(id string) *Issue {
	for _, i := range k.Data.Issues {
		if i.ID == id {
			return i
		}
	}
	return nil
}

func (k *KM2) createComment(w http.ResponseWriter, body map[string]any) {
	d := k.Data
	issue := k.issueByID(str(body["issue"]))
	if issue == nil {
		km2Detail(w, 400, "'issue' points at a record that does not exist")
		return
	}
	k.nextID++
	c := Comment{ID: fmt.Sprintf("c0000000-0000-4000-8000-%012d", k.nextID), Author: User{ID: str(body["author"]), Name: str(body["author_name"])},
		At: time.Now().UTC().Truncate(time.Microsecond), Body: str(body["body"])}
	issue.Comments = append(issue.Comments, c)
	d.Posted = append(d.Posted, Posted{Issue: issue.Key, Body: body})
	writeJSON(w, 201, map[string]any{"id": c.ID, "body": c.Body, "issue": issue.ID, "issue_key": body["issue_key"], "project_key": body["project_key"],
		"author": body["author"], "author_name": body["author_name"], "mentions": body["mentions"], "created_at": km2At(c.At), "updated_at": km2At(c.At)})
}

func (k *KM2) createIssue(w http.ResponseWriter, body map[string]any) {
	d := k.Data
	key := str(body["identifier"])
	if k.Fail500 > 0 {
		k.Fail500--
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(500)
		_, _ = w.Write([]byte("Internal Server Error"))
		return
	}
	if k.Collide > 0 {
		k.Collide--
		d.nextID++
		d.Issues[key] = &Issue{ID: fmt.Sprintf("1%04d", d.nextID), Key: key, Title: "Someone else's", Project: str(body["project_key"]),
			Category: "new", Status: "Backlog", Updated: time.Now()}
	}
	if _, taken := d.Issues[key]; taken {
		km2Detail(w, 409, "a record with this identifier already exists")
		return
	}
	d.Created = append(d.Created, body)
	d.nextID++
	i := &Issue{ID: fmt.Sprintf("1%04d", d.nextID), Key: key, Title: str(body["title"]), Project: str(body["project_key"]),
		Parent: str(body["parent_key"]), Status: str(body["status"]), Category: "new", Updated: time.Now().UTC(), Creator: str(body["creator"]), Created: time.Now().UTC()}
	d.Issues[key] = i
	if k.Fail500AfterWrite > 0 {
		k.Fail500AfterWrite--
		w.WriteHeader(500)
		_, _ = w.Write([]byte("Internal Server Error"))
		return
	}
	rec := k.issueRecord(i)
	rec["updated_at"] = km2At(i.Updated)
	writeJSON(w, 201, rec)
}
