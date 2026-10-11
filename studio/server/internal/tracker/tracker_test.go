package tracker_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/tracker"
	"crewstudio/internal/tracker/trackertest"
)

const (
	jiraToken = "jira-secret-token-123"
	linearKey = "lin_api_secret_456"
)

var (
	sam  = trackertest.User{ID: "acc-sam", Name: "Sam Lee", Email: "sam@example.com"}
	dana = trackertest.User{ID: "acc-dana", Name: "Dana Ruiz"}
)

func noSleep(context.Context, time.Duration) error { return nil }

func seed() *trackertest.Data {
	d := trackertest.NewData()
	d.Users = []trackertest.User{sam, dana}
	d.Projects["PRJ"] = &dana
	d.Add(trackertest.Issue{Key: "PRJ-10", Title: "Atlas rollout", Due: "2026-11-15", Category: "indeterminate", Priority: "High"})
	d.Add(trackertest.Issue{Key: "PRJ-11", Title: "Vendor access", Parent: "PRJ-10", Assignee: &sam, Due: "2026-10-01", Labels: []string{"blocked"}})
	d.Add(trackertest.Issue{Key: "PRJ-12", Title: "Docs", Parent: "PRJ-10", Category: "done"})
	d.Add(trackertest.Issue{Key: "PRJ-13", Title: "Sub-task", Parent: "PRJ-11", Due: "2026-12-01"})
	d.Add(trackertest.Issue{Key: "PRJ-14", Title: "Loose", Status: "Won't Do", Category: "done"})
	d.Comment("PRJ-11", trackertest.Comment{Author: sam, At: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), Body: "Blocked on vendor"})
	d.Comment("PRJ-11", trackertest.Comment{Author: dana, At: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC), Body: "Any news?"})
	return d
}

