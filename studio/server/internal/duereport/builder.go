package duereport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
	"crewstudio/internal/watch"
)

// ReadTimeout bounds reading the tracker when a report is delivered: a slow tracker costs the report its snapshot,
// never the report itself.
const ReadTimeout = 20 * time.Second

// readMaxItems bounds the tracked items read for one report, like a watch's own default limit
const readMaxItems = 2000

// errNoItems is a project with nothing on the tracking list.
var errNoItems = errors.New("the tracking list has no items for the project")

// Request is a report about to be delivered.
type Request struct {
	CrewID     string
	ScheduleID *string
	// Project is the report's project; its tracking-list items are those whose project has the same slug
	Project  string
	Cadence  string
	Location *time.Location
	Now      time.Time
}

// Builder reads a project's tracked items the way a tracking_list watch does and makes the snapshot.
type Builder struct {
	store  *store.Store
	source watch.TrackingListSource
}

// NewBuilder builds the snapshot maker.
func NewBuilder(st *store.Store, trackers *tracker.Service) *Builder {
	return &Builder{store: st, source: watch.TrackingListSource{Trackers: trackers, Store: st}}
}

// Snapshot reads the tracker (within ReadTimeout) and the project's earlier snapshots, and returns the encoded
// snapshot. An error says why there is none; the caller delivers the report without it.
func (b *Builder) Snapshot(ctx context.Context, r Request) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	issues, err := b.read(ctx, r)
	if err != nil {
		return nil, err
	}
	slug := model.ProjectSlug(r.Project)
	history, err := b.history(ctx, r.CrewID, slug, r.Cadence)
	if err != nil {
		return nil, err
	}
	return Encode(Make(Input{Project: r.Project, Cadence: r.Cadence, Location: r.Location, Now: r.Now, Issues: issues, History: history}))
}

// read gathers the project's tracked items and their children through the tracking_list watch source.
func (b *Builder) read(ctx context.Context, r Request) ([]Tracked, error) {
	// A run no schedule started reads only the crew-wide tracking list (no schedule has the nil id)
	sched := "00000000-0000-0000-0000-000000000000"
	if r.ScheduleID != nil {
		sched = *r.ScheduleID
	}
	def := model.WatchInput{Source: model.WatchSource{Kind: model.WatchSourceTrackingList}, MaxItems: readMaxItems}
	fetched, err := b.source.Fetch(ctx, watch.Request{Def: def, CrewID: r.CrewID, ScheduleID: sched, Now: r.Now, FirstCheck: true})
	if err != nil {
		return nil, err
	}
	slug := model.ProjectSlug(r.Project)
	var out []Tracked
	for _, g := range fetched.Groups {
		if model.ProjectSlug(g.Project) != slug {
			continue
		}
		out = append(out, Tracked{Issue: g.Parent, Epic: g.Parent.Key})
		for _, c := range g.Children {
			out = append(out, Tracked{Issue: c, Epic: g.Parent.Key})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w %s", errNoItems, clean(r.Project))
	}
	return out, nil
}

// history are the project's earlier snapshots of the cadence, newest first.
func (b *Builder) history(ctx context.Context, crewID, slug, cadence string) ([]Previous, error) {
	rows, err := b.store.ReportSnapshots(ctx, crewID, slug, cadence, TrendPoints)
	if err != nil {
		return nil, err
	}
	out := make([]Previous, 0, len(rows))
	for _, row := range rows {
		s, err := Parse(row.Snapshot)
		if err != nil || s == nil {
			continue
		}
		out = append(out, Previous{ReportID: row.ReportID, Snapshot: *s})
	}
	return out, nil
}

// Reason says why a report has no snapshot, in words safe to store in the action log and show to people.
func Reason(err error) string {
	var se *watch.SourceError
	var le *watch.LimitError
	var te *tracker.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("the tracker did not answer within %d seconds", int(ReadTimeout.Seconds()))
	case errors.As(err, &se):
		return se.Error()
	case errors.As(err, &le):
		return le.Error()
	case errors.As(err, &te):
		return te.Message
	case errors.Is(err, ErrTooBig):
		return ErrTooBig.Error()
	case errors.Is(err, errNoItems):
		return err.Error()
	}
	return "the tracker could not be read; see the service log"
}
