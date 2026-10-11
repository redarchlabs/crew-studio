package duereport

import (
	"bytes"
	"fmt"
	"html/template"
	"regexp"
	"strings"
)

// The HTML export: one self-contained page (inline CSS, static SVG charts, no scripts, no event handlers, no remote
// assets), sent as the HTML part of report emails and offered as a download. Every value comes through html/template,
// which escapes text and attributes; links are kept only when SafeURL allows them. Email clients that drop SVG still
// get every number from the tables beside each chart.

// Colors follow the dataviz palette's light mode on a white page (validated: docs in the design note).
const (
	colorCritical = "#d03b3b" // status: critical (overdue)
	colorWarning  = "#fab219" // status: warning (due within 7 days)
	colorSeries   = "#2a78d6" // slot 1: the due timeline
	colorMuted    = "#898781" // de-emphasis (sparklines)
	colorGrid     = "#e1e0d9"
	colorAxis     = "#c3c2b7"
)

// Ordinal ramp (to do -> in progress -> done) and canceled in gray
var statusRamp = []string{"#86b6ef", "#2a78d6", "#104281", "#c3c2b7"}

type htmlView struct {
	Report    Report
	S         *Snapshot
	KPIs      []kpiTile
	Narrative template.HTML
	Sections  []htmlSection
	Workload  template.HTML
	Timeline  template.HTML
	StatusMix template.HTML
	MixRows   []mixRow
	Changes   []string
}

type kpiTile struct {
	Label, Value, Delta, DeltaClass string
	Spark                           template.HTML
}

type htmlSection struct {
	Title    string
	ExtraCol string
	Rows     []htmlRow
}

type htmlRow struct {
	Key, URL, Title, Assignee, Due, Status, Extra string
	Blocked                                       bool
}

type mixRow struct {
	Label string
	Count int
}