func jiraClient(t *testing.T, d *trackertest.Data) (tracker.Client, *httptest.Server) {
	t.Helper()
	srv := trackertest.Server(t, trackertest.Jira(d, "bot@example.com", jiraToken))
	conn := model.Connection{Name: "Acme Jira", Kind: "jira", Config: map[string]string{"site_url": srv.URL, "email": "bot@example.com", "allow_create": "true"}}
	c, err := tracker.New(conn, map[string]string{"api_token": jiraToken}, tracker.Options{JiraSites: []string{srv.URL}, HTTP: srv.Client(), Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func linearClient(t *testing.T, d *trackertest.Data) tracker.Client {
	t.Helper()
	srv := trackertest.Server(t, trackertest.Linear(d, linearKey))
	c, err := tracker.New(model.Connection{Name: "Acme Linear", Kind: "linear"}, map[string]string{"api_key": linearKey},
		tracker.Options{LinearURL: srv.URL, HTTP: srv.Client(), Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func clients(t *testing.T) map[string]struct {
	c tracker.Client
	d *trackertest.Data
} {
	jd, ld, kd := seed(), seed(), seed()
	jc, _ := jiraClient(t, jd)
	kc, _ := km2Client(t, trackertest.NewKM2(kd, km2Key), nil)
	return map[string]struct {
		c tracker.Client
		d *trackertest.Data
	}{"jira": {jc, jd}, "linear": {linearClient(t, ld), ld}, "km2_tracker": {kc, kd}}
}

func TestReadIssuesBothTrackers(t *testing.T) {
	ctx := context.Background()
	for name, tc := range clients(t) {
		t.Run(name, func(t *testing.T) {
			if tc.c.Kind() != name {
				t.Errorf("kind = %s", tc.c.Kind())
			}
			me, err := tc.c.Myself(ctx)
			if err != nil || me.AccountID != "me-1" {
				t.Fatalf("myself = %+v, %v", me, err)
			}
			issue, err := tc.c.Issue(ctx, "PRJ-11")
			if err != nil {
				t.Fatal(err)
			}
			if issue.Title != "Vendor access" || issue.Parent != "PRJ-10" || issue.Assignee == nil || issue.Assignee.AccountID != "acc-sam" ||
				issue.DueDate != "2026-10-01" || issue.StatusCategory != model.StatusToDo || issue.Project != "PRJ" ||
				issue.UpdatedAt.IsZero() || !strings.Contains(issue.Description, "Vendor access") || len(issue.Labels) != 1 {
				t.Errorf("issue = %+v", issue)
			}
			if _, err := tc.c.Issue(ctx, "PRJ-99"); !tracker.IsNotFound(err) {
				t.Errorf("missing issue err = %v", err)
			}
			open, err := tc.c.Search(ctx, tracker.Query{Project: "PRJ"})
			if err != nil || len(open) != 3 {
				t.Fatalf("open = %d %v", len(open), err)
			}
			all, err := tc.c.Search(ctx, tracker.Query{Project: "PRJ", IncludeDone: true})
			if err != nil || len(all) != 5 {
				t.Fatalf("all = %d %v", len(all), err)
			}
			if _, err := tc.c.Search(ctx, tracker.Query{Project: "PRJ", IncludeDone: true, Max: 3}); !errors.As(err, new(*tracker.LimitError)) {
				t.Errorf("limit err = %v", err)
			}
			parent, _ := tc.c.Issue(ctx, "PRJ-10")
			direct, err := tracker.Children(ctx, tc.c, parent, 1, tracker.Query{}, 100)
			if err != nil || len(direct) != 2 {
				t.Fatalf("direct = %v %v", direct, err)
			}
			deep, err := tracker.Children(ctx, tc.c, parent, 0, tracker.Query{}, 100)
			if err != nil || len(deep) != 3 {
				t.Fatalf("deep = %v %v", deep, err)
			}
			if _, err := tracker.Children(ctx, tc.c, parent, 0, tracker.Query{}, 2); !errors.As(err, new(*tracker.LimitError)) {
				t.Errorf("children limit err = %v", err)
			}
			comments, err := tc.c.Comments(ctx, "PRJ-11", 1)
			if err != nil || len(comments) != 1 || comments[0].Body != "Any news?" || comments[0].Author.AccountID != "acc-dana" {
				t.Errorf("comments = %+v %v", comments, err)
			}
			projects, err := tc.c.Projects(ctx, 10)
			if err != nil || len(projects) != 1 || projects[0].Key != "PRJ" {
				t.Errorf("projects = %+v %v", projects, err)
			}
			if _, err := tc.c.Project(ctx, "PRJ"); err != nil {
				t.Errorf("project err = %v", err)
			}
			if _, err := tc.c.Project(ctx, "NOPE"); !tracker.IsNotFound(err) {
				t.Errorf("missing project err = %v", err)
			}
			users, err := tc.c.Users(ctx, 10)
			if err != nil || len(users) != 2 {
				t.Errorf("users = %+v %v", users, err)
			}
			u, err := tc.c.User(ctx, "acc-sam")
			if err != nil || u.Name != "Sam Lee" {
				t.Errorf("user = %+v %v", u, err)
			}
			if _, err := tc.c.User(ctx, "nobody"); err == nil {
				t.Error("unknown user found")
			}
		})
	}
}

func TestJiraSpecifics(t *testing.T) {
	ctx := context.Background()
	d := seed()
	c, srv := jiraClient(t, d)
	loose, _ := c.Issue(ctx, "PRJ-14")
	if loose.StatusCategory != model.StatusCanceled || !loose.Done() {
		t.Errorf("won't do = %+v", loose)
	}
	if lead, _ := c.Project(ctx, "PRJ"); lead.Lead == nil || lead.Lead.AccountID != "acc-dana" {
		t.Errorf("lead = %+v", lead)
	}
	posted, err := c.PostComment(ctx, "PRJ-11", "Hi there\nsecond line\n\nNew paragraph", []tracker.Person{{AccountID: "acc-sam", Name: "Sam Lee"}})
	if err != nil || !strings.HasPrefix(posted.URL, srv.URL+"/browse/PRJ-11?focusedCommentId=") {
		t.Fatalf("posted = %+v %v", posted, err)
	}
	body := d.PostedComments()[0].Body.(map[string]any)
	content := body["content"].([]any)
	first := content[0].(map[string]any)["content"].([]any)
	mention := first[0].(map[string]any)
	if body["type"] != "doc" || len(content) != 2 || mention["type"] != "mention" || mention["attrs"].(map[string]any)["id"] != "acc-sam" ||
		mention["attrs"].(map[string]any)["text"] != "@Sam Lee" {
		t.Errorf("adf = %#v", body)
	}
	created, err := c.CreateIssue(ctx, tracker.NewIssue{Project: "PRJ", Title: "New thing", Description: "why", Parent: "PRJ-10"})
	if err != nil || created.Key != "NEW-1" {
		t.Fatalf("created = %+v %v", created, err)
	}
	fields := d.Created[0]["fields"].(map[string]any)
	if fields["issuetype"].(map[string]any)["name"] != "Task" || fields["parent"].(map[string]any)["key"] != "PRJ-10" {
		t.Errorf("create fields = %v", fields)
	}
	for _, req := range d.RequestLog() {
		if strings.Contains(req, "/rest/api/3/search ") || strings.HasSuffix(req, "/rest/api/3/search") {
			t.Errorf("the removed search endpoint was called: %s", req)
		}
	}
}

func TestJiraScopedTokenUsesCloudID(t *testing.T) {
	var path string
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		path = r.URL.String()
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}}, nil
	})}
	conn := model.Connection{Name: "J", Kind: "jira", Config: map[string]string{"site_url": "https://acme.atlassian.net", "email": "a@b.co",
		"cloud_id": "11111111-2222-4333-8444-555555555555"}}
	c, err := tracker.New(conn, map[string]string{"api_token": "t"}, tracker.Options{HTTP: client})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Myself(context.Background())
	if path != "https://api.atlassian.com/ex/jira/11111111-2222-4333-8444-555555555555/rest/api/3/myself" {
		t.Errorf("path = %s", path)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLinearSpecifics(t *testing.T) {
	ctx := context.Background()
	d := seed()
	c := linearClient(t, d)
	issue, _ := c.Issue(ctx, "PRJ-10")
	if issue.Lead == nil || issue.Lead.AccountID != "acc-dana" || issue.StatusCategory != model.StatusInProgress {
		t.Errorf("issue = %+v", issue)
	}
	since := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	found, err := c.Search(ctx, tracker.Query{Project: "PRJ", CommentsSince: &since})
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range found {
		if !i.CommentsRead {
			t.Errorf("%s has no comments read", i.Key)
		}
		if i.Key == "PRJ-11" && (len(i.Comments) != 1 || i.Comments[0].Body != "Any news?") {
			t.Errorf("inline comments = %+v", i.Comments)
		}
	}
	if _, err := c.Search(ctx, tracker.Query{Project: "PRJ", JQL: "x"}); err == nil || !strings.Contains(err.Error(), "only applies to Jira") {
		t.Errorf("jql err = %v", err)
	}
	sam, _ := c.User(ctx, "acc-sam")
	if _, err := c.PostComment(ctx, "PRJ-11", "Please update", []tracker.Person{sam}); err != nil {
		t.Fatal(err)
	}
	if body := d.PostedComments()[0].Body.(string); body != "https://linear.app/acme/profiles/samlee\n\nPlease update" {
		t.Errorf("body = %q", body)
	}
	if _, err := c.PostComment(ctx, "PRJ-11", "x", []tracker.Person{{AccountID: "a", Name: "No URL"}}); err == nil {
		t.Error("mention without a profile URL posted")
	}
	if _, err := c.PostComment(ctx, "PRJ-99", "x", nil); err == nil {
		t.Error("comment on a missing issue posted")
	}
	created, err := c.CreateIssue(ctx, tracker.NewIssue{Project: "PRJ", Title: "New", Description: "d", Parent: "PRJ-10"})
	if err != nil || created.Key != "NEW-1" || d.Created[0]["teamId"] != "team-PRJ" {
		t.Errorf("created = %+v %v %v", created, err, d.Created)
	}
	if _, err := c.CreateIssue(ctx, tracker.NewIssue{Project: "ZZZ", Title: "x"}); !tracker.IsNotFound(err) {
		t.Errorf("create in a missing team err = %v", err)
	}
}

func TestRateLimitsAreRetried(t *testing.T) {
	ctx := context.Background()
	d := seed()
	c, _ := jiraClient(t, d)
	d.Fail, d.FailTimes = http.StatusTooManyRequests, 2
	if _, err := c.Issue(ctx, "PRJ-10"); err != nil {
		t.Fatalf("after two 429s: %v", err)
	}
	d.Fail, d.FailTimes = http.StatusTooManyRequests, 0
	_, err := c.Issue(ctx, "PRJ-10")
	var te *tracker.Error
	if !errors.As(err, &te) || !te.RateLimited {
		t.Errorf("err = %v", err)
	}
	d.Fail = 0

	calls := 0
	srv := trackertest.Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			trackertest.LinearRateLimited(w)
			return
		}
		trackertest.Linear(seed(), linearKey).ServeHTTP(w, r)
	}))
	lc, _ := tracker.New(model.Connection{Name: "L", Kind: "linear"}, map[string]string{"api_key": linearKey},
		tracker.Options{LinearURL: srv.URL, HTTP: srv.Client(), Sleep: noSleep})
	if _, err := lc.Myself(ctx); err != nil || calls != 2 {
		t.Errorf("linear retry: calls %d err %v", calls, err)
	}
}

