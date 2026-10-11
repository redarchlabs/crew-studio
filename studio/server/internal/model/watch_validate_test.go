package model

import (
	"strings"
	"testing"
)

const connID = "11111111-0000-4000-8000-000000000001"

func httpWatch() WatchInput {
	return WatchInput{
		ConnectionID:  ptr(connID),
		Source:        WatchSource{Kind: WatchSourceHTTPJSON, Path: "/issues?state=open", ItemsPath: "data.items"},
		IDField:       "key",
		TrackedFields: []string{"status", "fields.due_date"},
	}
}

func TestNormalizeWatchDefaults(t *testing.T) {
	out, err := NormalizeWatch(httpWatch())
	if err != nil {
		t.Fatal(err)
	}
	if out.RunWhen != WatchRunChanged || out.DryRun == nil || !*out.DryRun || out.MaxItems != DefaultWatchMaxItems {
		t.Errorf("defaults not applied: %+v", out)
	}
	in := httpWatch()
	in.Source.ItemsPath = "$.data.items"
	in.TrackedFields = []string{" status ", "status", "$.owner"}
	out, err = NormalizeWatch(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Source.ItemsPath != "data.items" || strings.Join(out.TrackedFields, ",") != "status,owner" {
		t.Errorf("paths not cleaned: %+v", out)
	}
}

func TestNormalizeWatchProblems(t *testing.T) {
	tests := []struct {
		name string
		edit func(*WatchInput)
		want string
	}{
		{"unknown kind", func(w *WatchInput) { w.Source.Kind = "jira" }, "source.kind"},
		{"no connection", func(w *WatchInput) { w.ConnectionID = nil }, "connection_id: is required"},
		{"bad connection", func(w *WatchInput) { w.ConnectionID = ptr("x") }, "connection_id: must be a UUID"},
		{"no path", func(w *WatchInput) { w.Source.Path = "" }, "source.path"},
		{"absolute url path", func(w *WatchInput) { w.Source.Path = "http://evil/x" }, "source.path"},
		{"scheme-relative path", func(w *WatchInput) { w.Source.Path = "//evil/x" }, "source.path"},
		{"path with space", func(w *WatchInput) { w.Source.Path = "/a b" }, "source.path"},
		{"dot dot", func(w *WatchInput) { w.Source.Path = "/issues/../../admin" }, "'..' segments"},
		{"encoded dot dot", func(w *WatchInput) { w.Source.Path = "/issues/%2e%2E/admin?x=1" }, "'..' segments"},
		{"encoded slash", func(w *WatchInput) { w.Source.Path = "/a%2F..%2fadmin" }, "encoded"},
		{"bad escape", func(w *WatchInput) { w.Source.Path = "/a%zz" }, "source.path"},
		{"no id field", func(w *WatchInput) { w.IDField = "" }, "id_field: is required"},
		{"bad id path", func(w *WatchInput) { w.IDField = "a..b" }, "id_field"},
		{"no tracked", func(w *WatchInput) { w.TrackedFields = nil }, "tracked_fields: list at least one"},
		{"bad run_when", func(w *WatchInput) { w.RunWhen = "sometimes" }, "run_when"},
		{"max items", func(w *WatchInput) { w.MaxItems = MaxWatchMaxItems + 1 }, "max_items"},
		{"negative max items", func(w *WatchInput) { w.MaxItems = -1 }, "max_items"},
		{"url on http", func(w *WatchInput) { w.Source.URL = "https://x" }, "source.url: only applies"},
		{"too many tracked", func(w *WatchInput) {
			w.TrackedFields = nil
			for i := 0; i < 40; i++ {
				w.TrackedFields = append(w.TrackedFields, "f"+strings.Repeat("x", i))
			}
		}, "tracked_fields: at most"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := httpWatch()
			tt.edit(&in)
			_, err := NormalizeWatch(in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNormalizeWebPageWatch(t *testing.T) {
	in := WatchInput{Source: WatchSource{Kind: WatchSourceWebPage, URL: " https://example.com/status ", Selector: " main "}}
	out, err := NormalizeWatch(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.IDField != "url" || strings.Join(out.TrackedFields, ",") != "text" || out.Source.URL != "https://example.com/status" || out.Source.Selector != "main" {
		t.Errorf("web page not normalized: %+v", out)
	}
	for _, bad := range []WatchInput{
		{Source: WatchSource{Kind: WatchSourceWebPage, URL: "ftp://x/y"}},
		{Source: WatchSource{Kind: WatchSourceWebPage, URL: "https://user:pw@x/y"}},
		{Source: WatchSource{Kind: WatchSourceWebPage, URL: ""}},
		{Source: WatchSource{Kind: WatchSourceWebPage, URL: "https://x/"}, ConnectionID: ptr(connID)},
		{Source: WatchSource{Kind: WatchSourceWebPage, URL: "https://x/", Selector: strings.Repeat("a", 600)}},
		{Source: WatchSource{Kind: WatchSourceWebPage, URL: "https://x/"}, Followups: &FollowupLadder{DateField: "x"}},
	} {
		if _, err := NormalizeWatch(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestNormalizeWorkspaceFileWatch(t *testing.T) {
	in := WatchInput{Source: WatchSource{Kind: WatchSourceWorkspaceFile, File: "./watch//courses.CSV"}, IDField: "Course ID", TrackedFields: []string{"Due date"}}
	out, err := NormalizeWatch(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Source.File != "watch/courses.CSV" || out.Source.Format != "csv" {
		t.Errorf("file not normalized: %+v", out.Source)
	}
	in = WatchInput{Source: WatchSource{Kind: WatchSourceWorkspaceFile, File: "watch/list.json", ItemsPath: "items"}, IDField: "id", TrackedFields: []string{"due"}}
	if out, err = NormalizeWatch(in); err != nil || out.Source.Format != "json" {
		t.Fatalf("json file: %+v %v", out, err)
	}
	for name, src := range map[string]WatchSource{
		"escape":        {Kind: WatchSourceWorkspaceFile, File: "../secrets.json"},
		"absolute":      {Kind: WatchSourceWorkspaceFile, File: "/etc/passwd.json"},
		"run folder":    {Kind: WatchSourceWorkspaceFile, File: "runs/abc/out.json"},
		"no extension":  {Kind: WatchSourceWorkspaceFile, File: "watch/list"},
		"outside watch": {Kind: WatchSourceWorkspaceFile, File: "inbox/list.csv"},
		"watch climb":   {Kind: WatchSourceWorkspaceFile, File: "watch/../runs/x.csv"},
		"bad format":    {Kind: WatchSourceWorkspaceFile, File: "watch/list.json", Format: "xml"},
		"csv items":     {Kind: WatchSourceWorkspaceFile, File: "watch/a.csv", ItemsPath: "x"},
		"empty":         {Kind: WatchSourceWorkspaceFile},
	} {
		in := WatchInput{Source: src, IDField: "id", TrackedFields: []string{"due"}}
		if _, err := NormalizeWatch(in); err == nil {
			t.Errorf("%s: accepted %+v", name, src)
		}
	}
}

func ladder() *FollowupLadder {
	return &FollowupLadder{
		DateField: "fields.due_date",
		SkipWhen:  map[string][]string{"status": {"Done", " Canceled "}},
		Steps: []FollowupStep{
			{Name: "due_soon", Offset: "-3d", Notify: "owner"},
			{Name: "overdue_weekly", Offset: "+9d", Notify: "lead", Repeat: "1w"},
		},
		QuietDays: []string{"saturday", "Sun"},
		Timezone:  "America/Denver",
	}
}

func TestNormalizeLadder(t *testing.T) {
	in := httpWatch()
	in.Followups = ladder()
	out, err := NormalizeWatch(in)
	if err != nil {
		t.Fatal(err)
	}
	l := out.Followups
	if strings.Join(l.QuietDays, ",") != "Sat,Sun" || l.MaxPerTick != DefaultFollowupsPerTick || l.SkipWhen["status"][1] != "Canceled" {
		t.Errorf("ladder not normalized: %+v", l)
	}
	tests := []struct {
		name string
		edit func(*FollowupLadder)
		want string
	}{
		{"date not tracked", func(l *FollowupLadder) { l.DateField = "created" }, "followups.date_field"},
		{"no steps", func(l *FollowupLadder) { l.Steps = nil }, "followups.steps"},
		{"bad offset", func(l *FollowupLadder) { l.Steps[0].Offset = "3 days" }, "offset"},
		{"bad repeat", func(l *FollowupLadder) { l.Steps[1].Repeat = "0d" }, "repeat"},
		{"bad name", func(l *FollowupLadder) { l.Steps[0].Name = "Due Soon!" }, "name"},
		{"duplicate name", func(l *FollowupLadder) { l.Steps[1].Name = "due_soon" }, "more than once"},
		{"no notify", func(l *FollowupLadder) { l.Steps[0].Notify = " " }, "notify"},
		{"bad quiet day", func(l *FollowupLadder) { l.QuietDays = []string{"Funday"} }, "quiet_days"},
		{"bad zone", func(l *FollowupLadder) { l.Timezone = "Mars/Base" }, "timezone"},
		{"skip not tracked", func(l *FollowupLadder) { l.SkipWhen = map[string][]string{"owner": {"x"}} }, "skip_when"},
		{"cap", func(l *FollowupLadder) { l.MaxPerTick = MaxFollowupsPerTick + 1 }, "max_per_tick"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := httpWatch()
			in.Followups = ladder()
			tt.edit(in.Followups)
			_, err := NormalizeWatch(in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseDays(t *testing.T) {
	for text, want := range map[string]int{"-3d": -3, "0d": 0, "+2d": 2, "9d": 9, "2w": 14, "-1w": -7} {
		got, err := ParseDayOffset(text)
		if err != nil || got != want {
			t.Errorf("ParseDayOffset(%q) = %d, %v", text, got, err)
		}
	}
	for _, bad := range []string{"", "3", "d", "+-3d", "99999d", "3h"} {
		if _, err := ParseDayOffset(bad); err == nil {
			t.Errorf("ParseDayOffset(%q) accepted", bad)
		}
	}
	if d, err := ParseRepeatDays("7d"); err != nil || d != 7 {
		t.Errorf("repeat 7d = %d, %v", d, err)
	}
	if _, err := ParseRepeatDays("-7d"); err == nil {
		t.Error("negative repeat accepted")
	}
}

func TestSameSnapshotShape(t *testing.T) {
	w := Watch{ConnectionID: ptr(connID), Source: httpWatch().Source, IDField: "key", TrackedFields: []string{"status", "fields.due_date"}}
	in := httpWatch()
	if !SameSnapshotShape(w, in) {
		t.Error("same shape reported different")
	}
	in.TrackedFields = []string{"status"}
	if SameSnapshotShape(w, in) {
		t.Error("fewer fields reported same")
	}
	in = httpWatch()
	in.ConnectionID = nil
	if SameSnapshotShape(w, in) {
		t.Error("other connection reported same")
	}
	if _, ok := WatchSourceKindOf("nope"); ok {
		t.Error("unknown kind found")
	}
}
