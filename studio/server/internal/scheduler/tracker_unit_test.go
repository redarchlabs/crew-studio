package scheduler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/connections"
	"crewstudio/internal/model"
	"crewstudio/internal/secrets"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
	"crewstudio/internal/tracker/trackertest"
	"crewstudio/internal/watch"
)

type trackerEnv struct {
	*env
	data  *trackertest.Data
	conns *connections.Service
	conn  model.Connection
}

var (
	tSam  = trackertest.User{ID: "acc-sam", Name: "Sam Lee", Email: "sam@example.com"}
	tDana = trackertest.User{ID: "acc-dana", Name: "Dana Ruiz"}
)

// newTrackerEnv runs a fake Jira (or Linear) with a project PRJ: PRJ-10 with children PRJ-11 (late, blocked, Sam's)
// and PRJ-12 (done), and PRJ-13 below PRJ-11 (unassigned, due after PRJ-10).
func newTrackerEnv(t *testing.T, kind string) *trackerEnv {
	e := newEnv(t)
	d := trackertest.NewData()
	d.Users = []trackertest.User{tSam, tDana}
	d.Projects["PRJ"] = &tDana
	past := e.now.AddDate(0, 0, -3).Format(time.DateOnly)
	d.Add(trackertest.Issue{Key: "PRJ-10", Title: "Atlas", Due: "2099-11-15", Category: "indeterminate"})
	d.Add(trackertest.Issue{Key: "PRJ-11", Title: "Vendor", Parent: "PRJ-10", Assignee: &tSam, Due: past, Labels: []string{"blocked"}})
	d.Add(trackertest.Issue{Key: "PRJ-12", Title: "Docs", Parent: "PRJ-10", Category: "done"})
	d.Add(trackertest.Issue{Key: "PRJ-13", Title: "Sub", Parent: "PRJ-11", Due: "2099-12-01"})
	box, err := secrets.NewBox(secrets.GenerateKey())
	ok(t, err)
	conns := connections.New(e.st, box)
	in := model.ConnectionInput{Name: "Tracker", Kind: kind, AreaIDs: []string{e.area}}
	var topts tracker.Options
	if kind == model.TrackerJira {
		srv := trackertest.Server(t, trackertest.Jira(d, "bot@example.com", "tok"))
		in.Config, in.Secrets = map[string]string{"site_url": srv.URL, "email": "bot@example.com"}, map[string]string{"api_token": "tok"}
		topts = tracker.Options{JiraSites: []string{srv.URL}, HTTP: srv.Client()}
	} else if kind == model.TrackerKM2 {
		srv := trackertest.Server(t, trackertest.NewKM2(d, "km2_key").Handler())
		in.Config, in.Secrets = map[string]string{"base_url": srv.URL, "self_email": "bot@example.com"}, map[string]string{"api_key": "km2_key"}
		topts = tracker.Options{HTTP: srv.Client()}
	} else {
		srv := trackertest.Server(t, trackertest.Linear(d, "key"))
		in.Config, in.Secrets = map[string]string{}, map[string]string{"api_key": "key"}
		topts = tracker.Options{LinearURL: srv.URL, HTTP: srv.Client()}
	}
	conn, err := conns.Create(bg(), in)
	ok(t, err)
	ok(t, e.st.SetCrewConnections(bg(), e.crew.ID, []string{conn.ID}, store.AllAreas))
	ts := tracker.NewService(e.st, conns, topts)
	e.s.SetWatches(watch.New(e.st, conns, t.TempDir()).WithTrackers(ts))
	return &trackerEnv{env: e, data: d, conns: conns, conn: conn}
}

func (w *trackerEnv) tickAgain(scheduleID string) {
	w.t.Helper()
	w.now = w.now.Add(time.Minute)
	_, err := w.st.Pool().Exec(bg(), `UPDATE schedules SET next_run_at = $2 WHERE id = $1`, scheduleID, w.now.Add(-time.Minute))
	ok(w.t, err)
	w.s.Tick(bg())
}