func TestErrorsNeverLeakSecrets(t *testing.T) {
	ctx := context.Background()
	d := seed()
	srv := trackertest.Server(t, trackertest.Jira(d, "bot@example.com", "another-token"))
	conn := model.Connection{Name: "Acme Jira", Kind: "jira", Config: map[string]string{"site_url": srv.URL, "email": "bot@example.com"}}
	c, _ := tracker.New(conn, map[string]string{"api_token": jiraToken}, tracker.Options{JiraSites: []string{srv.URL}, HTTP: srv.Client(), Sleep: noSleep})
	_, err := c.Myself(ctx)
	var te *tracker.Error
	if !errors.As(err, &te) || !te.Auth || strings.Contains(err.Error(), jiraToken) {
		t.Errorf("auth err = %v", err)
	}
	lsrv := trackertest.Server(t, trackertest.Linear(d, "other"))
	lc, _ := tracker.New(model.Connection{Name: "L", Kind: "linear"}, map[string]string{"api_key": linearKey},
		tracker.Options{LinearURL: lsrv.URL, HTTP: lsrv.Client(), Sleep: noSleep})
	if _, err := lc.Myself(ctx); err == nil || strings.Contains(err.Error(), linearKey) {
		t.Errorf("linear auth err = %v", err)
	}
	srv.Close()
	if _, err := c.Myself(ctx); err == nil || strings.Contains(err.Error(), srv.URL) || strings.Contains(err.Error(), jiraToken) {
		t.Errorf("network err = %v", err)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	target := trackertest.Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("redirect followed with auth %q", r.Header.Get("Authorization"))
	}))
	srv := trackertest.Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/steal", http.StatusFound)
	}))
	conn := model.Connection{Name: "J", Kind: "jira", Config: map[string]string{"site_url": srv.URL, "email": "a@b.co"}}
	c, _ := tracker.New(conn, map[string]string{"api_token": "t"}, tracker.Options{JiraSites: []string{srv.URL}, HTTP: srv.Client()})
	if _, err := c.Myself(context.Background()); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("err = %v", err)
	}
}

