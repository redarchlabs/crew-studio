package tracker_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/tracker"
	"crewstudio/internal/tracker/trackertest"
)

func TestKM2CreateIssueAfterAFailedAnswerReturnsWhatItMade(t *testing.T) {
	ctx := context.Background()
	d := seed()
	k := trackertest.NewKM2(d, km2Key)
	var mu sync.Mutex
	var waits []time.Duration
	srv := trackertest.Server(t, k.Handler())
	c, err := tracker.New(model.Connection{Name: "Acme KM2", Kind: model.TrackerKM2, Config: map[string]string{"base_url": srv.URL, "self_email": "bot@example.com"}},
		map[string]string{"api_key": km2Key}, tracker.Options{HTTP: srv.Client(), Sleep: func(_ context.Context, w time.Duration) error {
			mu.Lock()
			defer mu.Unlock()
			waits = append(waits, w)
			return nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	// KM2 stored the issue, then failed to answer: the issue it made is returned, not a second one
	k.Fail500AfterWrite = 1
	created, err := c.CreateIssue(ctx, tracker.NewIssue{Project: "PRJ", Title: "Stored anyway"})
	if err != nil || created.Key != "PRJ-15" || len(d.Created) != 1 {
		t.Fatalf("created = %+v %v, %d creates", created, err, len(d.Created))
	}
	// Two conflicts wait 300 ms, then 600 ms, before the next tries
	k.Collide = 2
	created, err = c.CreateIssue(ctx, tracker.NewIssue{Project: "PRJ", Title: "Busy"})
	if err != nil || created.Key != "PRJ-18" || !slices.Equal(waits, []time.Duration{300 * time.Millisecond, 600 * time.Millisecond}) {
		t.Errorf("after conflicts: %+v %v, waits %v", created, err, waits)
	}
}

func TestKM2CheckRequiresTheStatusCategoryPicklistAndWarnsOnIdentifiers(t *testing.T) {
	ctx := context.Background()
	k := trackertest.NewKM2(seed(), km2Key)
	c, _ := km2Client(t, k, nil)
	checker := c.(tracker.Checker)
	edits := map[string]func(map[string]any){
		"is a text field":      func(f map[string]any) { f["field_type"], f["picklist_options"] = "text", nil },
		"options to_do, doing": func(f map[string]any) { f["picklist_options"] = []string{"to_do", "doing", "done", "canceled"} },
		"options to_do, in_progress, done, canceled, cancelled": func(f map[string]any) {
			f["picklist_options"] = []string{"to_do", "in_progress", "done", "canceled", "cancelled"}
		},
	}
	for want, edit := range edits {
		k.CatalogEdit = func(entity string, f map[string]any) {
			if entity == "issue" && f["slug"] == "status_category" {
				edit(f)
			}
		}
		found, err := checker.Check(ctx)
		if err != nil || len(found.Problems) != 1 || !strings.Contains(found.Problems[0], "trk_issue.status_category must be a picklist of exactly") ||
			!strings.Contains(found.Problems[0], want) {
			t.Errorf("%s: %+v %v", want, found, err)
		}
	}
	k.CatalogEdit = func(entity string, f map[string]any) {
		if entity == "issue" && f["slug"] == "identifier" {
			f["is_unique"] = false
		}
	}
	found, err := checker.Check(ctx)
	if err != nil || len(found.Problems) != 0 || len(found.Warnings) != 1 || !strings.Contains(found.Warnings[0], "trk_issue.identifier is not unique") {
		t.Errorf("warnings = %+v %v", found, err)
	}
}

func TestKM2NamesPeopleBeyondTheCacheOneByOne(t *testing.T) {
	defer tracker.SetKM2Limits(1, 200, 10_000)()
	ctx := context.Background()
	d := seed()
	k := trackertest.NewKM2(d, km2Key)
	c, _ := km2Client(t, k, nil)
	issue, err := c.Issue(ctx, "PRJ-11")
	if err != nil || issue.Assignee == nil || issue.Assignee.Name != "Sam Lee" {
		t.Fatalf("issue = %+v %v", issue, err)
	}
	project, err := c.Project(ctx, "PRJ")
	if err != nil || project.Lead == nil || project.Lead.Name != "Dana Ruiz" {
		t.Errorf("project = %+v %v", project, err)
	}
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	found, err := c.Search(ctx, tracker.Query{Project: "PRJ", CommentsSince: &since})
	if err != nil || len(found) != 3 {
		t.Fatalf("search = %v %v", found, err)
	}
	byID := 0
	for _, req := range d.RequestLog() {
		if strings.Contains(req, "/trk_person/records?filter=id%3Ain%3A") {
			byID++
		}
	}
	// Sam once, then Dana once: people already found are not read again
	if byID != 2 {
		t.Errorf("read people by id %d times: %v", byID, d.RequestLog())
	}
}

func TestKM2RefreshesThePeopleCacheOnAMissAtMostEveryFewMinutes(t *testing.T) {
	ctx := context.Background()
	d := seed()
	k := trackertest.NewKM2(d, km2Key)
	srv := trackertest.Server(t, k.Handler())
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	opts := tracker.WithKM2Cache(tracker.Options{HTTP: srv.Client(), Sleep: noSleep}, clock)
	conn := model.Connection{ID: "conn-1", Name: "Acme KM2", Kind: model.TrackerKM2, Config: map[string]string{"base_url": srv.URL, "self_email": "bot@example.com"}}
	client := func() tracker.Client {
		c, err := tracker.New(conn, map[string]string{"api_key": km2Key}, opts)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if i, err := client().Issue(ctx, "PRJ-11"); err != nil || i.Assignee.Name != "Sam Lee" {
		t.Fatalf("first = %+v %v", i, err)
	}
	// A person added in KM2 later: the next client shares the cache and does not read the people again at once
	eve := trackertest.User{ID: "acc-eve", Name: "Eve Park", Email: "eve@example.com"}
	d.Users = append(d.Users, eve)
	d.Update("PRJ-13", func(i *trackertest.Issue) { i.Assignee = &eve })
	if i, _ := client().Issue(ctx, "PRJ-13"); i.Assignee == nil || i.Assignee.Name != "" || i.Assignee.AccountID != "acc-eve" {
		t.Errorf("within the refresh window = %+v", i.Assignee)
	}
	mu.Lock()
	now = now.Add(6 * time.Minute)
	mu.Unlock()
	if i, _ := client().Issue(ctx, "PRJ-13"); i.Assignee == nil || i.Assignee.Name != "Eve Park" {
		t.Errorf("after the refresh window = %+v", i.Assignee)
	}
	reads := 0
	for _, req := range d.RequestLog() {
		if strings.HasPrefix(req, "GET /api/v1/entities/trk_person/records?limit=") {
			reads++
		}
	}
	if reads != 2 {
		t.Errorf("read the whole person list %d times", reads)
	}
}

func TestKM2ReadsCommentsThroughTheIssueRelationship(t *testing.T) {
	ctx := context.Background()
	d := seed()
	k := trackertest.NewKM2(d, km2Key)
	k.BareComments = true
	c, _ := km2Client(t, k, nil)
	newest, err := c.Comments(ctx, "PRJ-11", 5)
	if err != nil || len(newest) != 2 || newest[0].Body != "Any news?" {
		t.Errorf("comments = %+v %v", newest, err)
	}
	since := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	found, err := c.Search(ctx, tracker.Query{Project: "PRJ", CommentsSince: &since})
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range found {
		if i.Key == "PRJ-11" && (len(i.Comments) != 1 || i.Comments[0].Body != "Any news?") {
			t.Errorf("inline comments = %+v", i.Comments)
		}
	}
	for _, req := range d.RequestLog() {
		if strings.Contains(req, "trk_comment") && (strings.Contains(req, "issue_key") || strings.Contains(req, "project_key")) {
			t.Errorf("comments read through a copied key: %s", req)
		}
	}
	if _, err := c.Comments(ctx, "PRJ-99", 5); !tracker.IsNotFound(err) {
		t.Errorf("comments of a missing issue: %v", err)
	}
}

func TestKM2CommentCapCoversEveryChunk(t *testing.T) {
	defer tracker.SetKM2Limits(5_000, 1, 1)()
	d := seed()
	d.Comment("PRJ-10", trackertest.Comment{Author: sam, At: time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC), Body: "Kickoff"})
	c, _ := km2Client(t, trackertest.NewKM2(d, km2Key), nil)
	since := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	// One comment per chunk of one issue, but two in all: past the cap of one
	_, err := c.Search(context.Background(), tracker.Query{Project: "PRJ", CommentsSince: &since})
	var le *tracker.LimitError
	if !errors.As(err, &le) || le.Max != 1 || le.Count != 2 {
		t.Errorf("err = %v", err)
	}
}

func TestKM2CommentWithoutATimeIsLogged(t *testing.T) {
	d := seed()
	k := trackertest.NewKM2(d, km2Key)
	k.BadCommentTimes = map[string]bool{d.Issues["PRJ-11"].Comments[0].ID: true}
	var logs bytes.Buffer
	srv := trackertest.Server(t, k.Handler())
	c, _ := tracker.New(model.Connection{Name: "Acme KM2", Kind: model.TrackerKM2, Config: map[string]string{"base_url": srv.URL, "self_email": "bot@example.com"}},
		map[string]string{"api_key": km2Key}, tracker.Options{HTTP: srv.Client(), Sleep: noSleep, Log: slog.New(slog.NewTextHandler(&logs, nil))})
	comments, err := c.Comments(context.Background(), "PRJ-11", 5)
	if err != nil || len(comments) != 2 || !comments[1].At.IsZero() {
		t.Fatalf("comments = %+v %v", comments, err)
	}
	if !strings.Contains(logs.String(), "no readable created_at") || !strings.Contains(logs.String(), "record_id="+d.Issues["PRJ-11"].Comments[0].ID) {
		t.Errorf("log = %s", logs.String())
	}
}

func TestKM2SitesAllowlistAndErrorDetail(t *testing.T) {
	conn := model.Connection{Name: "K", Kind: model.TrackerKM2, Config: map[string]string{"base_url": "https://km2.example.com", "self_email": "a@b.co"}}
	key := map[string]string{"api_key": km2Key}
	if _, err := tracker.New(conn, key, tracker.Options{KM2TrackerSites: []string{"https://other.example.com"}}); err == nil ||
		!strings.Contains(err.Error(), "STUDIO_KM2_TRACKER_SITES") {
		t.Errorf("unlisted site: %v", err)
	}
	if _, err := tracker.New(conn, key, tracker.Options{KM2TrackerSites: []string{"https://KM2.example.com/"}}); err != nil {
		t.Errorf("listed site: %v", err)
	}
	if err := tracker.CheckKM2Base("https://anything.example.org", nil); err != nil {
		t.Errorf("no list: %v", err)
	}
	answers := map[string]string{
		"<html>Bad gateway: <script>x</script></html>":                   "Acme KM2 answered HTTP 400",
		`{"detail": "line\u0007one\nline two"}`:                          "Acme KM2 answered HTTP 400: line one line two",
		`{"detail": "` + strings.Repeat("x", 400) + `"}`:                 "Acme KM2 answered HTTP 400: " + strings.Repeat("x", 300) + "…",
		`{"detail": [{"loc": ["q"], "msg": "bad"}, {"loc": ["q"]}]}`:     "Acme KM2 answered HTTP 400",
		`{"detail": {"nested": "object"}}`:                               "Acme KM2 answered HTTP 400",
		`{"detail": [{"loc": ["q"], "msg": "a"}, {"msg": "b"}], "x": 1}`: "Acme KM2 answered HTTP 400: a; b",
	}
	for body, want := range answers {
		srv := trackertest.Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(body))
		}))
		c, _ := tracker.New(model.Connection{Name: "Acme KM2", Kind: model.TrackerKM2, Config: map[string]string{"base_url": srv.URL, "self_email": "a@b.co"}},
			key, tracker.Options{HTTP: srv.Client(), Sleep: noSleep})
		if _, err := c.Issue(context.Background(), "PRJ-1"); err == nil || err.Error() != want {
			t.Errorf("%.40s: err = %v", body, err)
		}
	}
}

