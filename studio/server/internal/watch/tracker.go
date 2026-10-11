package watch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

// Tracker sources (design sections 5 and 12): a Jira project, Linear team or KM2 project, and the crew's tracking
// list (every tracked item with its children). Both can also report new comments. Items are identified by their subject, such
// as jira:PRJ-15, which is also what follow-ups are logged under.

const trackerFetchTimeout = 2 * time.Minute

// TrackerSource reads a Jira project or Linear team through the watch's connection.
type TrackerSource struct {
	Trackers *tracker.Service
	Store    *store.Store
}

// TrackingListSource reads every item on the crew's tracking list with its children.
type TrackingListSource struct {
	Trackers *tracker.Service
	Store    *store.Store
}

// trackerError keeps tracker problems readable (they are safe to show) and passes limits on as limits.
func trackerError(conn string, err error, max int) error {
	var le *tracker.LimitError
	if errors.As(err, &le) {
		return &LimitError{Count: le.Count, Max: max}
	}
	var te *tracker.Error
	if errors.As(err, &te) {
		return sourceErrorf("%s", te.Message)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return sourceErrorf("%s did not answer in time", conn)
	}
	return err
}

func (t TrackerSource) Fetch(ctx context.Context, req Request) (Fetched, error) {
	if req.Connection == nil {
		return Fetched{}, sourceErrorf("this watch needs a tracker connection (Jira, Linear or KM2 issues)")
	}
	ctx, cancel := context.WithTimeout(ctx, trackerFetchTimeout)
	defer cancel()
	conn, src := *req.Connection, req.Def.Source
	client, err := t.Trackers.WithSecrets(conn, req.Secrets)
	if err != nil {
		return Fetched{}, trackerError(conn.Name, err, req.Def.MaxItems)
	}
	q := tracker.Query{Project: src.Project, JQL: src.JQL, IncludeDone: src.IncludeDone, Max: req.Def.MaxItems}
	if src.Comments {
		q.CommentsSince = commentsSince(req.Previous)
	}
	issues, err := client.Search(ctx, q)
	if err != nil {
		return Fetched{}, trackerError(conn.Name, err, req.Def.MaxItems)
	}
	people, err := t.peopleOf(ctx, conn.ID)
	if err != nil {
		return Fetched{}, err
	}
	lead, err := projectLead(ctx, client, src.Project, src.Lead, people)
	if err != nil {
		return Fetched{}, trackerError(conn.Name, err, req.Def.MaxItems)
	}
	b := itemBuilder{kind: conn.Kind, fields: req.Def.TrackedFields, people: people}
	items := make([]Item, 0, len(issues))
	for _, issue := range issues {
		items = append(items, b.item(issue, firstPerson(person(issue.Lead, people), lead), ""))
	}
	out := Fetched{Label: fmt.Sprintf("%s: %s", conn.Name, trackerLabel(src)), Items: items}
	if src.Comments {
		r := &commentReader{client: client, kind: conn.Kind, previous: req.Previous, now: req.Now}
		all, err := readComments(ctx, t.Trackers, conn.ID, r, issues, items)
		if err != nil {
			return Fetched{}, trackerError(conn.Name, err, req.Def.MaxItems)
		}
		out = withComments(out, all)
	}
	return out, checkUniqueIDs(items)
}

func trackerLabel(src model.WatchSource) string {
	label := src.Project
	if src.JQL != "" {
		label += " (" + src.JQL + ")"
	}
	if !src.IncludeDone {
		label += ", open issues"
	}
	return label
}

// withComments adds new comments to a fetch: the newest MaxCommentsPerTick for the run, and how many each item got
// (counted before the cap, so roll-ups see every item that was commented on).
func withComments(f Fetched, all []Comment) Fetched {
	out := f
	out.CommentCounts = map[string]int{}
	for _, c := range all {
		out.CommentCounts[c.ID]++
	}
	out.Comments, out.CommentsOmitted = capComments(all)
	return out
}

// readComments reads every issue's new comments, storing each issue's new seen time in its item's meta.
func readComments(ctx context.Context, ts *tracker.Service, connID string, r *commentReader, issues []tracker.Issue, items []Item) ([]Comment, error) {
	me, err := ts.Myself(ctx, connID, r.client)
	if err != nil {
		return nil, err
	}
	r.me = me
	byID := make(map[string]int, len(items))
	for i, it := range items {
		byID[it.ID] = i
	}
	var all []Comment
	for _, issue := range issues {
		found, seen, err := r.read(ctx, issue)
		if err != nil {
			return nil, err
		}
		if i, ok := byID[model.TrackerSubject(r.kind, issue.Key)]; ok {
			items[i].Meta.CommentsSeenAt = seen
		}
		all = append(all, found...)
	}
	return all, nil
}

