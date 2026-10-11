package watch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/tracker"
)

var (
	t0  = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	sam = &tracker.Person{AccountID: "acc-sam", Name: "Sam"}
)

func TestBuildRollups(t *testing.T) {
	parent := tracker.Issue{Key: "P-1", Title: "Parent", DueDate: "2026-11-01", StatusCategory: model.StatusInProgress}
	children := []tracker.Issue{
		{Key: "P-2", DueDate: "2026-10-01", StatusCategory: model.StatusToDo, Assignee: sam, UpdatedAt: t0},
		{Key: "P-3", DueDate: "2026-12-01", StatusCategory: model.StatusInProgress, Status: "Blocked", Assignee: sam, UpdatedAt: t0.AddDate(0, 0, -20)},
		{Key: "P-4", StatusCategory: model.StatusDone, DueDate: "2026-01-01"},
		{Key: "P-5", StatusCategory: model.StatusCanceled},
		{Key: "P-6", StatusCategory: model.StatusToDo, UpdatedAt: t0},
	}
	groups := []Group{{Ref: "jira:P-1", Kind: "jira", Project: "Atlas", Parent: parent, Children: children},
		{Ref: "jira:Q-1", Kind: "jira", Project: "Birch", Parent: tracker.Issue{Key: "Q-1"}}}
	prevAt := t0.Add(-time.Hour)
	quiet := Changes{PreviousCheckAt: &prevAt}
	r := BuildRollups(groups, quiet, nil, nil, 0, t0, time.UTC)
	got := r.Items[0]
	if got.Children != (ChildCounts{Total: 5, Done: 1, InProgress: 1, ToDo: 2, Canceled: 1}) || strings.Join(got.Overdue, ",") != "P-2" ||
		len(got.DueAfterParent) != 1 || got.DueAfterParent[0].ID != "P-3" || strings.Join(got.Blocked, ",") != "P-3" ||
		strings.Join(got.Unassigned, ",") != "P-6" || len(got.Stale) != 1 || got.Stale[0].ID != "P-3" || got.ChangedSinceLastCheck || r.StaleDays != 14 {
		t.Errorf("rollup = %+v", got)
	}
	if in := r.Changed(); len(in.Items) != 0 || in.UnchangedItems != 2 || in.HowToUse == "" {
		t.Errorf("quiet input = %+v", in)
	}

	cases := map[string]Changes{
		"changed child": {PreviousCheckAt: &prevAt, Changed: []ChangedItem{{ID: "jira:P-3"}}},
		"added parent":  {PreviousCheckAt: &prevAt, Added: []AddedItem{{ID: "jira:P-1"}}},
		"comment":       {PreviousCheckAt: &prevAt},
		"removed child": {PreviousCheckAt: &prevAt, Removed: []RemovedItem{{ID: "jira:P-9"}}},
		"first check":   {},
	}
	prev := map[string]ItemMeta{"jira:P-9": {TrackedBy: []string{"jira:P-1"}}}
	for name, ch := range cases {
		var counts map[string]int
		if name == "comment" {
			counts = map[string]int{"jira:P-2": 2} // counted before the per-check cap
		}
		r := BuildRollups(groups, ch, counts, prev, 7, t0, time.UTC)
		if !r.Items[0].ChangedSinceLastCheck {
			t.Errorf("%s: not marked changed", name)
		}
		if name == "comment" && r.Items[0].NewComments != 2 {
			t.Errorf("comments = %d", r.Items[0].NewComments)
		}
		if name != "first check" && r.Items[1].ChangedSinceLastCheck {
			t.Errorf("%s: the other tracked item changed too", name)
		}
	}
}

func TestMetaRoundTrip(t *testing.T) {
	seen := t0
	items := []Item{{ID: "a", Fields: map[string]any{"status": "Open"}, Meta: &ItemMeta{CommentsSeenAt: &seen, TrackedBy: []string{"jira:P-1"},
		Owner: &NotifyPerson{AccountID: "x"}}}, {ID: "b", Fields: map[string]any{"status": "Done"}, Meta: &ItemMeta{Owner: &NotifyPerson{}}}}
	snap := SnapshotOf(items)
	if _, has := snap["b"][MetaKey]; has {
		t.Error("an item with nothing to keep stored meta")
	}
	if _, has := items[0].Fields[MetaKey]; has {
		t.Error("SnapshotOf changed the item's fields")
	}
	fields, meta := splitMeta(snap["a"])
	if _, has := fields[MetaKey]; has || meta == nil || !meta.CommentsSeenAt.Equal(seen) || meta.TrackedBy[0] != "jira:P-1" || meta.Owner != nil {
		t.Errorf("split = %v %+v", fields, meta)
	}
	if f, m := splitMeta(map[string]any{"x": 1}); m != nil || f["x"] != 1 {
		t.Error("no meta")
	}
	if _, m := splitMeta(map[string]any{MetaKey: "garbage"}); m != nil {
		t.Error("bad meta read")
	}
}

type commentClient struct {
	tracker.Client
	comments []tracker.Comment
	calls    int
	err      error
}