func TestKM2CleansNamesItWritesAndFindsItselfIgnoringCase(t *testing.T) {
	ctx := context.Background()
	d := seed()
	mallory := trackertest.User{ID: "acc-mal", Name: "Mal\nlory \u0007 " + strings.Repeat("y", 200), Email: "mal@example.com"}
	d.Users = append(d.Users, mallory)
	d.Me.Name, d.Me.Email = "Studio\r\nBot", "Bot@Example.COM"
	c, _ := km2Client(t, trackertest.NewKM2(d, km2Key), map[string]string{"self_email": "BOT@example.com"})
	me, err := c.Myself(ctx)
	if err != nil || me.AccountID != "me-1" || me.Name != "Studio Bot" {
		t.Fatalf("myself = %+v %v", me, err)
	}
	if _, err := c.PostComment(ctx, "PRJ-11", "Hello", []tracker.Person{{AccountID: "acc-mal"}}); err != nil {
		t.Fatal(err)
	}
	body := d.PostedComments()[0].Body.(map[string]any)
	first, _, _ := strings.Cut(body["body"].(string), "\n\n")
	if first != "@Mal lory "+strings.Repeat("y", 111) || body["author_name"] != "Studio Bot" {
		t.Errorf("mention line %q, author %q", first, body["author_name"])
	}
}
