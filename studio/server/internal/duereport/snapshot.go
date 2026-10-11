// Package duereport turns a project's tracked items into the data of a designed report (docs/crew-state-and-pmo-
// design.md, section 12): Crew Studio, not the model, works out what is overdue, due soon, blocked, unassigned and
// changed since the last report, and keeps that snapshot with the report so the page, its charts, its exports and
// later trends are computed from facts. The model's Markdown becomes the narrative around them.
//
// Everything that comes from a tracker (titles, names, keys, labels, links) is untrusted text: renderers escape it,
// and links are kept only when they are http or https.
package duereport

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"crewstudio/internal/model"
	"crewstudio/internal/tracker"
)

// Version is the snapshot format.
const Version = 1

// Limits keep a snapshot small, whatever the size of the project.
const (
	// MaxItems is the most open items a snapshot keeps (those named in a section first, then by due date)
	MaxItems = 400
	// MaxSectionRows bounds each section and change list
	MaxSectionRows = 100
	// MaxPeople bounds the by-person list
	MaxPeople = 50
	// MaxBytes is the largest snapshot stored; a bigger one keeps fewer items, and past that none is stored
	MaxBytes = 512 << 10
	// TimelineDays is how many days ahead the due timeline covers, today included
	TimelineDays = 14
	// TrendPoints is how many reports (today's included) the trend covers
	TrendPoints = 14
	// SoonDays is "due soon": within a week
	SoonDays = 7
	// UnassignedDays is how far ahead an unassigned item counts as a problem
	UnassignedDays = 14
	maxText        = 300
)

