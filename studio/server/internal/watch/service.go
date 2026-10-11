package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	"crewstudio/internal/connections"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

// MaxInputChars caps the changes and followups_due inputs. Past it the added items lose their fields; if that is
// still too long the check fails with a clear error.
const MaxInputChars = 120_000

// Service runs checks: it loads what a check needs, fetches the source and decides whether a run is due.
type Service struct {
	store   *store.Store
	conns   *connections.Service
	sources map[string]Source
}

// New builds the service with the sources available today. workspace is the Studio workspace folder.
func New(st *store.Store, conns *connections.Service, workspace string) *Service {
	return &Service{store: st, conns: conns, sources: map[string]Source{
		model.WatchSourceHTTPJSON:      HTTPJSON{Transport: connections.Transport()},
		model.WatchSourceWebPage:       WebPage{},
		model.WatchSourceWorkspaceFile: WorkspaceFile{Dir: workspace},
	}}
}

// WithTrackers adds the tracker and tracking-list sources, which read Jira and Linear through ts.
func (s *Service) WithTrackers(ts *tracker.Service) *Service {
	return s.WithSource(model.WatchSourceTracker, TrackerSource{Trackers: ts, Store: s.store}).
		WithSource(model.WatchSourceTrackingList, TrackingListSource{Trackers: ts, Store: s.store})
}

// WithSource replaces the source for a kind (tests use it to reach local servers).
func (s *Service) WithSource(kind string, src Source) *Service {
	sources := make(map[string]Source, len(s.sources))
	for k, v := range s.sources {
		sources[k] = v
	}
	sources[kind] = src
	return &Service{store: s.store, conns: s.conns, sources: sources}
}

// Check is one check of a watch definition for a schedule.
type Check struct {
	// Def is the normalized definition
	Def      model.WatchInput
	Schedule model.Schedule
	// Saved is the stored watch, or nil for a definition that is not saved yet (preview)
	Saved *model.Watch
	// Force runs even when there is nothing to do (Run now)
	Force bool
}

// Evaluation is the result of a check.
type Evaluation struct {
	Changes   Changes    `json:"changes"`
	Followups *Followups `json:"followups_due"`
	// Run says whether a work order should start, and Reason says why (or why not)
	Run    bool   `json:"would_run"`
	Reason string `json:"reason"`
	// Items are the items just fetched (the snapshot to save once the work order exists)
	Items []Item `json:"-"`
	// Sample is a few items as fetched, to help set up fields
	Sample []Item `json:"sample"`
	// ItemCount is how many items the source returned
	ItemCount int `json:"item_count"`
	// SeenMoved is true when an item's newest-comment time moved, so the snapshot must be saved even without a run
	SeenMoved bool `json:"-"`
	// Rollups are the tracking list's roll-ups (tracking_list sources), every tracked item with whether it changed
	Rollups *Rollups `json:"rollups,omitempty"`
	// Inputs are what the work order gets: changes, followups_due, rollups and dry_run
	Inputs map[string]string `json:"-"`
}

const sampleSize = 5

