package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"crewstudio/internal/model"
)

// KM2 custom entities as a tracker (docs/trackers.md, "KM2 issues"), through KM2's v1 records API
// (/api/v1/entities/{slug}/records): filter=<field>:<op>[:<value>], order_by and an opaque keyset cursor, at most 200
// records a page, relationships as bare record ids. The key is an org-wide km2_ key in "Authorization: Bearer".
// Projects are <prefix>project records keyed by their key (such as ERP), issues <prefix>issue records keyed by their
// identifier (ERP-12), people <prefix>person records and comments <prefix>comment records. Studio creates comments
// (and, when allowed, issues) and never edits a record.

// Limits of KM2 reads (vars so tests can lower them).
var (
	// km2InLimit is the most values KM2 accepts in one "in" filter
	km2InLimit = 200
	// maxKM2Comments bounds the new comments one search reads, across all its calls
	maxKM2Comments = 10_000
)

const (
	km2PageSize = 200
	maxKM2Pages = 200
	// km2CreateTries is how often a new issue is tried with the next free number when KM2 reports a conflict
	km2CreateTries = 3
	// km2ConflictBackoff is the wait before the second try after a conflict (doubled before the third)
	km2ConflictBackoff = 300 * time.Millisecond
	// km2RecentCreate is how far back an issue counts as the one a failed create may have made after all
	km2RecentCreate = 5 * time.Minute
	km2Path         = "/api/v1/entities/"
	// km2TimeLayout writes times as KM2 (Python's isoformat) does, with a numeric offset
	km2TimeLayout = "2006-01-02T15:04:05.999999-07:00"
)

// km2Status maps status_category values to Studio's categories. The schema requires status_category to be a
// picklist of exactly these four values (Check verifies it), so the server-side filter on to_do and in_progress and
// this mapping cannot disagree.
var km2Status = map[string]string{"to_do": model.StatusToDo, "in_progress": model.StatusInProgress, "done": model.StatusDone,
	"canceled": model.StatusCanceled}

// km2BaseColumns are on every KM2 record.
var km2BaseColumns = []string{"id", "created_at", "updated_at"}

type km2Tracker struct {
	name  string
	base  string
	key   string
	s     model.KM2TrackerSettings
	h     *httpClient
	log   *slog.Logger
	cache *km2PeopleCache

	mu sync.Mutex
	me *Person
}

// CheckKM2Base allows an https KM2 address, or plain http to this machine, with no path, credentials, query or
// fragment, so the org key is only ever sent to a KM2 API. When an administrator listed origins in
// STUDIO_KM2_TRACKER_SITES, only those are allowed.
func CheckKM2Base(base string, allowed []string) error {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") ||
		(u.Scheme != "https" && !(u.Scheme == "http" && model.LoopbackHTTP(u))) {
		return errorf("the KM2 base URL must be an https address such as https://km2.example.com (plain http only to this machine)")
	}
	if len(allowed) == 0 {
		return nil
	}
	origin := u.Scheme + "://" + u.Host
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimRight(a, "/"), origin) {
			return nil
		}
	}
	return errorf("the KM2 base URL must be one of the KM2 sites an administrator allowed (STUDIO_KM2_TRACKER_SITES)")
}

