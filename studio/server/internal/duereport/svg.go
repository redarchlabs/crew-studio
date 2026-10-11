package duereport

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

// Static SVG charts for the HTML export, drawn to the dataviz mark specs: bars at most 24px thick with a 4px rounded
// data end and a square baseline end, a 2px surface gap between touching segments, 1px solid hairline axes, text in
// text colors (never the series color), a legend for two or more series, and a <title> on every mark. Every label is
// escaped; the data tables beside each chart carry the same numbers.

const (
	barThick = 18
	barGap   = 10
	radius   = 4
	surface  = "#ffffff"
	inkMuted = "#586373"
	inkMain  = "#1a1f26"
)

func esc(s string) string { return template.HTMLEscapeString(s) }

// barPath is a horizontal bar from x0 to x1 with a rounded right end (when round) and a square left end.
func barPath(x0, x1, y, h float64, round bool) string {
	w := x1 - x0
	if w <= 0 {
		return ""
	}
	r := float64(radius)
	if !round || w < r {
		return fmt.Sprintf("M%.1f %.1fh%.1fv%.1fh%.1fz", x0, y, w, h, -w)
	}
	return fmt.Sprintf("M%.1f %.1fh%.1fa%.0f %.0f 0 0 1 %.0f %.0fv%.1fa%.0f %.0f 0 0 1 %.0f %.0fh%.1fz",
		x0, y, w-r, r, r, r, r, h-2*r, r, r, -r, r, -(w - r))
}

// columnPath is a vertical bar rising from the baseline y0 to height h with a rounded top.
func columnPath(x, y0, w, h float64) string {
	if h <= 0 {
		return ""
	}
	r := float64(radius)
	if h < r {
		return fmt.Sprintf("M%.1f %.1fv%.1fh%.1fv%.1fz", x, y0, -h, w, h)
	}
	return fmt.Sprintf("M%.1f %.1fv%.1fa%.0f %.0f 0 0 1 %.0f %.0fh%.1fa%.0f %.0f 0 0 1 %.0f %.0fv%.1fz",
		x, y0, -(h - r), r, r, r, -r, w-2*r, r, r, r, r, h-r)
}