// Evaluate fetches the source, compares it with the stored snapshot (when the saved watch's definition matches)
// and works out which follow-ups are due. It changes nothing.
func (s *Service) Evaluate(ctx context.Context, c Check, now time.Time) (Evaluation, error) {
	if c.Schedule.CrewID == nil {
		return Evaluation{}, sourceErrorf("the schedule's crew was deleted")
	}
	prev, prevMeta, previousAt, err := s.previous(ctx, c)
	if err != nil {
		return Evaluation{}, err
	}
	req := Request{Def: c.Def, CrewID: *c.Schedule.CrewID, ScheduleID: c.Schedule.ID, Previous: prevMeta,
		FirstCheck: previousAt == nil, Now: now}
	if c.Def.ConnectionID != nil {
		conn, secrets, err := s.connection(ctx, *c.Def.ConnectionID, *c.Schedule.CrewID, c.Def.Source.Kind)
		if err != nil {
			return Evaluation{}, err
		}
		req.Connection, req.Secrets = &conn, secrets
	}
	fetched, err := FetchItems(ctx, s.sources, req)
	if err != nil {
		return Evaluation{}, err
	}
	loc := location(c.Def.Followups, c.Schedule.Timezone)
	ev := Evaluation{Items: fetched.Items, ItemCount: len(fetched.Items), Sample: fetched.Items[:min(sampleSize, len(fetched.Items))]}
	ev.Changes = Compare(prev, fetched.Items, fetched.Label, now.In(loc), previousAt)
	ev.Changes.NewComments, ev.Changes.NewCommentsOmitted = fetched.Comments, fetched.CommentsOmitted
	if fetched.Groups != nil {
		ev.Rollups = BuildRollups(fetched.Groups, ev.Changes, fetched.CommentCounts, prevMeta, c.Def.Source.StaleDays, now, loc)
	}
	ev.SeenMoved = seenMoved(fetched.Items, prevMeta)
	if c.Def.Followups != nil {
		if ev.Followups, err = s.followups(ctx, c, fetched.Items, now, loc); err != nil {
			return Evaluation{}, err
		}
	}
	ev.Run, ev.Reason = decide(c.Def.RunWhen, ev.Changes, ev.Followups, c.Force)
	ev.Changes, ev.Inputs, err = inputs(ev.Changes, ev.Followups, ev.Rollups, c.Def.DryRun == nil || *c.Def.DryRun)
	return ev, err
}

// connection loads the watch's connection and checks the crew may use it.
func (s *Service) connection(ctx context.Context, connID, crewID, sourceKind string) (model.Connection, map[string]string, error) {
	use, err := s.store.ConnectionForCrew(ctx, connID, crewID)
	if errors.Is(err, store.ErrNotFound) {
		return model.Connection{}, nil, sourceErrorf("the watch's connection no longer exists")
	}
	if err != nil {
		return model.Connection{}, nil, err
	}
	if problem := ConnectionProblem(use, sourceKind); problem != "" {
		return model.Connection{}, nil, sourceErrorf("%s", problem)
	}
	if len(use.Connection.SecretKeys) == 0 {
		return use.Connection, map[string]string{}, nil
	}
	secrets, err := s.conns.Secrets(ctx, connID)
	if err != nil {
		return model.Connection{}, nil, sourceErrorf("the secrets of %s cannot be read: %v", use.Connection.Name, err)
	}
	return use.Connection, secrets, nil
}

// ConnectionProblem says why a crew cannot use a connection for a source kind, or "".
func ConnectionProblem(use store.CrewConnectionUse, sourceKind string) string {
	c := use.Connection
	kind, _ := model.WatchSourceKindOf(sourceKind)
	switch {
	case !model.ConnectionFits(kind.Connection, c.Kind):
		need := kind.Connection
		if need == model.ConnectionTracker {
			need = "tracker (Jira, Linear or KM2 issues)"
		}
		return fmt.Sprintf("the connection %s is a %s connection; this source needs a %s connection", c.Name, c.Kind, need)
	case !c.Enabled:
		return fmt.Sprintf("the connection %s is disabled", c.Name)
	case !use.Linked:
		return fmt.Sprintf("the crew may not use the connection %s (choose it on the crew's State tab)", c.Name)
	case !use.SharesArea:
		return fmt.Sprintf("the connection %s shares no functional area with the crew", c.Name)
	}
	return ""
}