func (t TrackingListSource) Fetch(ctx context.Context, req Request) (Fetched, error) {
	ctx, cancel := context.WithTimeout(ctx, trackerFetchTimeout)
	defer cancel()
	sched := req.ScheduleID
	tracked, err := t.Store.ListTrackingItems(ctx, store.TrackingFilter{CrewID: req.CrewID, ScheduleID: &sched})
	if err != nil {
		return Fetched{}, err
	}
	l := listFetch{source: t, req: req, byID: map[string]int{}, itemConn: map[string]string{}, groups: map[string]bool{}, out: Fetched{Label: fmt.Sprintf("tracking list (%d items)", len(tracked)),
		Items: []Item{}, Groups: []Group{}}, clients: map[string]connClient{}}
	for _, ti := range tracked {
		if err := l.add(ctx, ti); err != nil {
			return Fetched{}, err
		}
	}
	if req.Def.Source.Comments {
		if err := l.comments(ctx); err != nil {
			return Fetched{}, err
		}
	}
	return l.out, nil
}

type connClient struct {
	client tracker.Client
	people map[string]model.TrackerPerson
	leads  map[string]*NotifyPerson
}

// listFetch gathers the tracking list's items, one tracked item at a time.
type listFetch struct {
	source TrackingListSource
	req    Request
	out    Fetched
	byID   map[string]int
	// itemConn is the connection each item came through, to catch two connections giving the same subject
	itemConn map[string]string
	// groups are the tracked items already read, by connection and subject (an item tracked crew-wide and for the
	// schedule is read once)
	groups  map[string]bool
	issues  map[string][]tracker.Issue // by connection id, for comments
	clients map[string]connClient
}

func (l *listFetch) client(ctx context.Context, ti model.TrackingItem) (connClient, error) {
	if c, ok := l.clients[ti.ConnectionID]; ok {
		return c, nil
	}
	_, client, err := l.source.Trackers.ForCrew(ctx, ti.ConnectionID, l.req.CrewID)
	if err != nil {
		return connClient{}, sourceErrorf("%s (%s): %s", ti.ItemRef, ti.Project, tracker.SafeMessage(err))
	}
	people, err := TrackingPeople(ctx, l.source.Store, ti.ConnectionID)
	if err != nil {
		return connClient{}, err
	}
	c := connClient{client: client, people: people, leads: map[string]*NotifyPerson{}}
	l.clients[ti.ConnectionID] = c
	return c, nil
}

func (l *listFetch) add(ctx context.Context, ti model.TrackingItem) error {
	c, err := l.client(ctx, ti)
	if err != nil {
		return err
	}
	parent, err := c.client.Issue(ctx, ti.ItemRef)
	if tracker.IsNotFound(err) {
		return sourceErrorf("%s (%s) no longer exists, or %s cannot see it; remove it from the tracking list", ti.ItemRef, ti.Project, ti.ConnectionName)
	}
	if err != nil {
		return trackerError(ti.ConnectionName, err, l.req.Def.MaxItems)
	}
	var children []tracker.Issue
	if ti.IncludeChildren != model.IncludeChildrenNone {
		depth := 0
		if ti.IncludeChildren == model.IncludeChildrenDirect {
			depth = 1
		}
		q := tracker.Query{}
		if l.req.Def.Source.Comments {
			q.CommentsSince = commentsSince(l.req.Previous)
		}
		if children, err = tracker.Children(ctx, c.client, parent, depth, q, l.req.Def.MaxItems); err != nil {
			return trackerError(ti.ConnectionName, err, l.req.Def.MaxItems)
		}
	}
	lead, err := l.lead(ctx, c, ti, parent)
	if err != nil {
		return trackerError(ti.ConnectionName, err, l.req.Def.MaxItems)
	}
	ref := model.TrackerSubject(ti.ConnectionKind, parent.Key)
	if l.groups[ti.ConnectionID+" "+ref] {
		return nil
	}
	l.groups[ti.ConnectionID+" "+ref] = true
	l.out.Groups = append(l.out.Groups, Group{Ref: ref, Project: ti.Project, Connection: ti.ConnectionName, Note: ti.Note,
		Lead: lead, Parent: parent, Children: children, Kind: ti.ConnectionKind})
	b := itemBuilder{kind: ti.ConnectionKind, fields: l.req.Def.TrackedFields, people: c.people}
	for _, issue := range append([]tracker.Issue{parent}, children...) {
		if err := l.addItem(ti.ConnectionID, b.item(issue, lead, ref), issue); err != nil {
			return err
		}
	}
	if len(l.out.Items) > l.req.Def.MaxItems {
		return &LimitError{Count: len(l.out.Items), Max: l.req.Def.MaxItems}
	}
	return nil
}

// addItem keeps the first copy of an item that sits under two tracked items, noting both. The same subject from two
// connections (two Jira sites with the same keys) cannot be told apart, so the check fails instead.
func (l *listFetch) addItem(connID string, it Item, issue tracker.Issue) error {
	if i, ok := l.byID[it.ID]; ok {
		if l.itemConn[it.ID] != connID {
			return sourceErrorf("%s comes from two connections; track each key through one connection only", it.ID)
		}
		if !slices.Contains(l.out.Items[i].Meta.TrackedBy, it.Meta.TrackedBy[0]) {
			l.out.Items[i].Meta.TrackedBy = append(l.out.Items[i].Meta.TrackedBy, it.Meta.TrackedBy[0])
		}
		return nil
	}
	l.itemConn[it.ID] = connID
	l.byID[it.ID] = len(l.out.Items)
	l.out.Items = append(l.out.Items, it)
	if l.issues == nil {
		l.issues = map[string][]tracker.Issue{}
	}
	l.issues[connID] = append(l.issues[connID], issue)
	return nil
}