func svgOpen(w, h int, label string) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="100%%" viewBox="0 0 %d %d" role="img" aria-label="%s" style="max-width:%dpx;display:block">`,
		w, h, esc(label), w)
}

func legend(entries [][2]string) string {
	var b strings.Builder
	b.WriteString(`<div style="font-size:12px;color:#586373;margin:4px 0 8px">`)
	for _, e := range entries {
		fmt.Fprintf(&b, `<span style="display:inline-block;margin-right:14px"><span style="display:inline-block;width:10px;height:10px;border-radius:2px;background:%s;margin-right:5px;vertical-align:-1px"></span>%s</span>`, e[0], esc(e[1]))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// workloadSVG is a horizontal stacked bar per person: overdue (critical) then due within 7 days (warning), with the
// total at the bar end. At most 8 people; the rest are folded into "Other".
func workloadSVG(people []Person) template.HTML {
	rows := foldPeople(people, 8)
	if len(rows) == 0 {
		return template.HTML(`<p class="muted">Nobody has overdue work or work due this week.</p>`)
	}
	maxTotal := 0
	for _, p := range rows {
		maxTotal = max(maxTotal, p.Overdue+p.Due7)
	}
	const width, labelW, endW = 640, 150, 40
	plot := float64(width - labelW - endW)
	h := len(rows)*(barThick+barGap) + barGap
	var b strings.Builder
	b.WriteString(legend([][2]string{{colorCritical, "Overdue"}, {colorWarning, "Due within 7 days"}}))
	b.WriteString(svgOpen(width, h, "Workload by person: overdue and due within 7 days"))
	fmt.Fprintf(&b, `<line x1="%d" y1="0" x2="%d" y2="%d" stroke="%s" stroke-width="1"/>`, labelW, labelW, h, colorAxis)
	for i, p := range rows {
		y := float64(barGap + i*(barThick+barGap))
		fmt.Fprintf(&b, `<text x="%d" y="%.0f" font-size="12" fill="%s" text-anchor="end">%s</text>`, labelW-8, y+barThick-5, inkMain, esc(shorten(p.Name, 22)))
		x := float64(labelW)
		total := p.Overdue + p.Due7
		for j, part := range []struct {
			n     int
			color string
			label string
		}{{p.Overdue, colorCritical, "overdue"}, {p.Due7, colorWarning, "due within 7 days"}} {
			if part.n == 0 {
				continue
			}
			w := plot * float64(part.n) / float64(maxTotal)
			last := j == 1 || p.Due7 == 0
			end := x + w
			if !last {
				end -= 2 // the 2px surface gap between segments
			}
			fmt.Fprintf(&b, `<path d="%s" fill="%s"><title>%s: %d %s</title></path>`, barPath(x, end, y, barThick, last), part.color, esc(p.Name), part.n, part.label)
			x += w
		}
		fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-size="12" fill="%s">%d</text>`, x+6, y+barThick-5, inkMuted, total)
	}
	b.WriteString(`</svg>`)
	b.WriteString(`<table class="t" style="margin-top:8px"><tr><th>Person</th><th>Overdue</th><th>Due within 7 days</th></tr>`)
	for _, p := range rows {
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%d</td><td>%d</td></tr>`, esc(p.Name), p.Overdue, p.Due7)
	}
	b.WriteString(`</table>`)
	return template.HTML(b.String()) //nolint:gosec // built from escaped text and numbers
}

// foldPeople keeps the people with overdue or soon-due work, the busiest first, folding the tail into "Other".
func foldPeople(people []Person, keep int) []Person {
	var busy []Person
	for _, p := range people {
		if p.Overdue+p.Due7 > 0 {
			busy = append(busy, p)
		}
	}
	if len(busy) <= keep {
		return busy
	}
	other := Person{Name: "Other"}
	for _, p := range busy[keep-1:] {
		other.Overdue += p.Overdue
		other.Due7 += p.Due7
		other.Open += p.Open
	}
	return append(busy[:keep-1:keep-1], other)
}

// timelineSVG is a column per day for the next 14 days (one series, no legend), with the peak labeled and today
// marked on the axis.
func timelineSVG(days []Day) template.HTML {
	if len(days) == 0 {
		return ""
	}
	peak, peakAt := 0, -1
	for i, d := range days {
		if d.Count > peak {
			peak, peakAt = d.Count, i
		}
	}
	if peak == 0 {
		return template.HTML(`<p class="muted">Nothing is due in the next 14 days.</p>`)
	}
	const width, height, top, bottom, left = 640, 180, 20, 30, 28
	plotH := float64(height - top - bottom)
	slot := float64(width-left) / float64(len(days))
	colW := min(24, slot-6)
	base := float64(height - bottom)
	var b strings.Builder
	b.WriteString(svgOpen(width, height, "Items due on each of the next 14 days"))
	scaleMax := max(peak, 1)
	for _, v := range []int{0, scaleMax} {
		y := base - plotH*float64(v)/float64(scaleMax)
		fmt.Fprintf(&b, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="%s" stroke-width="1"/><text x="%d" y="%.1f" font-size="11" fill="%s" text-anchor="end">%d</text>`,
			left, y, width, y, colorGrid, left-6, y+4, inkMuted, v)
	}
	for i, d := range days {
		x := float64(left) + slot*float64(i) + (slot-colW)/2
		h := plotH * float64(d.Count) / float64(scaleMax)
		date, _ := time.Parse(time.DateOnly, d.Date)
		label := date.Format("2 Jan")
		fmt.Fprintf(&b, `<path d="%s" fill="%s"><title>%s: %d due</title></path>`, columnPath(x, base, colW, h), colorSeries, esc(label), d.Count)
		if i == 0 || i%2 == 0 || i == peakAt {
			weight := "400"
			if i == 0 {
				label, weight = "Today", "600"
			}
			fmt.Fprintf(&b, `<text x="%.1f" y="%d" font-size="11" fill="%s" font-weight="%s" text-anchor="middle">%s</text>`, x+colW/2, height-10, inkMuted, weight, esc(label))
		}
		if i == peakAt {
			fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="12" fill="%s" font-weight="600" text-anchor="middle">%d</text>`, x+colW/2, base-h-6, inkMain, d.Count)
		}
	}
	b.WriteString(`</svg><table class="t" style="margin-top:8px"><tr><th>Date</th><th>Due</th></tr>`)
	for _, d := range days {
		if d.Count > 0 {
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%d</td></tr>`, esc(d.Date), d.Count)
		}
	}
	b.WriteString(`</table>`)
	return template.HTML(b.String()) //nolint:gosec // built from escaped text and numbers
}