func newKM2(conn model.Connection, secrets map[string]string, opts Options, h *httpClient) (*km2Tracker, error) {
	s := model.KM2TrackerSettingsOf(conn.Config)
	if err := CheckKM2Base(s.BaseURL, opts.KM2TrackerSites); err != nil {
		return nil, err
	}
	key := strings.TrimSpace(secrets["api_key"])
	switch {
	case key == "":
		return nil, errorf("%s has no API key saved; replace its secret", conn.Name)
	case !strings.HasPrefix(key, model.KM2KeyPrefix):
		return nil, errorf("the API key of %s is not a KM2 key (KM2 keys start with %s); replace its secret", conn.Name, model.KM2KeyPrefix)
	case s.SelfEmail == "":
		return nil, errorf("%s has no Crew Studio person email; set it in the connection's settings", conn.Name)
	}
	if err := model.CheckKM2TrackerNames(s); err != nil {
		return nil, errorf("%s: %v", conn.Name, err)
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	cacheKey := ""
	if conn.ID != "" {
		cacheKey = conn.ID + "|" + s.BaseURL + "|" + s.EntityPrefix
	}
	return &km2Tracker{name: conn.Name, base: s.BaseURL, key: key, s: s, h: h, log: log, cache: opts.km2.get(cacheKey)}, nil
}

func (k *km2Tracker) Kind() string { return model.TrackerKM2 }

// record is one KM2 record as JSON values (fields are typed by the organisation's schema).
type record map[string]json.RawMessage

func (r record) str(field string) string {
	raw, ok := r[field]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

func (r record) time(field string) time.Time {
	v := r.str(field)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999", time.DateOnly} {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (r record) bool(field string) (bool, bool) {
	var b bool
	if raw, ok := r[field]; ok && json.Unmarshal(raw, &b) == nil {
		return b, true
	}
	return false, false
}

type km2Page struct {
	Items      []record `json:"items"`
	NextCursor *string  `json:"next_cursor"`
}

// call sends one request to the v1 API and decodes a 2xx answer into out.
func (k *km2Tracker) call(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	target := k.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encode km2 request: %w", err)
		}
	}
	resp, err := k.h.do(ctx, k.name, request{method: method, url: target, body: payload, headers: map[string]string{"Authorization": "Bearer " + k.key}})
	if err != nil {
		return err
	}
	if resp.status < 200 || resp.status >= 300 {
		return k.answerError(path, resp)
	}
	if out != nil && json.Unmarshal(resp.body, out) != nil {
		return errorf("%s answered with something that is not the JSON KM2 sends", k.name)
	}
	return nil
}

// maxKM2Detail caps the part of a KM2 error that is passed on
const maxKM2Detail = 300

// km2Detail is FastAPI's error detail, only when the body is KM2's JSON: {"detail": "..."} or a list of validation
// problems with string messages. Anything else (an HTML error page, a proxy's text) gives "", so it is never repeated.
// Control characters are dropped and the text is cut to maxKM2Detail characters.
func km2Detail(body []byte) string {
	var res struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(body, &res) != nil || len(res.Detail) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(res.Detail, &s) != nil {
		var list []struct {
			Msg *string `json:"msg"`
		}
		if json.Unmarshal(res.Detail, &list) != nil {
			return ""
		}
		msgs := make([]string, 0, len(list))
		for _, p := range list {
			if p.Msg == nil {
				return ""
			}
			msgs = append(msgs, *p.Msg)
		}
		s = strings.Join(msgs, "; ")
	}
	return oneLine(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, s), maxKM2Detail)
}

// entityOf is the entity slug a request path names.
func entityOf(path string) string {
	rest, _ := strings.CutPrefix(path, km2Path)
	slug, _, _ := strings.Cut(rest, "/")
	return slug
}

// answerError turns an error answer into a safe message: the status, KM2's short detail, and what it means for the
// setup.
func (k *km2Tracker) answerError(path string, resp response) error {
	detail := km2Detail(resp.body)
	e := rejectedIf(&Error{Status: resp.status, Message: fmt.Sprintf("%s answered HTTP %d", k.name, resp.status)}, resp.status)
	if detail != "" {
		e.Message += ": " + detail
	}
	lower := strings.ToLower(detail)
	switch {
	case resp.status == http.StatusUnauthorized:
		e.Auth = true
		e.Message = k.name + " refused the API key; replace it"
	case resp.status == http.StatusForbidden:
		e.Auth = true
		e.Message = fmt.Sprintf("%s does not allow this with its API key (it needs entities:read, records:read and records:write): %s", k.name, detail)
	case resp.status == http.StatusNotFound && strings.Contains(lower, "entity"):
		e.Message = fmt.Sprintf("%s: the KM2 organisation has no %s entity; set up the tracker entities (see the Test button)", k.name, entityOf(path))
	case resp.status == http.StatusNotFound || resp.status == http.StatusUnprocessableEntity && strings.Contains(path, "/records/"):
		e.NotFound = true
		e.Message = k.name + ": not found"
	case resp.status == http.StatusBadRequest && strings.Contains(lower, "unknown field"):
		e.Message = fmt.Sprintf("%s: the KM2 organisation lacks fields of the tracker entities (%s); use Test on the connection to list them", k.name, detail)
	case resp.status >= 300 && resp.status < 400:
		e.Message = fmt.Sprintf("%s answered with a redirect (HTTP %d); check the base URL", k.name, resp.status)
	}
	return e
}

func (k *km2Tracker) recordsPath(entity string) string {
	return km2Path + url.PathEscape(k.s.Entity(entity)) + "/records"
}