func (w *trackerEnv) saveWatch(scheduleID string, in model.WatchInput) model.Watch {
	w.t.Helper()
	clean, err := model.NormalizeWatch(in)
	ok(w.t, err)
	saved, err := w.st.SaveWatch(bg(), scheduleID, clean)
	ok(w.t, err)
	return saved
}

func TestTrackingListWatchRollsUpChildren(t *testing.T) {
	for _, kind := range []string{model.TrackerJira, model.TrackerKM2} {
		t.Run(kind, func(t *testing.T) { trackingListRollsUp(t, kind) })
	}
}

// trackingListRollsUp runs a tracking-list watch on a Jira or KM2 connection (both give project leads).
func trackingListRollsUp(t *testing.T, kind string) {
	w := newTrackerEnv(t, kind)
	w.user("sam", []string{auth.WorkOrdersCreate}, []string{w.area})
	_, err := w.st.SeeTrackerPeople(bg(), w.conn.ID, []model.TrackerPerson{{AccountID: "acc-sam", DisplayName: "Sam Lee", Email: "SAM@example.com"}}, w.now)
	ok(t, err)
	sc := w.schedule("pmo", w.runAs, allow, time.Minute)
	sched := sc.ID
	_, err = w.st.AddTrackingItem(bg(), w.crew.ID, model.TrackingItemInput{ScheduleID: &sched, ConnectionID: w.conn.ID, Project: "Atlas",
		ItemRef: "PRJ-10", IncludeChildren: model.IncludeChildrenAll}, nil)
	ok(t, err)
	ladder := &model.FollowupLadder{DateField: "due_date", SkipWhen: map[string][]string{"status_category": {"done"}},
		Steps: []model.FollowupStep{{Name: "overdue", Offset: "+1d", Notify: "owner"}}}
	wt := w.saveWatch(sc.ID, model.WatchInput{Source: model.WatchSource{Kind: model.WatchSourceTrackingList, Comments: true},
		TrackedFields: []string{"status", "status_category", "assignee", "due_date"}, RunWhen: model.WatchRunChangedOrFollowups, Followups: ladder})

	w.s.Tick(bg())
	wos := w.workOrders()
	if len(wos) != 1 {
		t.Fatalf("first check: %d work orders; checks %+v", len(wos), w.checksOf(wt.ID))
	}
	ch := decodeChanges(t, wos[0])
	if len(ch.Added) != 4 || ch.Added[0].ID != kind+":PRJ-10" || ch.PreviousCheckAt != nil {
		t.Fatalf("changes = %+v", ch)
	}
	var r watch.RollupsInput
	ok(t, json.Unmarshal([]byte(wos[0].Inputs[model.WatchInputRollups]), &r))
	if len(r.Items) != 1 {
		t.Fatalf("rollups = %+v", r)
	}
	ru := r.Items[0]
	if ru.Item != "PRJ-10" || ru.Project != "Atlas" || ru.Children.Total != 3 || ru.Children.Done != 1 || ru.Children.ToDo != 2 ||
		strings.Join(ru.Overdue, ",") != "PRJ-11" || strings.Join(ru.Blocked, ",") != "PRJ-11" || strings.Join(ru.Unassigned, ",") != "PRJ-13" ||
		len(ru.DueAfterParent) != 1 || ru.DueAfterParent[0].ID != "PRJ-13" || !ru.ChangedSinceLastCheck || ru.Lead == nil || ru.Lead.AccountID != "acc-dana" {
		t.Errorf("rollup = %+v", ru)
	}
	var f watch.Followups
	ok(t, json.Unmarshal([]byte(wos[0].Inputs[model.WatchInputFollowups]), &f))
	if len(f.Due) != 1 || f.Due[0].Subject != kind+":PRJ-11" || f.Due[0].NotifyPerson == nil || f.Due[0].NotifyPerson.AccountID != "acc-sam" ||
		f.Due[0].NotifyPerson.StudioUser != "sam" {
		t.Fatalf("followups = %+v", f.Due)
	}

	// Sam comments (and so does the connection's own account): only Sam's comment is new, and PRJ-10's roll-up
	// changed. The follow-up is recorded so it no longer counts.
	_, _, err = w.st.RecordAction(bg(), store.StateScope{CrewID: w.crew.ID, ScheduleID: &sched, DryRun: true}, nil,
		model.LogRecord{Subject: kind + ":PRJ-11", Action: "followup:overdue", Details: map[string]any{}})
	ok(t, err)
	w.data.Comment("PRJ-11", trackertest.Comment{Author: tSam, At: w.now.Add(30 * time.Second), Body: strings.Repeat("Blocked on vendor. ", 40)})
	w.data.Comment("PRJ-13", trackertest.Comment{Author: w.data.Me, At: w.now.Add(31 * time.Second), Body: "Reminder"})
	w.tickAgain(sc.ID)
	wos = w.workOrders()
	if len(wos) != 2 {
		t.Fatalf("a new comment starts a run: %d work orders; checks %+v", len(wos), w.checksOf(wt.ID))
	}
	ch = decodeChanges(t, wos[0])
	if len(ch.NewComments) != 1 || ch.NewComments[0].ID != kind+":PRJ-11" || ch.NewComments[0].Author != "Sam Lee" ||
		!ch.NewComments[0].Truncated || len([]rune(ch.NewComments[0].Excerpt)) != watch.ExcerptChars+1 {
		t.Fatalf("new comments = %+v", ch.NewComments)
	}
	ok(t, json.Unmarshal([]byte(wos[0].Inputs[model.WatchInputRollups]), &r))
	if len(r.Items) != 1 || r.Items[0].NewComments != 1 {
		t.Errorf("rollups after a comment = %+v", r)
	}

	// Nothing new: no run, and the comments are not reported twice
	w.tickAgain(sc.ID)
	if n := len(w.workOrders()); n != 2 {
		t.Errorf("a quiet tick started a run (%d work orders): %+v", n, w.checksOf(wt.ID)[0])
	}

	// A child disappears: its tracked item's roll-up changes
	w.data.Update("PRJ-13", func(i *trackertest.Issue) { i.Parent = "" })
	w.tickAgain(sc.ID)
	wos = w.workOrders()
	ok(t, json.Unmarshal([]byte(wos[0].Inputs[model.WatchInputRollups]), &r))
	if len(wos) != 3 || len(r.Items) != 1 || r.Items[0].Children.Total != 2 {
		t.Errorf("after a child left: %d work orders, rollups %+v", len(wos), r)
	}

	// A tracked item that no longer exists fails the check with a clear message
	w.data.Update("PRJ-10", func(i *trackertest.Issue) { i.Key = "GONE-1" })
	delete(w.data.Issues, "PRJ-10")
	w.tickAgain(sc.ID)
	if c := w.checksOf(wt.ID)[0]; c.Outcome != model.WatchCheckError || !strings.Contains(c.Message, "PRJ-10 (Atlas) no longer exists") {
		t.Errorf("missing item check = %+v", c)
	}
}

