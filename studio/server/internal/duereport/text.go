package duereport

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// mdLinkRe finds Markdown links and images: [text](target) and ![alt](target)
	mdLinkRe = regexp.MustCompile(`!?\[([^\]]*)\]\(\s*<?((?:[^()\s<>]|\([^()]*\))*)>?(?:\s+"[^"]*")?\s*\)`)
	// autoLinkRe finds <scheme:...> autolinks
	autoLinkRe = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9+.-]*:[^>\s]*)>`)
)

// Narrative makes the model's Markdown safe to pass on: a link whose target is not http or https keeps only its
// text, and so does an image (reports carry no remote images). The model may have read hostile tracker text.
func Narrative(md string) string {
	md = mdLinkRe.ReplaceAllStringFunc(md, func(m string) string {
		parts := mdLinkRe.FindStringSubmatch(m)
		if strings.HasPrefix(m, "!") || SafeURL(parts[2]) == "" {
			return parts[1]
		}
		return m
	})
	return autoLinkRe.ReplaceAllStringFunc(md, func(m string) string {
		if SafeURL(m[1:len(m)-1]) == "" {
			return strings.Trim(m, "<>")
		}
		return m
	})
}

// Report is what the exports need of a stored report besides its snapshot.
type Report struct {
	Title   string
	Project string
	Cadence string
	Period  string
	DryRun  bool
	// Narrative is the model's Markdown: a summary, the critical path, risks and asks
	Narrative string
}

var mdEscaper = strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`, "|", `\|`, "#", `\#`)

// escapeMD keeps tracker text from turning into Markdown links, emphasis or HTML.
func escapeMD(s string) string { return mdEscaper.Replace(clean(s)) }

// mdLink is a key linked to its issue when the link is safe, plain otherwise.
func mdLink(it Item) string {
	if u := SafeURL(it.URL); u != "" {
		return fmt.Sprintf("[%s](<%s>)", escapeMD(it.Key), strings.NewReplacer("<", "%3C", ">", "%3E", " ", "%20").Replace(u))
	}
	return escapeMD(it.Key)
}

func signed(n int) string {
	if n > 0 {
		return fmt.Sprintf("+%d", n)
	}
	return fmt.Sprintf("%d", n)
}

// kpiLine is the headline counts with their change since the previous report.
func kpiLine(s *Snapshot, sep string) string {
	d := s.Deltas
	part := func(label string, v int, delta func(*KPIs) int) string {
		out := fmt.Sprintf("%s %d", label, v)
		if d != nil {
			out += fmt.Sprintf(" (%s)", signed(delta(d)))
		}
		return out
	}
	parts := []string{
		part("Overdue", s.KPIs.Overdue, func(k *KPIs) int { return k.Overdue }),
		part("Due within 7 days", s.KPIs.Due7, func(k *KPIs) int { return k.Due7 }),
		part("Blocked", s.KPIs.Blocked, func(k *KPIs) int { return k.Blocked }),
		part("Unassigned and due soon", s.KPIs.UnassignedSoon, func(k *KPIs) int { return k.UnassignedSoon }),
	}
	if s.KPIs.DoneSincePrev != nil {
		parts = append(parts, fmt.Sprintf("Done since last report %d", *s.KPIs.DoneSincePrev))
	}
	return strings.Join(parts, sep)
}

func titleOf(items map[string]Item, key string) (Item, bool) {
	it, ok := items[key]
	if !ok {
		it = Item{Key: key}
	}
	return it, ok
}