// Item is one open tracked item.
type Item struct {
	Key            string   `json:"key"`
	Title          string   `json:"title"`
	URL            string   `json:"url,omitempty"`
	Assignee       string   `json:"assignee,omitempty"`
	Status         string   `json:"status"`
	StatusCategory string   `json:"status_category"`
	DueDate        string   `json:"due_date,omitempty"`
	Labels         []string `json:"labels"`
	Blocked        bool     `json:"blocked"`
	Parent         string   `json:"parent,omitempty"`
	// Epic is the tracking-list item the item sits under
	Epic      string `json:"epic,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// KPIs are the headline counts.
type KPIs struct {
	Open           int `json:"open"`
	Overdue        int `json:"overdue"`
	DueToday       int `json:"due_today"`
	Due7           int `json:"due_7"`
	Blocked        int `json:"blocked"`
	UnassignedSoon int `json:"unassigned_soon"`
	// DoneSincePrev is nil on a project's first report
	DoneSincePrev *int `json:"done_since_prev"`
}

// Late is an overdue item and how many days late it is.
type Late struct {
	Key      string `json:"key"`
	DaysLate int    `json:"days_late"`
}

// Person is one assignee's open work.
type Person struct {
	Name    string `json:"name"`
	Open    int    `json:"open"`
	Overdue int    `json:"overdue"`
	Due7    int    `json:"due_7"`
}

// Sections are the report's lists of keys (items holds the details).
type Sections struct {
	Overdue        []Late   `json:"overdue"`
	DueToday       []string `json:"due_today"`
	DueNext7       []string `json:"due_next_7"`
	Blocked        []string `json:"blocked"`
	UnassignedSoon []string `json:"unassigned_soon"`
	ByPerson       []Person `json:"by_person"`
}

// Move is a field that changed on an item, from one value to another.
type Move struct {
	Key   string `json:"key"`
	Title string `json:"title,omitempty"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// Finished is an item done (or canceled) since the previous report.
type Finished struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// Changes are what moved since the previous report of the same cadence.
type Changes struct {
	NewlyOverdue []string   `json:"newly_overdue"`
	Finished     []Finished `json:"finished"`
	Slipped      []Move     `json:"slipped"`
	Reassigned   []Move     `json:"reassigned"`
	New          []string   `json:"new"`
}

// Day is how many open items are due on a date.
type Day struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// StatusMix counts every tracked item by status category.
type StatusMix struct {
	ToDo       int `json:"to_do"`
	InProgress int `json:"in_progress"`
	Done       int `json:"done"`
	Canceled   int `json:"canceled"`
}

// Point is one report's KPIs, for trends.
type Point struct {
	Date string `json:"date"`
	KPIs
}

// Snapshot is a project's tracked work when a report was delivered.
type Snapshot struct {
	Version     int       `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	Timezone    string    `json:"timezone"`
	// Today is the local date in the schedule's time zone
	Today   string `json:"today"`
	Cadence string `json:"cadence"`
	Project string `json:"project"`
	// PreviousReportID is the report the deltas and changes compare with ("" for a first report)
	PreviousReportID string `json:"previous_report_id,omitempty"`
	// Truncated is true when items were left out to stay within the limits (the counts still cover everything)
	Truncated bool      `json:"truncated"`
	Items     []Item    `json:"items"`
	KPIs      KPIs      `json:"kpis"`
	Deltas    *KPIs     `json:"deltas"`
	Sections  Sections  `json:"sections"`
	Changes   *Changes  `json:"changes"`
	Timeline  []Day     `json:"timeline"`
	StatusMix StatusMix `json:"status_mix"`
	Trend     []Point   `json:"trend"`
}

// Previous is an earlier report's snapshot.
type Previous struct {
	ReportID string
	Snapshot Snapshot
}

// Input is what a snapshot is made from.
type Input struct {
	Project  string
	Cadence  string
	Location *time.Location
	Now      time.Time
	// Issues are every tracked item and its children (done ones too), each with the tracking-list item it sits under
	Issues []Tracked
	// History are earlier snapshots of the same project and cadence, newest first; the first is the previous report
	History []Previous
}

// Tracked is a tracker issue with the tracking-list item (epic) it was read under.
type Tracked struct {
	Issue tracker.Issue
	Epic  string
}

// SafeURL keeps a link only when it is an absolute http or https URL, so a tracker cannot hand the report a
// javascript: or data: link.
func SafeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}

// clean makes tracker text one line without control characters, at most maxText characters.
func clean(s string) string {
	s = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, s)), " ")
	if r := []rune(s); len(r) > maxText {
		s = string(r[:maxText]) + "…"
	}
	return s
}

// Make builds the snapshot: the open items, the counts and sections as of today in the schedule's time zone, and
// what changed since the previous report.
func Make(in Input) Snapshot {
	loc := in.Location
	if loc == nil {
		loc = time.UTC
	}
	today := in.Now.In(loc).Format(time.DateOnly)
	s := Snapshot{Version: Version, GeneratedAt: in.Now.UTC(), Timezone: loc.String(), Today: today, Cadence: in.Cadence,
		Project: clean(in.Project), Items: []Item{}, Timeline: []Day{}, Trend: []Point{}}
	open, done, mix := split(in.Issues)
	s.StatusMix = mix
	s.Sections = sections(open, today)
	s.KPIs = kpis(open, s.Sections, today)
	s.Timeline = timeline(open, in.Now.In(loc))
	if len(in.History) > 0 {
		prev := in.History[0]
		s.PreviousReportID = prev.ReportID
		s.Changes = changes(open, done, prev.Snapshot, today)
		finished := len(s.Changes.Finished)
		s.KPIs.DoneSincePrev = &finished
		s.Deltas = deltas(s.KPIs, prev.Snapshot.KPIs)
	}
	s.Trend = trend(s.Today, s.KPIs, in.History)
	s.Items, s.Truncated = keep(open, s.Sections, s.Changes, MaxItems)
	capSections(&s)
	return s
}

// split turns tracked issues into open items (first copy of each key wins), the done ones, and the status mix.
func split(issues []Tracked) ([]Item, map[string]tracker.Issue, StatusMix) {
	seen := map[string]bool{}
	var open []Item
	done := map[string]tracker.Issue{}
	var mix StatusMix
	for _, t := range issues {
		i := t.Issue
		if i.Key == "" || seen[i.Key] {
			continue
		}
		seen[i.Key] = true
		switch i.StatusCategory {
		case model.StatusDone:
			mix.Done++
		case model.StatusCanceled:
			mix.Canceled++
		case model.StatusInProgress:
			mix.InProgress++
		default:
			mix.ToDo++
		}
		if i.Done() {
			done[i.Key] = i
			continue
		}
		open = append(open, itemOf(t))
	}
	sort.SliceStable(open, func(a, b int) bool { return dueBefore(open[a], open[b]) })
	return open, done, mix
}

func itemOf(t Tracked) Item {
	i := t.Issue
	it := Item{Key: clean(i.Key), Title: clean(i.Title), URL: SafeURL(i.URL), Status: clean(i.Status), StatusCategory: i.StatusCategory,
		DueDate: dateOnly(i.DueDate), Labels: []string{}, Blocked: tracker.Blocked(i), Parent: clean(i.Parent), Epic: clean(t.Epic)}
	if it.Epic == it.Key {
		it.Epic = ""
	}
	if i.Assignee != nil {
		it.Assignee = clean(i.Assignee.Name)
		if it.Assignee == "" {
			it.Assignee = clean(i.Assignee.AccountID)
		}
	}
	for _, l := range i.Labels {
		if l = clean(l); l != "" && len(it.Labels) < 20 {
			it.Labels = append(it.Labels, l)
		}
	}
	if !i.UpdatedAt.IsZero() {
		it.UpdatedAt = i.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return it
}

// dateOnly keeps a YYYY-MM-DD due date and drops anything else.
func dateOnly(s string) string {
	if len(s) >= len(time.DateOnly) {
		if _, err := time.Parse(time.DateOnly, s[:len(time.DateOnly)]); err == nil {
			return s[:len(time.DateOnly)]
		}
	}
	return ""
}

// dueBefore orders items by due date (none last), then key.
func dueBefore(a, b Item) bool {
	switch {
	case a.DueDate == b.DueDate:
		return a.Key < b.Key
	case a.DueDate == "":
		return false
	case b.DueDate == "":
		return true
	}
	return a.DueDate < b.DueDate
}

func addDays(date string, n int) string {
	t, _ := time.Parse(time.DateOnly, date)
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

func daysBetween(from, to string) int {
	a, _ := time.Parse(time.DateOnly, from)
	b, _ := time.Parse(time.DateOnly, to)
	return int(b.Sub(a).Hours() / 24)
}

func sections(open []Item, today string) Sections {
	s := Sections{Overdue: []Late{}, DueToday: []string{}, DueNext7: []string{}, Blocked: []string{}, UnassignedSoon: []string{}, ByPerson: []Person{}}
	soon, unassignedBy := addDays(today, SoonDays), addDays(today, UnassignedDays)
	people := map[string]*Person{}
	for _, it := range open {
		due := it.DueDate
		overdue := due != "" && due < today
		due7 := due != "" && due >= today && due <= soon
		switch {
		case overdue:
			s.Overdue = append(s.Overdue, Late{Key: it.Key, DaysLate: daysBetween(due, today)})
		case due == today:
			s.DueToday = append(s.DueToday, it.Key)
		case due7:
			s.DueNext7 = append(s.DueNext7, it.Key)
		}
		if it.Blocked {
			s.Blocked = append(s.Blocked, it.Key)
		}
		if it.Assignee == "" && due != "" && due <= unassignedBy {
			s.UnassignedSoon = append(s.UnassignedSoon, it.Key)
		}
		name := it.Assignee
		if name == "" {
			name = "Unassigned"
		}
		p := people[name]
		if p == nil {
			p = &Person{Name: name}
			people[name] = p
		}
		p.Open++
		if overdue {
			p.Overdue++
		}
		if due7 {
			p.Due7++
		}
	}
	sort.SliceStable(s.Overdue, func(a, b int) bool { return s.Overdue[a].DaysLate > s.Overdue[b].DaysLate })
	for _, p := range people {
		s.ByPerson = append(s.ByPerson, *p)
	}
	sort.Slice(s.ByPerson, func(a, b int) bool {
		pa, pb := s.ByPerson[a], s.ByPerson[b]
		if pa.Overdue+pa.Due7 != pb.Overdue+pb.Due7 {
			return pa.Overdue+pa.Due7 > pb.Overdue+pb.Due7
		}
		if pa.Overdue != pb.Overdue {
			return pa.Overdue > pb.Overdue
		}
		if pa.Open != pb.Open {
			return pa.Open > pb.Open
		}
		return pa.Name < pb.Name
	})
	return s
}

func kpis(open []Item, s Sections, today string) KPIs {
	return KPIs{Open: len(open), Overdue: len(s.Overdue), DueToday: len(s.DueToday), Due7: len(s.DueToday) + len(s.DueNext7),
		Blocked: len(s.Blocked), UnassignedSoon: len(s.UnassignedSoon)}
}

// timeline counts open items due on each of the next TimelineDays days, today first.
func timeline(open []Item, now time.Time) []Day {
	out := make([]Day, TimelineDays)
	index := map[string]int{}
	for i := range out {
		out[i].Date = now.AddDate(0, 0, i).Format(time.DateOnly)
		index[out[i].Date] = i
	}
	for _, it := range open {
		if i, ok := index[it.DueDate]; ok {
			out[i].Count++
		}
	}
	return out
}

func changes(open []Item, done map[string]tracker.Issue, prev Snapshot, today string) *Changes {
	c := &Changes{NewlyOverdue: []string{}, Finished: []Finished{}, Slipped: []Move{}, Reassigned: []Move{}, New: []string{}}
	before := map[string]Item{}
	for _, it := range prev.Items {
		before[it.Key] = it
	}
	for _, it := range open {
		was, known := before[it.Key]
		overdue := it.DueDate != "" && it.DueDate < today
		switch {
		case !known:
			if !prev.Truncated {
				c.New = append(c.New, it.Key)
			}
			if overdue {
				c.NewlyOverdue = append(c.NewlyOverdue, it.Key)
			}
			continue
		case overdue && !(was.DueDate != "" && was.DueDate < prev.Today):
			c.NewlyOverdue = append(c.NewlyOverdue, it.Key)
		}
		if was.DueDate != "" && it.DueDate > was.DueDate {
			c.Slipped = append(c.Slipped, Move{Key: it.Key, Title: it.Title, From: was.DueDate, To: it.DueDate})
		}
		if was.Assignee != it.Assignee {
			c.Reassigned = append(c.Reassigned, Move{Key: it.Key, Title: it.Title, From: was.Assignee, To: it.Assignee})
		}
	}
	for _, it := range prev.Items {
		if d, ok := done[it.Key]; ok {
			c.Finished = append(c.Finished, Finished{Key: it.Key, Title: clean(d.Title)})
		}
	}
	return c
}

func deltas(now, prev KPIs) *KPIs {
	d := &KPIs{Open: now.Open - prev.Open, Overdue: now.Overdue - prev.Overdue, DueToday: now.DueToday - prev.DueToday,
		Due7: now.Due7 - prev.Due7, Blocked: now.Blocked - prev.Blocked, UnassignedSoon: now.UnassignedSoon - prev.UnassignedSoon}
	if now.DoneSincePrev != nil && prev.DoneSincePrev != nil {
		v := *now.DoneSincePrev - *prev.DoneSincePrev
		d.DoneSincePrev = &v
	}
	return d
}

// trend is one point per day (the newest report of a day wins), oldest first, ending with today.
func trend(today string, k KPIs, history []Previous) []Point {
	byDate := map[string]Point{today: {Date: today, KPIs: k}}
	dates := []string{today}
	for _, h := range history {
		d := h.Snapshot.Today
		if _, ok := byDate[d]; ok || d == "" || d > today {
			continue
		}
		byDate[d] = Point{Date: d, KPIs: h.Snapshot.KPIs}
		dates = append(dates, d)
	}
	slices.Sort(dates)
	if len(dates) > TrendPoints {
		dates = dates[len(dates)-TrendPoints:]
	}
	out := make([]Point, len(dates))
	for i, d := range dates {
		out[i] = byDate[d]
	}
	return out
}

// keep chooses which open items the snapshot carries: those a section or change names first, then by due date.
func keep(open []Item, s Sections, c *Changes, limit int) ([]Item, bool) {
	if len(open) <= limit {
		return append([]Item{}, open...), false
	}
	wanted := map[string]bool{}
	for _, l := range s.Overdue {
		wanted[l.Key] = true
	}
	for _, list := range [][]string{s.DueToday, s.DueNext7, s.Blocked, s.UnassignedSoon} {
		for _, k := range list {
			wanted[k] = true
		}
	}
	if c != nil {
		for _, list := range [][]string{c.NewlyOverdue, c.New} {
			for _, k := range list {
				wanted[k] = true
			}
		}
		for _, m := range append(append([]Move{}, c.Slipped...), c.Reassigned...) {
			wanted[m.Key] = true
		}
	}
	out := []Item{}
	for _, it := range open {
		if wanted[it.Key] && len(out) < limit {
			out = append(out, it)
		}
	}
	for _, it := range open {
		if !wanted[it.Key] && len(out) < limit {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return dueBefore(out[a], out[b]) })
	return out, true
}

func capSections(s *Snapshot) {
	cut := func(n int) bool { return n > MaxSectionRows }
	if cut(len(s.Sections.Overdue)) {
		s.Sections.Overdue, s.Truncated = s.Sections.Overdue[:MaxSectionRows], true
	}
	for _, list := range []*[]string{&s.Sections.DueToday, &s.Sections.DueNext7, &s.Sections.Blocked, &s.Sections.UnassignedSoon} {
		if cut(len(*list)) {
			*list, s.Truncated = (*list)[:MaxSectionRows], true
		}
	}
	if len(s.Sections.ByPerson) > MaxPeople {
		s.Sections.ByPerson, s.Truncated = s.Sections.ByPerson[:MaxPeople], true
	}
	if c := s.Changes; c != nil {
		for _, list := range []*[]string{&c.NewlyOverdue, &c.New} {
			if cut(len(*list)) {
				*list, s.Truncated = (*list)[:MaxSectionRows], true
			}
		}
		for _, list := range []*[]Move{&c.Slipped, &c.Reassigned} {
			if cut(len(*list)) {
				*list, s.Truncated = (*list)[:MaxSectionRows], true
			}
		}
		if cut(len(c.Finished)) {
			c.Finished, s.Truncated = c.Finished[:MaxSectionRows], true
		}
	}
}

// ErrTooBig is a snapshot that stays over MaxBytes even with few items.
var ErrTooBig = errors.New("the report snapshot is too large to keep")

// Encode marshals a snapshot within MaxBytes, keeping fewer items when it must.
func Encode(s Snapshot) (json.RawMessage, error) {
	for _, limit := range []int{len(s.Items), 200, 50, 0} {
		if limit < len(s.Items) {
			s.Items, s.Truncated = s.Items[:limit], true
		}
		raw, err := json.Marshal(s)
		if err != nil {
			return nil, fmt.Errorf("encode report snapshot: %w", err)
		}
		if len(raw) <= MaxBytes {
			return raw, nil
		}
	}
	return nil, ErrTooBig
}

// Parse reads a stored snapshot; nil (and no error) when there is none.
func Parse(raw json.RawMessage) (*Snapshot, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("read report snapshot: %w", err)
	}
	return &s, nil
}

// ItemsByKey indexes a snapshot's items.
func (s *Snapshot) ItemsByKey() map[string]Item {
	out := make(map[string]Item, len(s.Items))
	for _, it := range s.Items {
		out[it.Key] = it
	}
	return out
}