// statusMixSVG is one 100% stacked bar of every tracked item by status, on an ordinal blue ramp (to do, in
// progress, done) with canceled in gray.
func statusMixSVG(m StatusMix) template.HTML {
	parts := []struct {
		n     int
		label string
	}{{m.ToDo, "To do"}, {m.InProgress, "In progress"}, {m.Done, "Done"}, {m.Canceled, "Canceled"}}
	total := m.ToDo + m.InProgress + m.Done + m.Canceled
	if total == 0 {
		return template.HTML(`<p class="muted">No tracked items.</p>`)
	}
	var entries [][2]string
	for i, p := range parts {
		entries = append(entries, [2]string{statusRamp[i], fmt.Sprintf("%s %d", p.label, p.n)})
	}
	const width = 640
	var b strings.Builder
	b.WriteString(legend(entries))
	b.WriteString(svgOpen(width, barThick+4, "Status mix of every tracked item"))
	x := 0.0
	last := -1
	for i, p := range parts {
		if p.n > 0 {
			last = i
		}
	}
	for i, p := range parts {
		if p.n == 0 {
			continue
		}
		w := float64(width) * float64(p.n) / float64(total)
		end := x + w
		if i != last {
			end -= 2
		}
		fmt.Fprintf(&b, `<path d="%s" fill="%s"><title>%s: %d</title></path>`, barPath(x, end, 2, barThick, i == last), statusRamp[i], p.label, p.n)
		x += w
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String()) //nolint:gosec // built from fixed labels and numbers
}

// sparklineSVG is a small 2px line of a KPI over the recent reports, in the de-emphasis gray with today's point in
// the series blue.
func sparklineSVG(label string, values []int) template.HTML {
	if len(values) < 2 {
		return ""
	}
	const w, h, pad = 120, 28, 4
	top := 1
	for _, v := range values {
		top = max(top, v)
	}
	step := float64(w-2*pad) / float64(len(values)-1)
	pts := make([]string, len(values))
	var lx, ly float64
	for i, v := range values {
		lx = pad + step*float64(i)
		ly = float64(h-pad) - float64(h-2*pad)*float64(v)/float64(top)
		pts[i] = fmt.Sprintf("%.1f,%.1f", lx, ly)
	}
	return template.HTML(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" role="img" aria-label="%s over the last %d reports: %s" style="display:block;margin-top:6px">`+
		`<polyline points="%s" fill="none" stroke="%s" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/>`+
		`<circle cx="%.1f" cy="%.1f" r="4" fill="%s" stroke="%s" stroke-width="2"/></svg>`,
		w, h, esc(label), len(values), intsText(values), strings.Join(pts, " "), colorMuted, lx, ly, colorSeries, surface)) //nolint:gosec // numbers and an escaped label
}

func intsText(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = fmt.Sprint(n)
	}
	return strings.Join(s, ", ")
}

func shorten(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