// lead is the tracked item's lead override, or its project's lead.
func (l *listFetch) lead(ctx context.Context, c connClient, ti model.TrackingItem, parent tracker.Issue) (*NotifyPerson, error) {
	if ti.LeadAccountID != "" {
		return personByID(ti.LeadAccountID, c.people), nil
	}
	if p := person(parent.Lead, c.people); p != nil {
		return p, nil
	}
	if lead, ok := c.leads[parent.Project]; ok {
		return lead, nil
	}
	lead, err := projectLead(ctx, c.client, parent.Project, "", c.people)
	c.leads[parent.Project] = lead
	return lead, err
}

func (l *listFetch) comments(ctx context.Context) error {
	var all []Comment
	for connID, issues := range l.issues {
		c := l.clients[connID]
		r := &commentReader{client: c.client, kind: c.client.Kind(), previous: l.req.Previous, now: l.req.Now}
		found, err := readComments(ctx, l.source.Trackers, connID, r, issues, l.out.Items)
		if err != nil {
			return trackerError("the tracker", err, l.req.Def.MaxItems)
		}
		all = append(all, found...)
	}
	l.out = withComments(l.out, all)
	return nil
}

// projectLead is the lead override (an account id) or the project's lead from the tracker (nil when it has none,
// as Linear teams do).
func projectLead(ctx context.Context, c tracker.Client, project, override string, people map[string]model.TrackerPerson) (*NotifyPerson, error) {
	if override != "" {
		return personByID(override, people), nil
	}
	if !model.TrackerHasProjectLeads(c.Kind()) || project == "" {
		return nil, nil
	}
	p, err := c.Project(ctx, project)
	if tracker.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return person(p.Lead, people), nil
}

// TrackingPeople is a connection's people map by account id.
func TrackingPeople(ctx context.Context, st *store.Store, connID string) (map[string]model.TrackerPerson, error) {
	list, err := st.ListTrackerPeople(ctx, connID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]model.TrackerPerson, len(list))
	for _, p := range list {
		out[p.AccountID] = p
	}
	return out, nil
}

func (t TrackerSource) peopleOf(ctx context.Context, connID string) (map[string]model.TrackerPerson, error) {
	return TrackingPeople(ctx, t.Store, connID)
}

func person(p *tracker.Person, people map[string]model.TrackerPerson) *NotifyPerson {
	if p == nil {
		return nil
	}
	out := &NotifyPerson{AccountID: p.AccountID, Name: p.Name}
	if known, ok := people[p.AccountID]; ok {
		out.StudioUser = known.UserName
		if out.Name == "" {
			out.Name = known.DisplayName
		}
	}
	return out
}

func personByID(id string, people map[string]model.TrackerPerson) *NotifyPerson {
	known := people[id]
	return &NotifyPerson{AccountID: id, Name: known.DisplayName, StudioUser: known.UserName}
}

func firstPerson(a, b *NotifyPerson) *NotifyPerson {
	if a != nil {
		return a
	}
	return b
}

// itemBuilder turns tracker issues into watch items with the tracked fields.
type itemBuilder struct {
	kind   string
	fields []string
	people map[string]model.TrackerPerson
}

func (b itemBuilder) item(issue tracker.Issue, lead *NotifyPerson, trackedBy string) Item {
	all := TrackerFields(issue)
	fields := make(map[string]any, len(b.fields))
	for _, f := range b.fields {
		fields[f] = normalizeValue(all[f])
	}
	meta := &ItemMeta{Owner: person(issue.Assignee, b.people), Lead: lead}
	if trackedBy != "" {
		meta.TrackedBy = []string{trackedBy}
	}
	return Item{ID: model.TrackerSubject(b.kind, issue.Key), Fields: fields, Meta: meta}
}

// TrackerFields are an issue's trackable fields as watch values (nil when empty).
func TrackerFields(i tracker.Issue) map[string]any {
	var assignee, due, parent, updated any
	if i.Assignee != nil {
		assignee = i.Assignee.Name
	}
	if i.DueDate != "" {
		due = i.DueDate
	}
	if i.Parent != "" {
		parent = i.Parent
	}
	if !i.UpdatedAt.IsZero() {
		updated = i.UpdatedAt.UTC().Format(time.RFC3339)
	}
	labels := slices.Clone(i.Labels)
	slices.Sort(labels)
	list := make([]any, len(labels))
	for n, l := range labels {
		list[n] = l
	}
	return map[string]any{"title": i.Title, "status": i.Status, "status_category": i.StatusCategory, "assignee": assignee,
		"due_date": due, "priority": i.Priority, "updated_at": updated, "parent": parent, "labels": list}
}