func (w *trackerEnv) checksOf(watchID string) []model.WatchCheck {
	w.t.Helper()
	items, err := w.st.ListWatchChecks(bg(), watchID, 50)
	ok(w.t, err)
	return items
}

func TestTrackerProjectWatchOnLinear(t *testing.T) {
	w := newTrackerEnv(t, model.TrackerLinear)
	sc := w.schedule("eng", w.runAs, allow, time.Minute)
	wt := w.saveWatch(sc.ID, model.WatchInput{ConnectionID: &w.conn.ID,
		Source: model.WatchSource{Kind: model.WatchSourceTracker, Project: "PRJ", Comments: true}})
	w.s.Tick(bg())
	wos := w.workOrders()
	if len(wos) != 1 {
		t.Fatalf("first check: %+v", w.checksOf(wt.ID))
	}
	ch := decodeChanges(t, wos[0])
	if len(ch.Added) != 3 || ch.Added[0].ID != "linear:PRJ-10" || !strings.Contains(ch.Source, "PRJ, open issues") {
		t.Fatalf("changes = %+v", ch)
	}
	if _, has := wos[0].Inputs[model.WatchInputRollups]; has {
		t.Error("a project watch has no roll-ups")
	}
	w.data.Comment("PRJ-11", trackertest.Comment{Author: tDana, At: w.now.Add(20 * time.Second), Body: "Moved to Thursday"})
	w.data.Update("PRJ-13", func(i *trackertest.Issue) { i.Assignee = &tDana })
	w.tickAgain(sc.ID)
	wos = w.workOrders()
	if len(wos) != 2 {
		t.Fatalf("changes did not start a run: %+v", w.checksOf(wt.ID))
	}
	ch = decodeChanges(t, wos[0])
	if len(ch.Changed) != 1 || ch.Changed[0].ID != "linear:PRJ-13" || len(ch.NewComments) != 1 || ch.NewComments[0].Excerpt != "Moved to Thursday" {
		t.Fatalf("changes = %+v", ch)
	}
	// Unlinking the connection stops the watch with a clear error
	ok(t, w.st.SetCrewConnections(bg(), w.crew.ID, nil, store.AllAreas))
	w.tickAgain(sc.ID)
	if c := w.checksOf(wt.ID)[0]; c.Outcome != model.WatchCheckError || !strings.Contains(c.Message, "may not use") {
		t.Errorf("unlinked = %+v", c)
	}
}