func (c *commentClient) Kind() string { return model.TrackerJira }

func (c *commentClient) Comments(context.Context, string, int) ([]tracker.Comment, error) {
	c.calls++
	return c.comments, c.err
}

func TestCommentReader(t *testing.T) {
	prev := t0.Add(-time.Hour)
	client := &commentClient{comments: []tracker.Comment{
		{Author: tracker.Person{AccountID: "me"}, At: t0.Add(-time.Minute), Body: "own"},
		{Author: *sam, At: t0.Add(-2 * time.Minute), Body: "  new\n\ncomment "},
		{Author: tracker.Person{}, At: t0.Add(-3 * time.Minute), Body: "bot"},
		{Author: *sam, At: prev.Add(-time.Minute), Body: "old"},
	}}
	r := &commentReader{client: client, me: tracker.Person{AccountID: "me"}, kind: model.TrackerJira, now: t0,
		previous: map[string]ItemMeta{"jira:A-1": {CommentsSeenAt: &prev}}}
	got, seen, err := r.read(context.Background(), tracker.Issue{Key: "A-1", UpdatedAt: t0})
	if err != nil || len(got) != 2 || got[1].Excerpt != "new comment" || got[0].Author != "(an app or integration)" || !seen.Equal(t0.Add(-time.Minute)) {
		t.Fatalf("got %+v seen %v err %v", got, seen, err)
	}
	// Not updated since: no call
	if got, seen, _ := r.read(context.Background(), tracker.Issue{Key: "A-1", UpdatedAt: prev}); len(got) != 0 || !seen.Equal(prev) || client.calls != 1 {
		t.Errorf("unchanged issue read comments: %v %v calls %d", got, seen, client.calls)
	}
	// New to the watch: starts at the check's time
	if got, seen, _ := r.read(context.Background(), tracker.Issue{Key: "A-2", UpdatedAt: t0}); len(got) != 0 || !seen.Equal(t0) {
		t.Errorf("new issue = %v %v", got, seen)
	}
	// Comments read with the issue are used as they are
	inline := tracker.Issue{Key: "A-1", CommentsRead: true, Comments: []tracker.Comment{{Author: *sam, At: t0, Body: "inline"}}}
	if got, _, _ := r.read(context.Background(), inline); len(got) != 1 || got[0].Excerpt != "inline" || client.calls != 1 {
		t.Errorf("inline = %+v", got)
	}
	client.err = errors.New("boom")
	if _, _, err := r.read(context.Background(), tracker.Issue{Key: "A-1", UpdatedAt: t0}); err == nil {
		t.Error("error swallowed")
	}
	r.reads = maxCommentReads
	if _, seen, err := r.read(context.Background(), tracker.Issue{Key: "A-1", UpdatedAt: t0}); err != nil || !seen.Equal(prev) {
		t.Errorf("past the read cap the seen time must not move: %v %v", seen, err)
	}
	if s := commentsSince(map[string]ItemMeta{"a": {CommentsSeenAt: &t0}, "b": {CommentsSeenAt: &prev}, "c": {}}); s == nil || !s.Equal(prev) {
		t.Errorf("since = %v", s)
	}
}

func TestCommentReaderReadsTrackersWhoseIssuesKeepTheirTime(t *testing.T) {
	prev := t0.Add(-time.Hour)
	for _, kind := range []string{model.TrackerLinear, model.TrackerKM2} {
		client := &commentClient{comments: []tracker.Comment{{Author: *sam, At: t0, Body: "new"}}}
		r := &commentReader{client: client, kind: kind, now: t0, previous: map[string]ItemMeta{kind + ":A-1": {CommentsSeenAt: &prev}}}
		got, _, err := r.read(context.Background(), tracker.Issue{Key: "A-1", UpdatedAt: prev})
		if err != nil || len(got) != 1 || client.calls != 1 {
			t.Errorf("%s: an issue whose time did not move was not read: %+v %v", kind, got, err)
		}
	}
}

func TestCommentsWithoutATimeDoNotMoveTheSeenTime(t *testing.T) {
	prev := t0.Add(-time.Hour)
	r := &commentReader{kind: model.TrackerKM2, now: t0, previous: map[string]ItemMeta{"km2_tracker:A-1": {CommentsSeenAt: &prev}}}
	issue := tracker.Issue{Key: "A-1", CommentsRead: true, Comments: []tracker.Comment{{Author: *sam, Body: "no time"}, {Author: *sam, At: t0.Add(-time.Minute), Body: "ok"}}}
	got, seen, err := r.read(context.Background(), issue)
	if err != nil || len(got) != 1 || got[0].Excerpt != "ok" || !seen.Equal(t0.Add(-time.Minute)) {
		t.Errorf("got %+v seen %v err %v", got, seen, err)
	}
	issue.Comments = issue.Comments[:1]
	if got, seen, _ := r.read(context.Background(), issue); len(got) != 0 || !seen.Equal(prev) {
		t.Errorf("only a comment without a time: %+v seen %v", got, seen)
	}
}

