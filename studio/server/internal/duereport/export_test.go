package duereport

import (
	"regexp"
	"strings"
	"testing"

	"crewstudio/internal/model"
	"crewstudio/internal/tracker"
)

// hostile is a project whose tracker text tries to inject markup through titles, names, labels and links.
func hostile() *Snapshot {
	issues := project()
	issues[1].Issue.Title = `<script>alert("t")</script>Vendor <img src=x onerror="alert(1)">`
	issues[1].Issue.Assignee = &tracker.Person{AccountID: "a", Name: `Sam <svg onload=alert(2)>`}
	issues[1].Issue.URL = "javascript:alert(3)"
	issues[1].Issue.Labels = []string{"blocked", `"><script>x</script>`}
	issues[3].Issue.Key = `ERP-4"><b>`
	s := Make(Input{Project: `ERP <i>`, Cadence: "daily", Location: denver, Now: now, Issues: issues})
	return &s
}

var report = Report{Title: "ERP daily report, 2026-10-06", Project: "ERP", Cadence: "daily", Period: "2026-10-06",
	Narrative: "**On track** apart from the vendor.\n\n- Critical path: ERP-5\n- Ask: <script>x</script> [link](javascript:alert(4))"}

func TestHTMLExportIsSafeToOpen(t *testing.T) {
	out, err := HTML(report, hostile())
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(out)
	// Hostile text may appear, escaped, as text; it must never become a tag, an attribute or a link
	for _, bad := range []string{"<script", "<img", "<svg onload", "<b>", "<i>", "@import", "url(http"} {
		if strings.Contains(lower, bad) {
			t.Errorf("export contains %q", bad)
		}
	}
	if regexp.MustCompile(`(?i)<[^>]*\son[a-z]+\s*=`).MatchString(out) {
		t.Error("export has an event handler attribute")
	}
	if regexp.MustCompile(`(?i)(href|src|xlink:href)\s*=\s*"\s*(javascript|data|vbscript):`).MatchString(out) {
		t.Error("export has a script link")
	}
	if regexp.MustCompile(`(?i)(src|href)\s*=\s*"(https?:)?//`).FindString(out) != "" && !strings.Contains(out, `href="https://km2.example.com`) {
		t.Error("export fetches a remote asset")
	}
	for _, want := range []string{"&lt;script&gt;", "Sam &lt;svg onload=alert(2)&gt;", `<strong>On track</strong>`, "<li>Critical path: ERP-5</li>",
		"Content-Security-Policy", "Workload by person", `aria-label="Items due on each of the next 14 days"`, "3 days late", "Daily · 2026-10-06"} {
		if !strings.Contains(out, want) {
			t.Errorf("export lacks %q", want)
		}
	}
	// The hostile item is listed without a link; safe links stay
	if strings.Contains(out, `href="javascript`) || !strings.Contains(out, `href="https://km2.example.com/r?id=ERP-5"`) {
		t.Error("links were not filtered")
	}
	if strings.Contains(out, "<link") || strings.Contains(out, "<iframe") || strings.Contains(out, "<object") {
		t.Error("export embeds other documents")
	}
}

func TestHTMLWithoutSnapshotShowsTheNarrative(t *testing.T) {
	out, err := HTML(Report{Title: "Atlas status, 2026-W41", Cadence: "weekly", Period: "2026-W41", DryRun: true, Narrative: "All good."}, nil)
	if err != nil || !strings.Contains(out, "<p>All good.</p>") || !strings.Contains(out, "dry run") || strings.Contains(out, "Workload") {
		t.Errorf("html = %s %v", out, err)
	}
	if narrativeHTML("  ") != "" {
		t.Error("empty narrative rendered")
	}
	if got := string(narrativeHTML("# Head\n1. one\n2. two\nplain")); got != "<h3>Head</h3><ol><li>one</li><li>two</li></ol><p>plain</p>" {
		t.Errorf("narrative = %s", got)
	}
}

