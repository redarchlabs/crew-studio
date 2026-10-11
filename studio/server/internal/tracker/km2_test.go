package tracker_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/tracker"
	"crewstudio/internal/tracker/trackertest"
)

const km2Key = "km2_tracker-secret-789"

// km2Client is a km2_tracker client on a fake KM2 organisation; config adds or replaces settings.
func km2Client(t *testing.T, k *trackertest.KM2, config map[string]string) (tracker.Client, *httptest.Server) {
	t.Helper()
	srv := trackertest.Server(t, k.Handler())
	settings := map[string]string{"base_url": srv.URL, "self_email": "bot@example.com", "allow_create": "true"}
	for key, v := range config {
		settings[key] = v
	}
	c, err := tracker.New(model.Connection{Name: "Acme KM2", Kind: model.TrackerKM2, Config: settings}, map[string]string{"api_key": km2Key},
		tracker.Options{HTTP: srv.Client(), Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestKM2Specifics(t *testing.T) {
	ctx := context.Background()
	d := seed()
	k := trackertest.NewKM2(d, km2Key)
	c, srv := km2Client(t, k, map[string]string{"issue_url_template": "https://km2.example.com/records/trk_issue?record_id={id}"})
	if c.Kind() != model.TrackerKM2 {
		t.Errorf("kind = %s", c.Kind())
	}
	users, err := c.Users(ctx, 10)
	if err != nil || slices.ContainsFunc(users, func(p tracker.Person) bool { return p.AccountID == "me-1" }) {
		t.Errorf("the bot person is in the people list: %+v %v", users, err)
	}
	project, err := c.Project(ctx, "PRJ")
	if err != nil || project.Lead == nil || project.Lead.AccountID != "acc-dana" || project.Lead.Name != "Dana Ruiz" || project.Name != "Project PRJ" {
		t.Errorf("project = %+v %v", project, err)
	}
	issue, err := c.Issue(ctx, "PRJ-11")
	if err != nil || issue.URL != "https://km2.example.com/records/trk_issue?record_id="+issue.ID || issue.Assignee.Name != "Sam Lee" ||
		issue.Assignee.Email != "sam@example.com" || !issue.UpdatedAt.Equal(k.RealUpdated) || issue.Labels[0] != "blocked" {
		t.Errorf("issue = %+v %v", issue, err)
	}
	loose, _ := c.Issue(ctx, "PRJ-14")
	if loose.StatusCategory != model.StatusCanceled || !loose.Done() {
		t.Errorf("won't do = %+v", loose)
	}

	since := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	found, err := c.Search(ctx, tracker.Query{Project: "PRJ", CommentsSince: &since})
	if err != nil || len(found) != 3 {
		t.Fatalf("found = %d %v", len(found), err)
	}
	for _, i := range found {
		if !i.CommentsRead {
			t.Errorf("%s has no comments read", i.Key)
		}
		if i.Key == "PRJ-11" && (len(i.Comments) != 1 || i.Comments[0].Body != "Any news?" || i.Comments[0].Author.Name != "Dana Ruiz" ||
			!i.Comments[0].At.Equal(time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC))) {
			t.Errorf("inline comments = %+v", i.Comments)
		}
	}
	parent, _ := c.Issue(ctx, "PRJ-10")
	children, err := c.Search(ctx, tracker.Query{Parent: &parent, IncludeDone: true, CommentsSince: &since})
	if err != nil || len(children) != 2 || !children[0].CommentsRead {
		t.Errorf("children = %+v %v", children, err)
	}
	if _, err := c.Search(ctx, tracker.Query{Project: "PRJ", JQL: "x"}); err == nil || !strings.Contains(err.Error(), "only applies to Jira") {
		t.Errorf("jql err = %v", err)
	}

	// A comment names the people it mentions in its text and lists their ids; it is posted as the bot person
	posted, err := c.PostComment(ctx, "PRJ-11", "Please update", []tracker.Person{{AccountID: "acc-sam"}})
	if err != nil || posted.ID == "" || !strings.HasPrefix(posted.URL, "https://km2.example.com/records/trk_issue?record_id=") {
		t.Fatalf("posted = %+v %v", posted, err)
	}
	body := d.PostedComments()[0].Body.(map[string]any)
	if body["body"] != "@Sam Lee\n\nPlease update" || body["mentions"] != "acc-sam" || body["author"] != "me-1" || body["author_name"] != "Studio Bot" ||
		body["issue"] != issue.ID || body["issue_key"] != "PRJ-11" || body["project_key"] != "PRJ" {
		t.Errorf("comment = %#v", body)
	}
	if _, err := c.PostComment(ctx, "PRJ-11", "x", []tracker.Person{{AccountID: "nobody"}}); err == nil || !strings.Contains(err.Error(), "nobody") {
		t.Errorf("mention of an unknown person: %v", err)
	}
	if _, err := c.PostComment(ctx, "PRJ-99", "x", nil); !tracker.IsNotFound(err) {
		t.Errorf("comment on a missing issue: %v", err)
	}
	// The bot's own comment is read back with the real time it was made
	newest, err := c.Comments(ctx, "PRJ-11", 1)
	if err != nil || len(newest) != 1 || newest[0].Author.AccountID != "me-1" || time.Since(newest[0].At) > time.Minute {
		t.Errorf("newest = %+v %v", newest, err)
	}

	created, err := c.CreateIssue(ctx, tracker.NewIssue{Project: "PRJ", Title: "New thing", Description: "why", Parent: "PRJ-10"})
	if err != nil || created.Key != "PRJ-15" || created.StatusCategory != model.StatusToDo || created.Project != "PRJ" {
		t.Fatalf("created = %+v %v", created, err)
	}
	fields := d.Created[0]
	if fields["number"] != float64(15) || fields["project"] != "proj-PRJ" || fields["parent"] != parent.ID || fields["parent_key"] != "PRJ-10" ||
		fields["status"] != "Backlog" || fields["status_category"] != "to_do" || fields["creator"] != "me-1" || fields["description"] != "why" {
		t.Errorf("create fields = %v", fields)
	}
	if _, err := c.CreateIssue(ctx, tracker.NewIssue{Project: "ZZZ", Title: "x"}); !tracker.IsNotFound(err) {
		t.Errorf("create in a missing project: %v", err)
	}
	if _, err := c.CreateIssue(ctx, tracker.NewIssue{Project: "PRJ", Title: "x", Parent: "PRJ-99"}); !tracker.IsNotFound(err) {
		t.Errorf("create under a missing parent: %v", err)
	}
	for _, req := range d.RequestLog() {
		if strings.Contains(req, km2Key) || (strings.HasPrefix(req, "PATCH") || strings.HasPrefix(req, "DELETE")) {
			t.Errorf("request %s", req)
		}
	}
	_ = srv
}

func TestKM2CreateIssueRetriesTakenNumbers(t *testing.T) {
	ctx := context.Background()
	d := seed()
	k := trackertest.NewKM2(d, km2Key)
	c, _ := km2Client(t, k, nil)
	// Another writer takes PRJ-15 first (409 from KM2 2.6.2): the next number is used
	k.Collide = 1
	created, err := c.CreateIssue(ctx, tracker.NewIssue{Project: "PRJ", Title: "Race"})
	if err != nil || created.Key != "PRJ-16" {
		t.Fatalf("after a collision: %+v %v", created, err)
	}
	// Before 2.6.2 a unique violation is a 500: tried once more
	k.Fail500 = 1
	created, err = c.CreateIssue(ctx, tracker.NewIssue{Project: "PRJ", Title: "Old KM2"})
	if err != nil || created.Key != "PRJ-17" {
		t.Fatalf("after one 500: %+v %v", created, err)
	}
	k.Fail500 = 2
	if _, err := c.CreateIssue(ctx, tracker.NewIssue{Project: "PRJ", Title: "Broken"}); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("two 500s: %v", err)
	}
	k.Collide = 5
	if _, err := c.CreateIssue(ctx, tracker.NewIssue{Project: "PRJ", Title: "Busy"}); err == nil || !strings.Contains(err.Error(), "conflict with an existing record 3 times") {
		t.Errorf("collisions every time: %v", err)
	}
}

func TestKM2PagesAndUsesTheActivityField(t *testing.T) {
	ctx := context.Background()
	d := seed()
	d.PageSize = 1
	k := trackertest.NewKM2(d, km2Key)
	c, _ := km2Client(t, k, map[string]string{"activity_field": "activity_at"})
	all, err := c.Search(ctx, tracker.Query{Project: "PRJ", IncludeDone: true})
	if err != nil || len(all) != 5 {
		t.Fatalf("all = %d %v", len(all), err)
	}
	if !all[0].UpdatedAt.Equal(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("updated from activity_at = %v", all[0].UpdatedAt)
	}
	cursors := 0
	for _, req := range d.RequestLog() {
		if strings.Contains(req, "cursor=") {
			cursors++
		}
		if strings.Contains(req, "/trk_issue/records?") && !strings.Contains(req, "filter=project_key%3Aeq%3APRJ") {
			t.Errorf("search without the project filter: %s", req)
		}
	}
	if cursors < 4 {
		t.Errorf("paged with %d cursors", cursors)
	}
	open, err := c.Search(ctx, tracker.Query{Project: "PRJ"})
	if err != nil || len(open) != 3 {
		t.Errorf("open = %d %v", len(open), err)
	}
}

func TestKM2ChecksTheOrganisation(t *testing.T) {
	ctx := context.Background()
	d := seed()
	k := trackertest.NewKM2(d, km2Key)
	c, _ := km2Client(t, k, map[string]string{"activity_field": "activity_at"})
	checker, ok := c.(tracker.Checker)
	if !ok {
		t.Fatal("the KM2 client cannot check the organisation")
	}
	if found, err := checker.Check(ctx); err != nil || len(found.Problems) != 0 || len(found.Warnings) != 0 {
		t.Errorf("complete org: %+v %v", found, err)
	}
	k.Hidden["issue"] = []string{"labels", "activity_at"}
	k.Hidden["comment"] = nil
	found, err := checker.Check(ctx)
	problems := found.Problems
	if err != nil || len(problems) != 2 || problems[0] != "trk_issue lacks the fields activity_at, labels" || problems[1] != "there is no trk_comment entity" {
		t.Errorf("problems = %q %v", problems, err)
	}
	// Reading through a missing entity or field says what is wrong with the organisation
	k.Hidden["issue"] = []string{"status_category"}
	if _, err := c.Search(ctx, tracker.Query{Project: "PRJ"}); err == nil || !strings.Contains(err.Error(), "tracker entities") {
		t.Errorf("unknown field err = %v", err)
	}
	k.Hidden["person"] = nil
	if _, err := c.Myself(ctx); err == nil || !strings.Contains(err.Error(), "no trk_person entity") {
		t.Errorf("missing entity err = %v", err)
	}
	k.Hidden = map[string][]string{}
	d.Me.Email = "other@example.com"
	if _, err := c.Myself(ctx); err == nil || !strings.Contains(err.Error(), "bot@example.com") {
		t.Errorf("missing bot person err = %v", err)
	}
}

func TestKM2ErrorsAndSettings(t *testing.T) {
	ctx := context.Background()
	d := seed()
	srv := trackertest.Server(t, trackertest.NewKM2(d, "km2_other").Handler())
	c, err := tracker.New(model.Connection{Name: "K", Kind: model.TrackerKM2, Config: map[string]string{"base_url": srv.URL, "self_email": "bot@example.com"}},
		map[string]string{"api_key": km2Key}, tracker.Options{HTTP: srv.Client(), Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Myself(ctx)
	var te *tracker.Error
	if !errors.As(err, &te) || !te.Auth || strings.Contains(err.Error(), km2Key) {
		t.Errorf("auth err = %v", err)
	}
	// A rate limit is waited out
	k := trackertest.NewKM2(seed(), km2Key)
	kc, _ := km2Client(t, k, nil)
	k.Data.Fail, k.Data.FailTimes = http.StatusTooManyRequests, 1
	if _, err := kc.Issue(ctx, "PRJ-10"); err != nil {
		t.Errorf("after a 429: %v", err)
	}
	if _, err := kc.User(ctx, "nobody"); !tracker.IsNotFound(err) {
		t.Errorf("unknown person err = %v", err)
	}
	// FastAPI's validation answers (a list of problems) are passed on as their messages
	bad := trackertest.Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":[{"loc":["query","limit"],"msg":"Input should be less than or equal to 200"}]}`))
	}))
	bc, _ := tracker.New(model.Connection{Name: "K", Kind: model.TrackerKM2, Config: map[string]string{"base_url": bad.URL, "self_email": "a@b.co"}},
		map[string]string{"api_key": km2Key}, tracker.Options{HTTP: bad.Client(), Sleep: noSleep})
	if _, err := bc.Issue(ctx, "PRJ-1"); err == nil || !strings.Contains(err.Error(), "HTTP 422: Input should be less than or equal to 200") {
		t.Errorf("422 err = %v", err)
	}
	if _, err := bc.User(ctx, "not-a-uuid"); !tracker.IsNotFound(err) {
		t.Errorf("a record id KM2 cannot read is not found: %v", err)
	}
	cases := []struct {
		config  map[string]string
		secrets map[string]string
		want    string
	}{
		{map[string]string{"base_url": "http://km2.example.com", "self_email": "a@b.co"}, map[string]string{"api_key": km2Key}, "https"},
		{map[string]string{"base_url": "https://km2.example.com/x?y=1", "self_email": "a@b.co"}, map[string]string{"api_key": km2Key}, "https"},
		{map[string]string{"base_url": "https://km2.example.com", "self_email": "a@b.co"}, map[string]string{}, "no API key"},
		{map[string]string{"base_url": "https://km2.example.com", "self_email": "a@b.co"}, map[string]string{"api_key": "lin_1"}, "km2_"},
		{map[string]string{"base_url": "https://km2.example.com"}, map[string]string{"api_key": km2Key}, "person email"},
		{map[string]string{"base_url": "https://km2.example.com", "self_email": "a@b.co", "entity_prefix": "Bad-"}, map[string]string{"api_key": km2Key}, "prefix"},
		{map[string]string{"base_url": "https://km2.example.com", "self_email": "a@b.co", "activity_field": "a b"}, map[string]string{"api_key": km2Key}, "activity field"},
	}
	for _, tc := range cases {
		if _, err := tracker.New(model.Connection{Name: "K", Kind: model.TrackerKM2, Config: tc.config}, tc.secrets, tracker.Options{}); err == nil ||
			!strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err = %v, want %q", tc.config, err, tc.want)
		}
	}
}