func TestCapCommentsAndExcerpts(t *testing.T) {
	var all []Comment
	for i := 0; i < MaxCommentsPerTick+5; i++ {
		all = append(all, Comment{ID: "x", At: t0.Add(time.Duration(i) * time.Second)})
	}
	kept, omitted := capComments(all)
	if len(kept) != MaxCommentsPerTick || omitted != 5 || !kept[0].At.Equal(t0.Add(5*time.Second)) {
		t.Errorf("kept %d omitted %d first %v", len(kept), omitted, kept[0].At)
	}
	long := excerpt("jira:A-1", tracker.Comment{Author: *sam, Body: strings.Repeat("é", ExcerptChars+10)})
	if !long.Truncated || len([]rune(long.Excerpt)) != ExcerptChars+1 {
		t.Errorf("excerpt = %d runes", len([]rune(long.Excerpt)))
	}
}

func TestTrackerFieldsAndNotify(t *testing.T) {
	f := TrackerFields(tracker.Issue{Title: "T", Status: "Open", Assignee: sam, DueDate: "2026-10-10", Parent: "P-1",
		UpdatedAt: t0, Labels: []string{"b", "a"}})
	if f["assignee"] != "Sam" || f["due_date"] != "2026-10-10" || f["parent"] != "P-1" || f["updated_at"] != "2026-10-05T12:00:00Z" ||
		f["labels"].([]any)[0] != "a" {
		t.Errorf("fields = %v", f)
	}
	if e := TrackerFields(tracker.Issue{}); e["assignee"] != nil || e["due_date"] != nil || e["updated_at"] != nil {
		t.Errorf("empty fields = %v", e)
	}
	m := &ItemMeta{Owner: &NotifyPerson{AccountID: "o"}, Lead: &NotifyPerson{AccountID: "l"}}
	if notifyPerson(m, "Owner").AccountID != "o" || notifyPerson(m, "lead").AccountID != "l" || notifyPerson(m, "assignee").AccountID != "o" ||
		notifyPerson(m, "manager") != nil || notifyPerson(nil, "owner") != nil {
		t.Error("notify person")
	}
	if trackerLabel(model.WatchSource{Project: "P", JQL: "labels = x", IncludeDone: true}) != "P (labels = x)" {
		t.Error("label")
	}
	people := map[string]model.TrackerPerson{"acc-sam": {DisplayName: "Samuel", UserName: "sam"}}
	if p := person(&tracker.Person{AccountID: "acc-sam"}, people); p.Name != "Samuel" || p.StudioUser != "sam" {
		t.Errorf("person = %+v", p)
	}
	if firstPerson(nil, personByID("x", nil)).AccountID != "x" || person(nil, nil) != nil {
		t.Error("first person")
	}
}

func TestTrackerErrors(t *testing.T) {
	if err := trackerError("J", &tracker.LimitError{Count: 5, Max: 3}, 3); !errors.As(err, new(*LimitError)) {
		t.Errorf("limit = %v", err)
	}
	if err := trackerError("J", &tracker.Error{Message: "J refused"}, 3); err.Error() != "J refused" || !errors.As(err, new(*SourceError)) {
		t.Errorf("tracker = %v", err)
	}
	if err := trackerError("J", context.DeadlineExceeded, 3); !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("timeout = %v", err)
	}
	other := errors.New("x")
	if trackerError("J", other, 3) != other {
		t.Error("other errors pass through")
	}
	if _, err := (TrackerSource{}).Fetch(context.Background(), Request{}); err == nil {
		t.Error("no connection")
	}
}

func TestWithCommentsCountsBeforeTheCap(t *testing.T) {
	var all []Comment
	for i := 0; i < MaxCommentsPerTick; i++ {
		all = append(all, Comment{ID: "jira:A-1", At: t0.Add(time.Duration(i+10) * time.Second)})
	}
	all = append(all, Comment{ID: "jira:B-1", At: t0}) // the oldest: left out by the cap, still counted
	f := withComments(Fetched{}, all)
	if len(f.Comments) != MaxCommentsPerTick || f.CommentsOmitted != 1 || f.CommentCounts["jira:B-1"] != 1 || f.CommentCounts["jira:A-1"] != MaxCommentsPerTick {
		t.Errorf("fetched = %d comments, %d omitted, counts %v", len(f.Comments), f.CommentsOmitted, f.CommentCounts)
	}
}

func TestSeenMoved(t *testing.T) {
	later := t0.Add(time.Minute)
	prev := map[string]ItemMeta{"a": {CommentsSeenAt: &t0}}
	if seenMoved([]Item{{ID: "a", Meta: &ItemMeta{CommentsSeenAt: &t0}}, {ID: "b"}}, prev) {
		t.Error("nothing moved")
	}
	if !seenMoved([]Item{{ID: "a", Meta: &ItemMeta{CommentsSeenAt: &later}}}, prev) || !seenMoved([]Item{{ID: "c", Meta: &ItemMeta{CommentsSeenAt: &t0}}}, prev) {
		t.Error("a moved or new seen time")
	}
}