func TestOversizedAndBadAnswers(t *testing.T) {
	big := strings.Repeat("x", 9<<20)
	srv := trackertest.Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "myself") {
			_, _ = w.Write([]byte(big))
			return
		}
		_, _ = w.Write([]byte("not json"))
	}))
	conn := model.Connection{Name: "J", Kind: "jira", Config: map[string]string{"site_url": srv.URL, "email": "a@b.co"}}
	c, _ := tracker.New(conn, map[string]string{"api_token": "t"}, tracker.Options{JiraSites: []string{srv.URL}, HTTP: srv.Client()})
	if _, err := c.Myself(context.Background()); err == nil || !strings.Contains(err.Error(), "MB") {
		t.Errorf("big err = %v", err)
	}
	if _, err := c.Issue(context.Background(), "A-1"); err == nil || !strings.Contains(err.Error(), "not the JSON") {
		t.Errorf("bad json err = %v", err)
	}
	lc, _ := tracker.New(model.Connection{Name: "L", Kind: "linear"}, map[string]string{"api_key": "k"}, tracker.Options{LinearURL: srv.URL, HTTP: srv.Client()})
	if _, err := lc.Issue(context.Background(), "A-1"); err == nil {
		t.Error("bad linear json accepted")
	}
}

func TestNewChecksSettings(t *testing.T) {
	cases := []struct {
		conn    model.Connection
		secrets map[string]string
		want    string
	}{
		{model.Connection{Kind: "http", Name: "H"}, nil, "not a tracker"},
		{model.Connection{Kind: "jira", Name: "J", Config: map[string]string{"site_url": "https://evil.example", "email": "a@b.co"}}, map[string]string{"api_token": "t"}, "atlassian.net"},
		{model.Connection{Kind: "jira", Name: "J", Config: map[string]string{"site_url": "https://acme.atlassian.net"}}, map[string]string{"api_token": "t"}, "no account email"},
		{model.Connection{Kind: "linear", Name: "L"}, map[string]string{}, "no API key"},
	}
	for _, tc := range cases {
		if _, err := tracker.New(tc.conn, tc.secrets, tracker.Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.conn.Kind, err, tc.want)
		}
	}
}