// previous is the stored snapshot when the saved watch's definition still matches, or nothing (first check), with
// what tracker sources remembered about each item.
func (s *Service) previous(ctx context.Context, c Check) (Snapshot, map[string]ItemMeta, *time.Time, error) {
	if c.Saved == nil || c.Saved.SnapshotAt == nil || !model.SameSnapshotShape(*c.Saved, c.Def) {
		return Snapshot{}, map[string]ItemMeta{}, nil, nil
	}
	stored, err := s.store.LoadSnapshot(ctx, c.Saved.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	prev := make(Snapshot, len(stored))
	metas := map[string]ItemMeta{}
	for id, raw := range stored {
		fields, meta := splitMeta(raw)
		if meta != nil {
			metas[id] = *meta
		}
		clean := make(map[string]any, len(fields))
		for k, v := range fields {
			clean[k] = normalizeValue(v)
		}
		prev[id] = clean
	}
	at := *c.Saved.SnapshotAt
	return prev, metas, &at, nil
}

func (s *Service) followups(ctx context.Context, c Check, items []Item, now time.Time, loc *time.Location) (*Followups, error) {
	dryRun := c.Def.DryRun == nil || *c.Def.DryRun
	entries, err := s.store.LatestFollowups(ctx, *c.Schedule.CrewID, c.Schedule.ID, dryRun)
	if err != nil {
		return nil, err
	}
	sent := make(Sent, len(entries))
	for _, e := range entries {
		sent[SentKey{Subject: e.Subject, Action: e.Action}] = e.At
	}
	due, waiting := DueFollowups(*c.Def.Followups, items, sent, now, loc)
	return &Followups{Due: due, MoreWaiting: waiting, Undated: UndatedItems(*c.Def.Followups, items, loc), DryRun: dryRun,
		HowToRecord: RecordHint}, nil
}

func location(l *model.FollowupLadder, scheduleZone string) *time.Location {
	zone := scheduleZone
	if l != nil && l.Timezone != "" {
		zone = l.Timezone
	}
	if loc, err := time.LoadLocation(zone); err == nil {
		return loc
	}
	return time.UTC
}

// decide applies run_when.
func decide(runWhen string, ch Changes, f *Followups, force bool) (bool, string) {
	due := 0
	if f != nil {
		due = len(f.Due)
	}
	summary := fmt.Sprintf("%d added, %d changed, %d removed, %d follow-ups due", len(ch.Added), len(ch.Changed), len(ch.Removed), due)
	switch {
	case force:
		return true, "started by hand: " + summary
	case runWhen == model.WatchRunAlways:
		return true, "runs every time: " + summary
	case ch.Count() > 0:
		return true, summary
	case runWhen == model.WatchRunChangedOrFollowups && due > 0:
		return true, summary
	case due > 0:
		return false, fmt.Sprintf("no changes (%d follow-ups due, but this watch only runs when something changed)", due)
	}
	return false, "checked, no changes"
}

// inputs encodes the work order's watch inputs, dropping added items' fields when the changes are too long. Only
// roll-ups of tracked items with changes beneath them are passed on.
func inputs(ch Changes, f *Followups, r *Rollups, dryRun bool) (Changes, map[string]string, error) {
	changes, err := json.Marshal(ch)
	if err != nil {
		return ch, nil, fmt.Errorf("encode changes: %w", err)
	}
	if utf8.RuneCount(changes) > MaxInputChars {
		ch = withoutAddedFields(ch)
		if changes, err = json.Marshal(ch); err != nil {
			return ch, nil, fmt.Errorf("encode changes: %w", err)
		}
	}
	if n := utf8.RuneCount(changes); n > MaxInputChars {
		return ch, nil, sourceErrorf("the change list is too long to pass to a run (%d characters, limit %d); track fewer fields or lower max_items", n, MaxInputChars)
	}
	out := map[string]string{model.WatchInputChanges: string(changes), model.WatchInputDryRun: strconv.FormatBool(dryRun)}
	if f != nil {
		data, err := json.Marshal(f)
		if err != nil {
			return ch, nil, fmt.Errorf("encode follow-ups: %w", err)
		}
		if n := utf8.RuneCount(data); n > MaxInputChars {
			return ch, nil, sourceErrorf("the follow-ups due are too long to pass to a run (%d characters); lower max_per_tick", n)
		}
		out[model.WatchInputFollowups] = string(data)
	}
	if r != nil {
		data, err := json.Marshal(r.Changed())
		if err != nil {
			return ch, nil, fmt.Errorf("encode roll-ups: %w", err)
		}
		if n := utf8.RuneCount(data); n > MaxInputChars {
			return ch, nil, sourceErrorf("the roll-ups are too long to pass to a run (%d characters); track fewer items", n)
		}
		out[model.WatchInputRollups] = string(data)
	}
	return ch, out, nil
}