// list reads every record matching filters, in order, page by page; more than max fails with a *LimitError (0: no
// limit besides maxKM2Pages).
func (k *km2Tracker) list(ctx context.Context, entity string, filters []string, orderBy, dir string, max int) ([]record, error) {
	out := []record{}
	cursor := ""
	for page := 0; page < maxKM2Pages; page++ {
		q := url.Values{"filter": filters, "limit": {strconv.Itoa(km2PageSize)}, "order_by": {orderBy}, "order_dir": {dir}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var res km2Page
		if err := k.call(ctx, http.MethodGet, k.recordsPath(entity), q, nil, &res); err != nil {
			return nil, err
		}
		out = append(out, res.Items...)
		if max > 0 && len(out) > max {
			return nil, &LimitError{Count: len(out), Max: max}
		}
		if res.NextCursor == nil || *res.NextCursor == "" || len(res.Items) == 0 {
			return out, nil
		}
		cursor = *res.NextCursor
	}
	return nil, errorf("%s returned too many pages of %s records", k.name, k.s.Entity(entity))
}

// first reads the first record matching filters, or nil.
func (k *km2Tracker) first(ctx context.Context, entity string, filters []string, orderBy, dir string) (record, error) {
	q := url.Values{"filter": filters, "limit": {"1"}, "order_by": {orderBy}, "order_dir": {dir}}
	var res km2Page
	if err := k.call(ctx, http.MethodGet, k.recordsPath(entity), q, nil, &res); err != nil {
		return nil, err
	}
	if len(res.Items) == 0 {
		return nil, nil
	}
	return res.Items[0], nil
}

func filter(field, op, value string) string { return field + ":" + op + ":" + value }

func (k *km2Tracker) Myself(ctx context.Context) (Person, error) {
	k.mu.Lock()
	me := k.me
	k.mu.Unlock()
	if me != nil {
		return *me, nil
	}
	r, err := k.selfRecord(ctx)
	if err != nil {
		return Person{}, err
	}
	if r == nil {
		return Person{}, errorf("%s: no %s record has the email %s; add the person that stands for Crew Studio", k.name, k.s.Entity("person"), k.s.SelfEmail)
	}
	p := personOf(r)
	k.mu.Lock()
	k.me = &p
	k.mu.Unlock()
	return p, nil
}

// selfRecord finds the Crew Studio person by email, ignoring case: KM2 compares text exactly, so when the stored
// email has other capitals, a case-insensitive "contains" search is narrowed down here.
func (k *km2Tracker) selfRecord(ctx context.Context) (record, error) {
	r, err := k.first(ctx, "person", []string{filter("email", "eq", k.s.SelfEmail)}, "created_at", "asc")
	if err != nil || r != nil {
		return r, err
	}
	q := url.Values{"filter": {filter("email", "contains", k.s.SelfEmail)}, "limit": {strconv.Itoa(km2PageSize)}, "order_by": {"created_at"}, "order_dir": {"asc"}}
	var res km2Page
	if err := k.call(ctx, http.MethodGet, k.recordsPath("person"), q, nil, &res); err != nil {
		return nil, err
	}
	for _, c := range res.Items {
		if strings.EqualFold(strings.TrimSpace(c.str("email")), k.s.SelfEmail) {
			return c, nil
		}
	}
	return nil, nil
}

func (k *km2Tracker) project(ctx context.Context, r record) (Project, error) {
	lead, err := k.person(ctx, r.str("lead"))
	if err != nil {
		return Project{}, err
	}
	return Project{ID: r.str("id"), Key: r.str("key"), Name: r.str("name"), Lead: lead}, nil
}

func (k *km2Tracker) Projects(ctx context.Context, max int) ([]Project, error) {
	recs, err := k.list(ctx, "project", nil, "key", "asc", 0)
	if err != nil {
		return nil, err
	}
	recs = recs[:min(len(recs), max)]
	if err := k.knowPeople(ctx, recs, "lead"); err != nil {
		return nil, err
	}
	out := []Project{}
	for _, r := range recs {
		p, err := k.project(ctx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (k *km2Tracker) Project(ctx context.Context, key string) (Project, error) {
	r, err := k.first(ctx, "project", []string{filter("key", "eq", key)}, "created_at", "asc")
	if err != nil {
		return Project{}, err
	}
	if r == nil {
		return Project{}, &Error{Message: k.name + ": no project with the key " + key, NotFound: true}
	}
	return k.project(ctx, r)
}

func splitLabels(s string) []string {
	out := []string{}
	for _, l := range strings.Split(s, ",") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func km2Category(r record) string {
	if c, ok := km2Status[strings.ToLower(r.str("status_category"))]; ok {
		return c
	}
	return model.StatusToDo
}

func (k *km2Tracker) issue(ctx context.Context, r record, withDescription bool) (Issue, error) {
	assignee, err := k.person(ctx, r.str("assignee"))
	if err != nil {
		return Issue{}, err
	}
	out := Issue{ID: r.str("id"), Key: r.str("identifier"), Title: r.str("title"), Status: r.str("status"), StatusCategory: km2Category(r),
		Assignee: assignee, DueDate: r.str("due_date"), Priority: r.str("priority"), Parent: r.str("parent_key"),
		Labels: splitLabels(r.str("labels")), Project: r.str("project_key")}
	out.URL = k.s.IssueURL(out.ID, out.Key)
	if len(out.DueDate) > len(time.DateOnly) {
		out.DueDate = out.DueDate[:len(time.DateOnly)]
	}
	if out.UpdatedAt = r.time(k.s.ActivityField); out.UpdatedAt.IsZero() {
		out.UpdatedAt = r.time("updated_at")
	}
	if withDescription {
		out.Description = oneLineLimit(r.str("description"), maxDescription)
	}
	return out, nil
}

func (k *km2Tracker) issueRecord(ctx context.Context, ref string) (record, error) {
	r, err := k.first(ctx, "issue", []string{filter("identifier", "eq", ref)}, "created_at", "asc")
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, &Error{Message: k.name + ": not found", NotFound: true}
	}
	return r, nil
}

func (k *km2Tracker) Issue(ctx context.Context, ref string) (Issue, error) {
	r, err := k.issueRecord(ctx, ref)
	if err != nil {
		return Issue{}, err
	}
	return k.issue(ctx, r, true)
}

// Search lists a project's (or a parent's) issues. With CommentsSince, the comments created after it are read in a
// few more calls (by project, or by issue in chunks) and given with every issue, so CommentsRead is set.
func (k *km2Tracker) Search(ctx context.Context, q Query) ([]Issue, error) {
	if strings.TrimSpace(q.JQL) != "" {
		return nil, errorf("JQL only applies to Jira connections; %s is a KM2 connection", k.name)
	}
	var filters []string
	switch {
	case q.Parent != nil && q.Parent.ID != "":
		filters = append(filters, filter("parent", "eq", q.Parent.ID))
	case q.Parent != nil:
		filters = append(filters, filter("parent_key", "eq", q.Parent.Key))
	default:
		filters = append(filters, filter("project_key", "eq", q.Project))
	}
	if !q.IncludeDone {
		filters = append(filters, filter("status_category", "in", model.StatusToDo+","+model.StatusInProgress))
	}
	recs, err := k.list(ctx, "issue", filters, "created_at", "asc", q.Max)
	if err != nil {
		return nil, err
	}
	if err := k.knowPeople(ctx, recs, "assignee"); err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(recs))
	for _, r := range recs {
		issue, err := k.issue(ctx, r, false)
		if err != nil {
			return nil, err
		}
		out = append(out, issue)
	}
	if q.CommentsSince != nil && len(out) > 0 {
		return k.withComments(ctx, q, out)
	}
	return out, nil
}

// withComments adds the comments created after q.CommentsSince to each issue. Comments are found through their
// issue relationship (issue:in: up to 200 issue ids a call), not the copied keys, which a comment made in KM2's own
// screens may lack. At most maxKM2Comments are read across all calls.
func (k *km2Tracker) withComments(ctx context.Context, q Query, issues []Issue) ([]Issue, error) {
	since := filter("created_at", "gt", q.CommentsSince.UTC().Format(km2TimeLayout))
	var recs []record
	for chunk := range slices.Chunk(issues, km2InLimit) {
		ids := make([]string, len(chunk))
		for i, issue := range chunk {
			ids[i] = issue.ID
		}
		// The budget left for this call (at least 1, since 0 would mean no limit; going over is caught below)
		found, err := k.list(ctx, "comment", []string{filter("issue", "in", strings.Join(ids, ",")), since}, "created_at", "asc",
			max(maxKM2Comments-len(recs), 1))
		var le *LimitError
		if errors.As(err, &le) {
			return nil, &LimitError{Count: len(recs) + le.Count, Max: maxKM2Comments}
		}
		if err != nil {
			return nil, err
		}
		if recs = append(recs, found...); len(recs) > maxKM2Comments {
			return nil, &LimitError{Count: len(recs), Max: maxKM2Comments}
		}
	}
	if err := k.knowPeople(ctx, recs, "author"); err != nil {
		return nil, err
	}
	byIssue := map[string][]Comment{}
	for _, r := range recs {
		c, err := k.comment(ctx, r)
		if err != nil {
			return nil, err
		}
		byIssue[r.str("issue")] = append(byIssue[r.str("issue")], c)
	}
	out := slices.Clone(issues)
	for i := range out {
		out[i].Comments, out[i].CommentsRead = newestFirst(byIssue[out[i].ID], -1), true
	}
	return out, nil
}

// comment reads a comment with its real creation time (never a simulated one). A comment whose created_at cannot be
// read keeps a zero time, which watches never report or count as seen; it is logged so it does not vanish silently.
func (k *km2Tracker) comment(ctx context.Context, r record) (Comment, error) {
	author, err := k.person(ctx, r.str("author"))
	if err != nil {
		return Comment{}, err
	}
	c := Comment{ID: r.str("id"), At: r.time("created_at"), Body: r.str("body")}
	if c.At.IsZero() {
		k.log.Warn("a KM2 comment has no readable created_at; watches will not report it", "connection", k.name, "record_id", c.ID)
	}
	if author != nil {
		c.Author = *author
	}
	if c.Author.Name == "" {
		c.Author.Name = cleanName(r.str("author_name"))
	}
	return c, nil
}

// Comments reads an issue's newest comments through the issue relationship.
func (k *km2Tracker) Comments(ctx context.Context, ref string, limit int) ([]Comment, error) {
	issue, err := k.issueRecord(ctx, ref)
	if err != nil {
		return nil, err
	}
	q := url.Values{"filter": {filter("issue", "eq", issue.str("id"))}, "limit": {strconv.Itoa(max(1, min(limit, km2PageSize)))},
		"order_by": {"created_at"}, "order_dir": {"desc"}}
	var res km2Page
	if err := k.call(ctx, http.MethodGet, k.recordsPath("comment"), q, nil, &res); err != nil {
		return nil, err
	}
	if err := k.knowPeople(ctx, res.Items, "author"); err != nil {
		return nil, err
	}
	out := make([]Comment, 0, len(res.Items))
	for _, r := range res.Items {
		c, err := k.comment(ctx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return newestFirst(out, limit), nil
}

// PostComment creates a comment record by the connection's person. KM2 has no mentions: the people are named as
// @Name at the top and their ids kept in the mentions field (nobody is notified).
func (k *km2Tracker) PostComment(ctx context.Context, ref, text string, mentions []Person) (Posted, error) {
	issue, err := k.issueRecord(ctx, ref)
	if err != nil {
		return Posted{}, err
	}
	me, err := k.Myself(ctx)
	if err != nil {
		return Posted{}, err
	}
	names, ids := []string{}, []string{}
	for _, m := range mentions {
		p, err := k.person(ctx, m.AccountID)
		if err != nil {
			return Posted{}, err
		}
		if p.Name == "" {
			return Posted{}, errorf("%s has no person %s, so they cannot be mentioned", k.name, m.AccountID)
		}
		names, ids = append(names, "@"+p.Name), append(ids, p.AccountID)
	}
	body := text
	if len(names) > 0 {
		body = strings.Join(names, " ") + "\n\n" + text
	}
	payload := map[string]any{"body": body, "issue": issue.str("id"), "issue_key": issue.str("identifier"), "project_key": issue.str("project_key"),
		"author": me.AccountID, "author_name": cleanName(me.Name)}
	if len(ids) > 0 {
		payload["mentions"] = strings.Join(ids, ",")
	}
	var created record
	if err := k.call(ctx, http.MethodPost, k.recordsPath("comment"), nil, payload, &created); err != nil {
		return Posted{}, err
	}
	return Posted{ID: created.str("id"), URL: k.s.IssueURL(issue.str("id"), issue.str("identifier"))}, nil
}

// nextNumber is one more than the project's highest issue number.
func (k *km2Tracker) nextNumber(ctx context.Context, project string) (int, error) {
	r, err := k.first(ctx, "issue", []string{filter("project_key", "eq", project), filter("number", "isnull", "false")}, "number", "desc")
	if err != nil || r == nil {
		return 1, err
	}
	n, _ := strconv.Atoi(r.str("number"))
	return n + 1, nil
}

// CreateIssue creates <PROJECT>-<n> in Backlog with the next free number. A conflict (409 from KM2 2.6.2 on: the
// number was taken, or another unique field clashed) tries the next number after a short wait. A 500 (how KM2 before
// 2.6.2 answers a unique violation, or a failure after the record was written) first looks for an issue the
// connection just made in the project with the same title, and returns it rather than making a second one.
func (k *km2Tracker) CreateIssue(ctx context.Context, in NewIssue) (Issue, error) {
	project, err := k.Project(ctx, in.Project)
	if err != nil {
		return Issue{}, err
	}
	me, err := k.Myself(ctx)
	if err != nil {
		return Issue{}, err
	}
	payload := map[string]any{"project_key": project.Key, "title": in.Title, "status": "Backlog", "status_category": model.StatusToDo,
		"project": project.ID, "creator": me.AccountID}
	if in.Description != "" {
		payload["description"] = in.Description
	}
	if in.Parent != "" {
		parent, err := k.issueRecord(ctx, in.Parent)
		if err != nil {
			return Issue{}, err
		}
		payload["parent"], payload["parent_key"] = parent.str("id"), parent.str("identifier")
	}
	if !slices.Contains(km2BaseColumns, k.s.ActivityField) {
		payload[k.s.ActivityField] = time.Now().UTC().Format(km2TimeLayout)
	}
	started := time.Now()
	conflicts, retried500 := 0, false
	for {
		n, err := k.nextNumber(ctx, project.Key)
		if err != nil {
			return Issue{}, err
		}
		payload["number"], payload["identifier"] = n, fmt.Sprintf("%s-%d", project.Key, n)
		var created record
		err = k.call(ctx, http.MethodPost, k.recordsPath("issue"), nil, payload, &created)
		var te *Error
		switch {
		case err == nil:
			return k.issue(ctx, created, false)
		case !asError(err, &te):
			return Issue{}, err
		case te.Status == http.StatusConflict:
			if conflicts++; conflicts >= km2CreateTries {
				return Issue{}, errorf("%s could not create the issue: KM2 reported a conflict with an existing record %d times (a taken "+
					"issue key or another unique field); try again", k.name, conflicts)
			}
			if err := k.h.sleep(ctx, km2ConflictBackoff<<(conflicts-1)); err != nil {
				return Issue{}, unreachable("could not reach %s: %s", k.name, netProblem(err))
			}
		case te.Status == http.StatusInternalServerError:
			made, findErr := k.recentlyCreated(ctx, project.Key, in.Title, me.AccountID, started)
			if findErr != nil || made != nil {
				return k.made(ctx, made, findErr)
			}
			if retried500 {
				return Issue{}, errorf("%s could not create the issue (HTTP 500 twice); KM2 failed, or the number was taken by another writer. Try again later", k.name)
			}
			retried500 = true
		default:
			return Issue{}, err
		}
	}
}

func (k *km2Tracker) made(ctx context.Context, r record, err error) (Issue, error) {
	if err != nil {
		return Issue{}, err
	}
	return k.issue(ctx, r, false)
}

// recentlyCreated finds an issue in the project with this title, created by the connection's person since a little
// before the create started (a failed answer may follow a write that went through), or nil.
func (k *km2Tracker) recentlyCreated(ctx context.Context, project, title, creator string, started time.Time) (record, error) {
	after := started.Add(-km2RecentCreate).UTC().Format(km2TimeLayout)
	return k.first(ctx, "issue", []string{filter("project_key", "eq", project), filter("title", "eq", title), filter("creator", "eq", creator),
		filter("created_at", "gte", after)}, "created_at", "desc")
}

func (k *km2Tracker) Users(ctx context.Context, max int) ([]Person, error) {
	recs, err := k.list(ctx, "person", nil, "created_at", "asc", MaxPeopleSync)
	if err != nil {
		return nil, err
	}
	out := []Person{}
	for _, r := range recs {
		active, set := r.bool("active")
		bot, _ := r.bool("is_bot")
		if (set && !active) || bot {
			continue
		}
		out = append(out, personOf(r))
	}
	return out[:min(len(out), max)], nil
}

func (k *km2Tracker) User(ctx context.Context, accountID string) (Person, error) {
	var r record
	if err := k.call(ctx, http.MethodGet, k.recordsPath("person")+"/"+url.PathEscape(accountID), nil, nil, &r); err != nil {
		if IsNotFound(err) {
			return Person{}, &Error{Message: "no such person", NotFound: true}
		}
		return Person{}, err
	}
	return personOf(r), nil
}

func asError(err error, target **Error) bool { return errors.As(err, target) }
