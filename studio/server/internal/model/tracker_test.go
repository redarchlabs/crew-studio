package model

import (
	"strings"
	"testing"
)

func TestNormalizeItemRef(t *testing.T) {
	cases := map[string]string{"prj-10": "PRJ-10", " jira:PRJ-10 ": "PRJ-10", "LINEAR:eng-42": "ENG-42", "A_B2-1": "A_B2-1"}
	for in, want := range cases {
		if got, ok := NormalizeItemRef(in); !ok || got != want {
			t.Errorf("%q -> %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "PRJ", "10", "PRJ-", "1PRJ-2", "PRJ-1 OR 1=1", "PRJ-1/../x", "jira:"} {
		if _, ok := NormalizeItemRef(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestProjectKeyAndAccountID(t *testing.T) {
	if k, ok := NormalizeProjectKey(" prj "); !ok || k != "PRJ" {
		t.Errorf("project key %q %v", k, ok)
	}
	if _, ok := NormalizeProjectKey("PR J"); ok {
		t.Error("space accepted")
	}
	for _, id := range []string{"5b10ac8d82e05b22cc7d4ef5", "712020:2b6c1b55-0b0e-4b8e-9d66-6c1c8c1f4b11", "a1b2-c3"} {
		if !ValidAccountID(id) {
			t.Errorf("%q refused", id)
		}
	}
	for _, id := range []string{"", "a b", "x\n", strings.Repeat("a", 129), "-x"} {
		if ValidAccountID(id) {
			t.Errorf("%q accepted", id)
		}
	}
	if !ConnectionFits(ConnectionTracker, "jira") || !ConnectionFits(ConnectionTracker, "linear") || ConnectionFits(ConnectionTracker, "http") {
		t.Error("tracker fits jira and linear only")
	}
	if !ConnectionFits("http", "http") || ConnectionFits("http", "jira") {
		t.Error("http fits http only")
	}
	if TrackerSubject("jira", "PRJ-1") != "jira:PRJ-1" {
		t.Error("subject")
	}
}

func TestNormalizeTrackingItem(t *testing.T) {
	conn := "11111111-1111-4111-8111-111111111111"
	out, err := NormalizeTrackingItem(TrackingItemInput{ConnectionID: conn, ItemRef: "prj-10", Project: " Atlas ", Note: " n "})
	if err != nil {
		t.Fatal(err)
	}
	if out.ItemRef != "PRJ-10" || out.Project != "Atlas" || out.IncludeChildren != IncludeChildrenAll || out.Note != "n" {
		t.Errorf("out = %+v", out)
	}
	sched := "bad"
	_, err = NormalizeTrackingItem(TrackingItemInput{ScheduleID: &sched, ConnectionID: "x", ItemRef: "?", Project: "",
		IncludeChildren: "some", LeadAccountID: "a b", Note: strings.Repeat("x", 1001)})
	verr, ok := err.(*ValidationError)
	if !ok || len(verr.Details) < 6 {
		t.Fatalf("err = %v", err)
	}
	if _, err := NormalizeTrackingItem(TrackingItemInput{ConnectionID: conn, ItemRef: "A-1", Project: "a\nb"}); err == nil {
		t.Error("multi-line project accepted")
	}
}

func TestTrackerWatchSource(t *testing.T) {
	conn := "11111111-1111-4111-8111-111111111111"
	out, err := NormalizeWatch(WatchInput{ConnectionID: &conn, Source: WatchSource{Kind: WatchSourceTracker, Project: "prj", Comments: true}})
	if err != nil {
		t.Fatal(err)
	}
	if out.IDField != TrackerIDField || strings.Join(out.TrackedFields, ",") != "status,assignee,due_date,priority" ||
		out.Source.Project != "PRJ" || out.Source.StaleDays != DefaultStaleDays || out.MentionCapPerDay != 3 || out.CommentCapPerRun != 30 {
		t.Errorf("out = %+v", out)
	}
	_, err = NormalizeWatch(WatchInput{Source: WatchSource{Kind: WatchSourceTracker, Project: "", JQL: "a ORDER  by b", Lead: "x y", ItemsPath: "x"},
		IDField: "id", TrackedFields: []string{"bogus"}, MentionCapPerDay: 99, CommentCapPerRun: -1})
	verr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("err = %v", err)
	}
	joined := strings.Join(verr.Details, "|")
	for _, want := range []string{"connection_id: is required: pick a tracker (Jira, Linear or KM2 issues) connection", "source.project", "source.jql", "source.lead",
		"id_field", "tracked_fields", "mention_cap_per_day", "comment_cap_per_run", "source.items_path"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %s", want, joined)
		}
	}
	if _, err := NormalizeWatch(WatchInput{Source: WatchSource{Kind: WatchSourceTracker, Project: "P", JQL: strings.Repeat("x", 1001)}, ConnectionID: &conn}); err == nil {
		t.Error("long jql accepted")
	}
	if _, err := NormalizeWatch(WatchInput{Source: WatchSource{Kind: WatchSourceTracker, Project: "P", JQL: "a\nb"}, ConnectionID: &conn}); err == nil {
		t.Error("multi-line jql accepted")
	}
	for jql, ok := range map[string]bool{`labels = x`: true, `(a = 1 OR b = 2) AND c = 3`: true, `summary ~ "a ) b"`: true, `summary ~ "say \"x)\""`: true,
		`1=1) OR (project = OTHER`: false, `(a`: false, `a)`: false, `summary ~ "open`: false, `x = 'a`: false} {
		_, err := NormalizeWatch(WatchInput{Source: WatchSource{Kind: WatchSourceTracker, Project: "P", JQL: jql}, ConnectionID: &conn})
		if (err == nil) != ok {
			t.Errorf("jql %q: err %v, want ok=%v", jql, err, ok)
		}
	}
}

func TestTrackingListWatchSource(t *testing.T) {
	out, err := NormalizeWatch(WatchInput{Source: WatchSource{Kind: WatchSourceTrackingList, StaleDays: 7},
		TrackedFields: []string{"status", "due_date", "status"},
		Followups:     &FollowupLadder{DateField: "due_date", Steps: []FollowupStep{{Name: "due", Offset: "0d", Notify: "owner"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Source.StaleDays != 7 || len(out.TrackedFields) != 2 || out.Followups == nil {
		t.Errorf("out = %+v", out)
	}
	_, err = NormalizeWatch(WatchInput{Source: WatchSource{Kind: WatchSourceWorkspaceFile, File: "watch/a.csv", Comments: true, Project: "X",
		JQL: "a", Lead: "b", IncludeDone: true}, IDField: "id", TrackedFields: []string{"a"}})
	verr, ok := err.(*ValidationError)
	if !ok || len(verr.Details) < 5 {
		t.Fatalf("err = %v", err)
	}
}

func TestSnapshotShapeIgnoresLeadAndStaleDays(t *testing.T) {
	src := WatchSource{Kind: WatchSourceTracker, Project: "P", Lead: "a", StaleDays: 3}
	w := Watch{Source: src, IDField: "key", TrackedFields: []string{"status"}}
	in := WatchInput{Source: WatchSource{Kind: WatchSourceTracker, Project: "P", Lead: "b", StaleDays: 9}, IDField: "key", TrackedFields: []string{"status"}}
	if !SameSnapshotShape(w, in) {
		t.Error("lead and stale days do not change the snapshot")
	}
	in.Source.Comments = true
	if SameSnapshotShape(w, in) {
		t.Error("comments change the snapshot")
	}
	if got := w.Input(); got.Source != src {
		t.Error("input keeps the source")
	}
}

func TestToggleAndCloudIDSettings(t *testing.T) {
	in := ConnectionInput{Name: "J", Kind: "jira", AreaIDs: []string{"11111111-1111-4111-8111-111111111111"},
		Config: map[string]string{"site_url": "https://a.atlassian.net", "email": "a@b.co", "allow_create": "TRUE", "cloud_id": "AAAAAAAA-1111-4111-8111-111111111111"}}
	out, err := NormalizeConnection(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Config["allow_create"] != "true" || out.Config["cloud_id"] != "aaaaaaaa-1111-4111-8111-111111111111" {
		t.Errorf("config = %v", out.Config)
	}
	in.Config = map[string]string{"site_url": "https://a.atlassian.net", "email": "a@b.co", "allow_create": "false"}
	if out, _ := NormalizeConnection(in); out.Config["allow_create"] != "" {
		t.Error("false is left out")
	}
	in.Config = map[string]string{"site_url": "https://a.atlassian.net", "email": "a@b.co", "allow_create": "maybe", "cloud_id": "nope"}
	if _, err := NormalizeConnection(in); err == nil || !strings.Contains(err.Error(), "true or false") || !strings.Contains(err.Error(), "cloud id") {
		t.Errorf("err = %v", err)
	}
	if !DestinationChanged("jira", map[string]string{"cloud_id": "a"}, map[string]string{"cloud_id": "b"}) {
		t.Error("a new cloud id moves the secrets")
	}
}

// Hidden characters (a right-to-left override, zero-width characters, a byte order mark, tag characters) are dropped from
// text that is posted or saved, so what a person reviews is what is sent; visible text, line breaks and tabs stay.
func TestHiddenCharactersAreDropped(t *testing.T) {
	in := "Pay\u202e01\u202c \u200bAcme\u200d\ufeff\U000E0041\U000E007F now\n\tok"
	if got := cleanText(in); got != "Pay01 Acme now\n\tok" {
		t.Errorf("cleanText = %q", got)
	}
	c, err := NormalizeTrackerComment(TrackerComment{Text: "\u200b\u202eHello"})
	if err != nil || c.Text != "Hello" {
		t.Errorf("comment = %q, %v", c.Text, err)
	}
	if _, err := NormalizeTrackerComment(TrackerComment{Text: "\u200b\u202e"}); err == nil {
		t.Error("a comment of hidden characters only was accepted")
	}
	s, err := NormalizeKM2Save(KM2Save{Title: "Ven\u202edors", Content: "# A\u200bcme", ExternalRef: "r"})
	if err != nil || s.Title != "Vendors" || s.Content != "# Acme" {
		t.Errorf("km2 = %+v, %v", s, err)
	}
	for _, r := range []rune{'a', 'é', '\n', ' ', '中'} {
		if IsHidden(r) {
			t.Errorf("%q counted as hidden", r)
		}
	}
}