func TestMarkdownAndSummaryEscapeTrackerText(t *testing.T) {
	s := hostile()
	md := Markdown(report, s)
	for _, want := range []string{"# ERP daily report, 2026-10-06", "**Overdue 1 · Due within 7 days 3 · Blocked 1 · Unassigned and due soon 1**",
		"## Summary", "## Overdue", "\\<script\\>", "## By person", "[ERP-5](<https://km2.example.com/r?id=ERP-5>)"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "](javascript") || strings.Contains(md, "<script>") {
		t.Errorf("markdown kept a hostile link or tag:\n%s", md)
	}
	sum := Summary(report, s)
	if !strings.Contains(sum, "**Most overdue**") || !strings.Contains(sum, "3 days late") || !strings.HasSuffix(strings.TrimSpace(sum), "x</script> link") {
		t.Errorf("summary = %s", sum)
	}
	if Summary(report, nil) != Narrative(report.Narrative) || !strings.HasPrefix(Markdown(report, nil), "# ERP daily report") {
		t.Error("without a snapshot the narrative is sent as it is")
	}
	// The overdue list in the summary stops after five, saying how many more
	issues := project()
	for i := 0; i < 8; i++ {
		issues = append(issues, Tracked{Issue: issue("LATE-"+string(rune('a'+i)), "2026-09-20", model.StatusToDo, "Kim")})
	}
	many := Make(Input{Project: "ERP", Cadence: "daily", Location: denver, Now: now, Issues: issues})
	if got := Summary(report, &many); !strings.Contains(got, "- and 4 more") {
		t.Errorf("summary of many = %s", got)
	}
	prev := Make(Input{Project: "ERP", Cadence: "daily", Location: denver, Now: now.AddDate(0, 0, -1), Issues: project()})
	withPrev := Make(Input{Project: "ERP", Cadence: "daily", Location: denver, Now: now, Issues: issues, History: []Previous{{ReportID: "p", Snapshot: prev}}})
	if got := Markdown(report, &withPrev); !strings.Contains(got, "Overdue 9 (+8)") || !strings.Contains(got, "## Since the last report") ||
		!strings.Contains(got, "Done since last report 0") {
		t.Errorf("markdown with deltas = %s", got)
	}
	if out, err := HTML(report, &withPrev); err != nil || !strings.Contains(out, "8 since last report") || !strings.Contains(out, "Since the last report") {
		t.Errorf("html with deltas: %v", err)
	}
}

func TestChartsHandleEmptyData(t *testing.T) {
	if !strings.Contains(string(workloadSVG(nil)), "Nobody has overdue work") || !strings.Contains(string(statusMixSVG(StatusMix{})), "No tracked items") {
		t.Error("empty charts should say so")
	}
	if !strings.Contains(string(timelineSVG([]Day{{Date: "2026-10-06"}})), "Nothing is due in the next 14 days") {
		t.Error("an empty timeline should say so")
	}
	if timelineSVG(nil) != "" || sparklineSVG("x", []int{3}) != "" {
		t.Error("charts without enough data should be empty")
	}
	people := []Person{}
	for i := 0; i < 12; i++ {
		people = append(people, Person{Name: "P" + string(rune('a'+i)), Overdue: 1, Due7: 1})
	}
	if got := foldPeople(people, 8); len(got) != 8 || got[7].Name != "Other" || got[7].Overdue != 5 {
		t.Errorf("folded = %+v", got)
	}
	if barPath(5, 5, 0, 10, true) != "" || columnPath(0, 0, 10, 0) != "" || !strings.Contains(barPath(0, 2, 0, 10, true), "h2.0v10.0") {
		t.Error("degenerate bars")
	}
}

func TestNarrativeKeepsOnlySafeLinks(t *testing.T) {
	in := "See [the board](https://km2.example.com/b) and [x](javascript:alert(1)), ![img](https://evil.example/p.png), <https://ok.example>, <javascript:alert(2)> and <b>bold</b>."
	got := Narrative(in)
	want := "See [the board](https://km2.example.com/b) and x, img, <https://ok.example>, javascript:alert(2) and <b>bold</b>."
	if got != want {
		t.Errorf("narrative = %q", got)
	}
}