func row(it Item, extra string) string {
	cells := []string{mdLink(it), escapeMD(it.Title), escapeMD(orDash(it.Assignee)), escapeMD(orDash(it.DueDate)), escapeMD(it.Status)}
	if extra != "" {
		cells = append(cells, extra)
	}
	return "| " + strings.Join(cells, " | ") + " |"
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// Markdown is the whole report as Markdown, built from the data with the narrative: what KM2 keeps, so a KM2 search
// finds the facts and the model's commentary together.
func Markdown(r Report, s *Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", escapeMD(r.Title))
	if s == nil {
		b.WriteString(strings.TrimSpace(Narrative(r.Narrative)) + "\n")
		return b.String()
	}
	fmt.Fprintf(&b, "_%s · data as of %s (%s)_\n\n", escapeMD(cadenceWord(r.Cadence)), s.Today, escapeMD(s.Timezone))
	fmt.Fprintf(&b, "**%s**\n\n", kpiLine(s, " · "))
	if n := strings.TrimSpace(Narrative(r.Narrative)); n != "" {
		// No raw HTML from the narrative reaches KM2's Markdown renderer
		b.WriteString("## Summary\n\n" + strings.NewReplacer("<", `\<`, ">", `\>`).Replace(n) + "\n\n")
	}
	items := s.ItemsByKey()
	table := func(heading, extraHead string, rows []string) {
		if len(rows) == 0 {
			return
		}
		fmt.Fprintf(&b, "## %s\n\n| Item | Title | Assignee | Due | Status |%s\n|---|---|---|---|---|%s\n", heading, ifSet(extraHead, " "+extraHead+" |"), ifSet(extraHead, "---|"))
		b.WriteString(strings.Join(rows, "\n") + "\n\n")
	}
	var overdue []string
	for _, l := range s.Sections.Overdue {
		it, _ := titleOf(items, l.Key)
		overdue = append(overdue, row(it, fmt.Sprintf("%d", l.DaysLate)))
	}
	table("Overdue", "Days late", overdue)
	keys := func(list []string) []string {
		var out []string
		for _, k := range list {
			it, _ := titleOf(items, k)
			out = append(out, row(it, ""))
		}
		return out
	}
	table("Due today", "", keys(s.Sections.DueToday))
	table("Due in the next 7 days", "", keys(s.Sections.DueNext7))
	table("Blocked", "", keys(s.Sections.Blocked))
	table("Unassigned and due within 14 days", "", keys(s.Sections.UnassignedSoon))
	if len(s.Sections.ByPerson) > 0 {
		b.WriteString("## By person\n\n| Person | Overdue | Due within 7 days | Open |\n|---|---|---|---|\n")
		for _, p := range s.Sections.ByPerson {
			fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", escapeMD(p.Name), p.Overdue, p.Due7, p.Open)
		}
		b.WriteString("\n")
	}
	writeChanges(&b, s)
	if s.Truncated {
		b.WriteString("_Some items were left out to keep the report short; the counts cover every item._\n")
	}
	return b.String()
}

func ifSet(s, v string) string {
	if s == "" {
		return ""
	}
	return v
}

func cadenceWord(c string) string {
	if c == "daily" {
		return "Daily report"
	}
	return "Weekly report"
}

func writeChanges(b *strings.Builder, s *Snapshot) {
	c := s.Changes
	if c == nil {
		return
	}
	var lines []string
	add := func(label string, keys []string) {
		if len(keys) > 0 {
			esc := make([]string, len(keys))
			for i, k := range keys {
				esc[i] = escapeMD(k)
			}
			lines = append(lines, fmt.Sprintf("- %s: %s", label, strings.Join(esc, ", ")))
		}
	}
	add("Newly overdue", c.NewlyOverdue)
	var finished []string
	for _, f := range c.Finished {
		finished = append(finished, f.Key)
	}
	add("Finished", finished)
	for _, m := range c.Slipped {
		lines = append(lines, fmt.Sprintf("- Slipped: %s from %s to %s", escapeMD(m.Key), escapeMD(m.From), escapeMD(m.To)))
	}
	for _, m := range c.Reassigned {
		lines = append(lines, fmt.Sprintf("- Reassigned: %s from %s to %s", escapeMD(m.Key), escapeMD(orDash(m.From)), escapeMD(orDash(m.To))))
	}
	add("New", c.New)
	if len(lines) > 0 {
		b.WriteString("## Since the last report\n\n" + strings.Join(lines, "\n") + "\n\n")
	}
}

// SummaryOverdue is how many overdue items the chat summary names.
const SummaryOverdue = 5

// Summary is the compact report for Slack and Teams: the headline counts, the most overdue items, then the
// narrative. The senders cut it to their limits.
func Summary(r Report, s *Snapshot) string {
	if s == nil {
		return Narrative(r.Narrative)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**%s**\n\n", kpiLine(s, " · "))
	items := s.ItemsByKey()
	if len(s.Sections.Overdue) > 0 {
		b.WriteString("**Most overdue**\n\n")
		for i, l := range s.Sections.Overdue {
			if i == SummaryOverdue {
				fmt.Fprintf(&b, "- and %d more\n", len(s.Sections.Overdue)-SummaryOverdue)
				break
			}
			it, _ := titleOf(items, l.Key)
			fmt.Fprintf(&b, "- %s %s: %d days late (%s)\n", mdLink(it), escapeMD(it.Title), l.DaysLate, escapeMD(orDash(it.Assignee)))
		}
		b.WriteString("\n")
	}
	if n := strings.TrimSpace(Narrative(r.Narrative)); n != "" {
		b.WriteString(n + "\n")
	}
	return b.String()
}