func TestTrackerProjectWatchOnKM2(t *testing.T) {
	w := newTrackerEnv(t, model.TrackerKM2)
	sc := w.schedule("pmo", w.runAs, allow, time.Minute)
	wt := w.saveWatch(sc.ID, model.WatchInput{ConnectionID: &w.conn.ID,
		Source: model.WatchSource{Kind: model.WatchSourceTracker, Project: "PRJ", Comments: true}})
	w.s.Tick(bg())
	wos := w.workOrders()
	if len(wos) != 1 {
		t.Fatalf("first check: %+v", w.checksOf(wt.ID))
	}
	ch := decodeChanges(t, wos[0])
	if len(ch.Added) != 3 || ch.Added[0].ID != "km2_tracker:PRJ-10" || !strings.Contains(ch.Source, "PRJ, open issues") {
		t.Fatalf("changes = %+v", ch)
	}
	// A comment does not move the issue's activity time in KM2, yet it is found; the bot's own comment is left out
	w.data.Comment("PRJ-11", trackertest.Comment{Author: tDana, At: w.now.Add(20 * time.Second), Body: "Moved to Thursday"})
	w.data.Comment("PRJ-10", trackertest.Comment{Author: w.data.Me, At: w.now.Add(21 * time.Second), Body: "Reminder"})
	w.data.Update("PRJ-13", func(i *trackertest.Issue) { i.Assignee = &tDana })
	w.tickAgain(sc.ID)
	wos = w.workOrders()
	if len(wos) != 2 {
		t.Fatalf("changes did not start a run: %+v", w.checksOf(wt.ID))
	}
	ch = decodeChanges(t, wos[0])
	if len(ch.Changed) != 1 || ch.Changed[0].ID != "km2_tracker:PRJ-13" || len(ch.NewComments) != 1 || ch.NewComments[0].Excerpt != "Moved to Thursday" ||
		ch.NewComments[0].Author != "Dana Ruiz" {
		t.Fatalf("changes = %+v", ch)
	}
	w.tickAgain(sc.ID)
	if n := len(w.workOrders()); n != 2 {
		t.Errorf("a quiet tick started a run (%d work orders): %+v", n, w.checksOf(wt.ID)[0])
	}
}