func TestCheckJiraSite(t *testing.T) {
	for _, ok := range []string{"https://acme.atlassian.net", "https://ACME.atlassian.net/"} {
		if err := tracker.CheckJiraSite(strings.TrimRight(ok, "/"), nil); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"http://acme.atlassian.net", "https://atlassian.net", "https://a.b.atlassian.net", "https://acme.atlassian.net:444",
		"https://acme.atlassian.net.evil.com", "https://u:p@acme.atlassian.net", "https://acme.atlassian.net/x", "https://evil.com/?a=.atlassian.net",
		"http://127.0.0.1:9000"} {
		if err := tracker.CheckJiraSite(bad, nil); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if err := tracker.CheckJiraSite("http://127.0.0.1:9000", []string{"http://127.0.0.1:9000/"}); err != nil {
		t.Errorf("listed site refused: %v", err)
	}
}

func TestFilters(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	issues := []tracker.Issue{
		{Key: "A-1", DueDate: "2026-10-01", StatusCategory: model.StatusToDo, UpdatedAt: old, Assignee: &tracker.Person{AccountID: "x", Name: "Sam", Email: "s@e.co"}},
		{Key: "A-2", DueDate: "2026-10-07", StatusCategory: model.StatusInProgress, UpdatedAt: now, Status: "Blocked"},
		{Key: "A-3", DueDate: "2026-09-01", StatusCategory: model.StatusDone, UpdatedAt: old},
		{Key: "A-4", StatusCategory: model.StatusToDo, UpdatedAt: now, Labels: []string{"Blocked-by-vendor"}},
	}
	keys := func(in []tracker.Issue) string {
		var out []string
		for _, i := range in {
			out = append(out, i.Key)
		}
		return strings.Join(out, ",")
	}
	cases := map[string]tracker.Filter{
		"A-1":         {Overdue: true},
		"A-2":         {DueWithin: 3},
		"A-1,":        {StaleDays: 14},
		"A-2,A-4":     {Unassigned: true},
		"A-2,A-4 ":    {Blocked: true},
		"A-1  ":       {Owner: "S@E.CO"},
		"A-1,A-2,A-4": {Owner: ""},
	}
	for want, f := range cases {
		want = strings.TrimRight(strings.TrimSpace(want), ",")
		if f == (tracker.Filter{}) {
			want = "A-1,A-2,A-3,A-4"
		}
		if got := keys(f.Apply(issues, now, time.UTC)); got != want {
			t.Errorf("%+v = %s, want %s", f, got, want)
		}
	}
}

// A refusal (a 4xx, a rate limit, Linear's GraphQL errors) is marked Rejected: nothing was written. A 5xx, or a 2xx
// Studio cannot read, is not, so a caller cannot tell whether a write was made.
func TestWriteRefusalsAreMarkedRejected(t *testing.T) {
	d := seed()
	c, _ := jiraClient(t, d)
	ctx := context.Background()
	d.Fail, d.FailTimes = http.StatusBadRequest, 1
	_, err := c.PostComment(ctx, "PRJ-10", "hi", nil)
	var te *tracker.Error
	if !errors.As(err, &te) || !te.Rejected {
		t.Errorf("4xx = %+v", err)
	}
	d.Fail, d.FailTimes = http.StatusInternalServerError, 1
	_, err = c.PostComment(ctx, "PRJ-10", "hi", nil)
	if !errors.As(err, &te) || te.Rejected {
		t.Errorf("5xx = %+v", err)
	}
	ld := seed()
	lc := linearClient(t, ld)
	ld.Fail, ld.FailTimes = http.StatusBadRequest, 1
	_, err = lc.PostComment(ctx, "PRJ-10", "hi", nil)
	if !errors.As(err, &te) || !te.Rejected {
		t.Errorf("linear refusal = %+v", err)
	}
	// A 2xx Studio cannot read
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte("<html>ok</html>"))
			return
		}
		trackertest.Jira(seed(), "bot@example.com", jiraToken).ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	conn := model.Connection{Name: "Odd Jira", Kind: "jira", Config: map[string]string{"site_url": srv.URL, "email": "bot@example.com"}}
	oc, err := tracker.New(conn, map[string]string{"api_token": jiraToken}, tracker.Options{JiraSites: []string{srv.URL}, HTTP: srv.Client(), Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	_, err = oc.PostComment(ctx, "PRJ-10", "hi", nil)
	if !errors.As(err, &te) || te.Rejected {
		t.Errorf("unreadable 2xx = %+v", err)
	}
}