var page = template.Must(template.New("report").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:">
<title>{{.Report.Title}}</title>
<style>
body{margin:0;background:#f3f4f1;color:#1a1f26;font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
.wrap{max-width:920px;margin:0 auto;padding:24px}
.card{background:#fff;border:1px solid #dfe2dd;border-radius:10px;padding:18px 20px;margin:0 0 16px}
h1{font-size:22px;margin:0 0 6px}h2{font-size:15px;margin:0 0 10px}
.chip{display:inline-block;border:1px solid #dfe2dd;border-radius:999px;padding:1px 9px;font-size:12px;color:#586373;margin-right:6px}
.chip.dry{border-color:#a86b00;color:#a86b00}
.kpis{width:100%;border-collapse:separate;border-spacing:8px 0;margin:0 -8px}
.kpi{background:#fff;border:1px solid #dfe2dd;border-radius:10px;padding:12px;vertical-align:top;width:20%}
.kpi .l{font-size:12px;color:#586373}.kpi .v{font-size:28px;font-weight:600;line-height:1.2}
.kpi .d{font-size:12px}.up{color:#cc2f36}.down{color:#1d8a57}.flat{color:#586373}
.callout{border-left:4px solid #2f6fd6;background:#f6f7f5;padding:10px 14px;border-radius:6px}
table.t{width:100%;border-collapse:collapse;font-size:13px}
table.t th{text-align:left;color:#586373;font-weight:500;border-bottom:1px solid #dfe2dd;padding:4px 6px}
table.t td{border-bottom:1px solid #eef0ec;padding:5px 6px;vertical-align:top}
.key{font-family:ui-monospace,Menlo,monospace;font-size:12px;border:1px solid #c4c9c2;border-radius:5px;padding:0 5px;color:#1a1f26;text-decoration:none}
.pill{border-radius:999px;padding:0 8px;font-size:12px;white-space:nowrap}
.late{background:#fbe4e4;color:#a1262b}.blocked{background:#fbe4e4;color:#a1262b}
.muted{color:#8a94a1;font-size:12px}
</style></head><body><div class="wrap">
<div class="card"><h1>{{.Report.Title}}</h1>
<span class="chip">{{if eq .Report.Cadence "daily"}}Daily{{else}}Weekly{{end}} · {{.Report.Period}}</span>
{{if .Report.DryRun}}<span class="chip dry">dry run</span>{{end}}
{{if .S}}<span class="muted">Data as of {{.S.Today}} ({{.S.Timezone}})</span>{{end}}</div>
{{if .KPIs}}<table class="kpis" role="presentation"><tr>{{range .KPIs}}<td class="kpi"><div class="l">{{.Label}}</div><div class="v">{{.Value}}</div>
{{if .Delta}}<div class="d {{.DeltaClass}}">{{.Delta}} since last report</div>{{end}}{{.Spark}}</td>{{end}}</tr></table><div style="height:16px"></div>{{end}}
{{if .Narrative}}<div class="card"><h2>Summary</h2><div class="callout">{{.Narrative}}</div></div>{{end}}
{{if .S}}
<div class="card"><h2>Workload by person</h2>{{.Workload}}</div>
<div class="card"><h2>Due in the next 14 days</h2>{{.Timeline}}</div>
<div class="card"><h2>Status mix</h2>{{.StatusMix}}
<table class="t"><tr>{{range .MixRows}}<th>{{.Label}}</th>{{end}}</tr><tr>{{range .MixRows}}<td>{{.Count}}</td>{{end}}</tr></table></div>
{{range .Sections}}{{if .Rows}}<div class="card"><h2>{{.Title}}</h2><table class="t"><tr><th>Item</th><th>Title</th><th>Assignee</th><th>Due</th><th>Status</th>{{if .ExtraCol}}<th>{{.ExtraCol}}</th>{{end}}</tr>
{{range .Rows}}<tr><td>{{if .URL}}<a class="key" href="{{.URL}}">{{.Key}}</a>{{else}}<span class="key">{{.Key}}</span>{{end}}</td><td>{{.Title}}{{if .Blocked}} <span class="pill blocked">blocked</span>{{end}}</td><td>{{.Assignee}}</td><td>{{.Due}}</td><td>{{.Status}}</td>{{if .Extra}}<td><span class="pill late">{{.Extra}}</span></td>{{end}}</tr>{{end}}
</table></div>{{end}}{{end}}
{{if .Changes}}<div class="card"><h2>Since the last report</h2><ul>{{range .Changes}}<li>{{.}}</li>{{end}}</ul></div>{{end}}
{{if .S.Truncated}}<p class="muted">Some items were left out to keep the report short; the counts cover every item.</p>{{end}}
{{end}}
<p class="muted">Made by Crew Studio from the tracker's data and the crew's summary.</p>
</div></body></html>`))

// HTML renders the report as a self-contained page. Without a snapshot it shows the narrative only.
func HTML(r Report, s *Snapshot) (string, error) {
	v := htmlView{Report: r, S: s, Narrative: narrativeHTML(r.Narrative)}
	if s != nil {
		v.KPIs = tiles(s)
		v.Sections = htmlSections(s)
		v.Workload = workloadSVG(s.Sections.ByPerson)
		v.Timeline = timelineSVG(s.Timeline)
		v.StatusMix = statusMixSVG(s.StatusMix)
		v.MixRows = []mixRow{{"To do", s.StatusMix.ToDo}, {"In progress", s.StatusMix.InProgress}, {"Done", s.StatusMix.Done}, {"Canceled", s.StatusMix.Canceled}}
		v.Changes = changeLines(s.Changes)
	}
	var b bytes.Buffer
	if err := page.Execute(&b, v); err != nil {
		return "", fmt.Errorf("render report html: %w", err)
	}
	return b.String(), nil
}

func tiles(s *Snapshot) []kpiTile {
	type def struct {
		label string
		value *int
		delta func(*KPIs) *int
		pick  func(KPIs) *int
		// upIsBad colors a rise red
		upIsBad bool
	}
	ptr := func(v int) *int { return &v }
	defs := []def{
		{"Overdue", ptr(s.KPIs.Overdue), func(k *KPIs) *int { return ptr(k.Overdue) }, func(k KPIs) *int { return ptr(k.Overdue) }, true},
		{"Due within 7 days", ptr(s.KPIs.Due7), func(k *KPIs) *int { return ptr(k.Due7) }, func(k KPIs) *int { return ptr(k.Due7) }, true},
		{"Blocked", ptr(s.KPIs.Blocked), func(k *KPIs) *int { return ptr(k.Blocked) }, func(k KPIs) *int { return ptr(k.Blocked) }, true},
		{"Unassigned, due soon", ptr(s.KPIs.UnassignedSoon), func(k *KPIs) *int { return ptr(k.UnassignedSoon) }, func(k KPIs) *int { return ptr(k.UnassignedSoon) }, true},
		{"Done since last report", s.KPIs.DoneSincePrev, func(k *KPIs) *int { return k.DoneSincePrev }, func(k KPIs) *int { return k.DoneSincePrev }, false},
	}
	out := make([]kpiTile, 0, len(defs))
	for _, d := range defs {
		t := kpiTile{Label: d.label, Value: "—"}
		if d.value != nil {
			t.Value = fmt.Sprint(*d.value)
		}
		if s.Deltas != nil {
			if dv := d.delta(s.Deltas); dv != nil {
				t.Delta, t.DeltaClass = signed(*dv), "flat"
				if *dv != 0 && (*dv > 0) == d.upIsBad {
					t.DeltaClass = "up"
				} else if *dv != 0 {
					t.DeltaClass = "down"
				}
			}
		}
		var series []int
		for _, p := range s.Trend {
			if v := d.pick(p.KPIs); v != nil {
				series = append(series, *v)
			}
		}
		t.Spark = sparklineSVG(d.label, series)
		out = append(out, t)
	}
	return out
}

func htmlSections(s *Snapshot) []htmlSection {
	items := s.ItemsByKey()
	mk := func(key, extra string) htmlRow {
		it, _ := titleOf(items, key)
		return htmlRow{Key: it.Key, URL: SafeURL(it.URL), Title: it.Title, Assignee: orDash(it.Assignee), Due: orDash(it.DueDate),
			Status: it.Status, Extra: extra, Blocked: it.Blocked}
	}
	var overdue []htmlRow
	for _, l := range s.Sections.Overdue {
		overdue = append(overdue, mk(l.Key, fmt.Sprintf("%d days late", l.DaysLate)))
	}
	list := func(keys []string) []htmlRow {
		var out []htmlRow
		for _, k := range keys {
			out = append(out, mk(k, ""))
		}
		return out
	}
	return []htmlSection{
		{Title: "Overdue", ExtraCol: "Late", Rows: overdue},
		{Title: "Due today", Rows: list(s.Sections.DueToday)},
		{Title: "Due in the next 7 days", Rows: list(s.Sections.DueNext7)},
		{Title: "Blocked", Rows: list(s.Sections.Blocked)},
		{Title: "Unassigned and due within 14 days", Rows: list(s.Sections.UnassignedSoon)},
	}
}

func changeLines(c *Changes) []string {
	if c == nil {
		return nil
	}
	var out []string
	if len(c.NewlyOverdue) > 0 {
		out = append(out, "Newly overdue: "+strings.Join(c.NewlyOverdue, ", "))
	}
	for _, f := range c.Finished {
		out = append(out, fmt.Sprintf("Finished: %s %s", f.Key, f.Title))
	}
	for _, m := range c.Slipped {
		out = append(out, fmt.Sprintf("Slipped: %s from %s to %s", m.Key, m.From, m.To))
	}
	for _, m := range c.Reassigned {
		out = append(out, fmt.Sprintf("Reassigned: %s from %s to %s", m.Key, orDash(m.From), orDash(m.To)))
	}
	if len(c.New) > 0 {
		out = append(out, "New: "+strings.Join(c.New, ", "))
	}
	return out
}

var (
	boldRe   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	bulletRe = regexp.MustCompile(`^\s*[-*+]\s+`)
	numRe    = regexp.MustCompile(`^\s*\d+[.)]\s+`)
)

// narrativeHTML renders the model's Markdown as plain, safe HTML: headings, paragraphs, lists and bold. Everything
// is escaped first, so no markup or link in the text survives.
func narrativeHTML(md string) template.HTML {
	md = strings.TrimSpace(strings.ReplaceAll(md, "\r\n", "\n"))
	if md == "" {
		return ""
	}
	var b strings.Builder
	inline := func(s string) string {
		return boldRe.ReplaceAllString(template.HTMLEscapeString(s), "<strong>$1</strong>")
	}
	list := ""
	closeList := func() {
		if list != "" {
			b.WriteString("</" + list + ">")
			list = ""
		}
	}
	for _, para := range strings.Split(md, "\n\n") {
		for _, line := range strings.Split(para, "\n") {
			t := strings.TrimSpace(line)
			switch {
			case t == "":
			case strings.HasPrefix(t, "#"):
				closeList()
				b.WriteString("<h3>" + inline(strings.TrimLeft(t, "# ")) + "</h3>")
			case bulletRe.MatchString(t) || numRe.MatchString(t):
				kind := "ul"
				if numRe.MatchString(t) {
					kind = "ol"
				}
				if list != kind {
					closeList()
					b.WriteString("<" + kind + ">")
					list = kind
				}
				b.WriteString("<li>" + inline(numRe.ReplaceAllString(bulletRe.ReplaceAllString(t, ""), "")) + "</li>")
			default:
				closeList()
				b.WriteString("<p>" + inline(t) + "</p>")
			}
		}
		closeList()
	}
	return template.HTML(b.String()) //nolint:gosec // every piece above was escaped by HTMLEscapeString
}
