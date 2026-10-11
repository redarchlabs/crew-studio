// Package scheduler fires schedules: each due schedule creates and submits a work order, acting as the
// person who last saved the schedule.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/watch"
)

const (
	tickInterval = 20 * time.Second
	batchSize    = 20
	// MinInterval stops a schedule from starting paid runs too often
	MinInterval = 5 * time.Minute
	// how many upcoming times are checked against MinInterval
	intervalSamples = 60
	// maxConcurrentFires bounds how many due schedules fire at once, so a slow watch source (a tracker taking its
	// time) does not hold up every other schedule in the tick
	maxConcurrentFires = 4
)

// watchFetchTimeout bounds one watch check (fetching its source and comparing); a variable so tests can shorten it.
var watchFetchTimeout = 3 * time.Minute

// fireTimeout bounds one schedule's whole fire (its check, the work order and the bookkeeping), so a stuck fire
// cannot hold a slot, and the tick, forever.
var fireTimeout = 5 * time.Minute

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// NextTimes returns the next n times expr fires after from, in the schedule's time zone. It also enforces
// MinInterval, so it is the validation used when saving a schedule.
func NextTimes(expr, timezone string, from time.Time, n int) ([]time.Time, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("timezone: %q is not a known time zone", timezone)
	}
	if strings.HasPrefix(strings.TrimSpace(expr), "@every") {
		return nil, errors.New("cron: use a calendar expression (5 fields, or @hourly/@daily/@weekly/@monthly), not @every")
	}
	sched, err := parser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("cron: %v", err)
	}
	var times []time.Time
	t := from.In(loc)
	samples := max(n, intervalSamples)
	for i := 0; i < samples; i++ {
		next := sched.Next(t)
		if next.IsZero() {
			break
		}
		if i > 0 && next.Sub(t) < MinInterval {
			return nil, fmt.Errorf("cron: it would run more often than every %d minutes", int(MinInterval.Minutes()))
		}
		times = append(times, next)
		t = next
	}
	if len(times) == 0 {
		return nil, errors.New("cron: it never fires")
	}
	return times[:min(n, len(times))], nil
}

// Next returns the next fire time after from.
func Next(expr, timezone string, from time.Time) (*time.Time, error) {
	times, err := NextTimes(expr, timezone, from, 1)
	if err != nil {
		return nil, err
	}
	next := times[0].UTC()
	return &next, nil
}

type Scheduler struct {
	store    *store.Store
	launcher *launch.Launcher
	log      *slog.Logger
	wake     chan struct{}
	now      func() time.Time
	// watches checks schedules' watches before they start work; nil turns watches off
	watches *watch.Service
}

func New(st *store.Store, launcher *launch.Launcher, log *slog.Logger) *Scheduler {
	return &Scheduler{store: st, launcher: launcher, log: log, wake: make(chan struct{}, 1), now: time.Now}
}

// SetWatches turns on watches: a schedule with a watch only starts a work order when its source changed or a
// follow-up is due (see watch.go).
func (s *Scheduler) SetWatches(w *watch.Service) { s.watches = w }

// Notify asks the scheduler to look for due schedules now (after a schedule is saved).
func (s *Scheduler) Notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run fires due schedules until ctx ends.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}

// Tick fires every schedule that is due now. A schedule missed while the service was down fires once.
func (s *Scheduler) Tick(ctx context.Context) {
	now := s.now()
	due, err := s.store.DueSchedules(ctx, now, batchSize)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("list due schedules", "err", err)
		}
		return
	}
	// Each schedule is claimed (moved to its next time) before it fires, as before; firing runs at most
	// maxConcurrentFires at a time, and the tick ends when all of them have
	slots := make(chan struct{}, maxConcurrentFires)
	var wg sync.WaitGroup
	defer wg.Wait()
	for _, sc := range due {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		if !s.claim(ctx, sc, now) {
			<-slots
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			fireCtx, cancel := context.WithTimeout(ctx, fireTimeout)
			defer cancel()
			s.fire(fireCtx, sc, now)
		}()
	}
}

// claim moves a due schedule to its next time, reporting whether this process should fire it now.
func (s *Scheduler) claim(ctx context.Context, sc model.Schedule, now time.Time) bool {
	next, err := Next(sc.Cron, sc.Timezone, now)
	if err != nil {
		// The cron was valid when saved; a broken one (e.g. a removed time zone) pauses the schedule
		msg := "Paused: " + err.Error()
		_ = s.store.RecordScheduleOutcome(ctx, sc.ID, nil, &msg, true)
		return false
	}
	claimed, err := s.store.ClaimSchedule(ctx, sc.ID, *sc.NextRunAt, next)
	if err != nil {
		s.log.Error("claim schedule", "schedule_id", sc.ID, "err", err)
		return false
	}
	// false: another process fired it, or it was edited meanwhile
	return claimed
}

func (s *Scheduler) fire(ctx context.Context, sc model.Schedule, now time.Time) {
	p, err := s.launcher.RunAsKey(ctx, sc.RunAs, sc.RunAsKey)
	if err != nil {
		// Only pause for problems that will not fix themselves; a database hiccup just records the error
		s.record(ctx, sc, nil, err, permanent(err))
		return
	}
	w, watched, err := s.watchOf(ctx, sc)
	if err != nil {
		s.record(ctx, sc, nil, err, false)
		return
	}
	if sc.OverlapPolicy == model.OverlapSkip {
		active, err := s.store.SourceHasActiveWorkOrder(ctx, model.SourceSchedule, sc.ID)
		if err != nil {
			// The schedule is already claimed and moved on, so record why this fire was missed (without pausing)
			s.record(ctx, sc, nil, fmt.Errorf("check for a running work order: %w", err), false)
			return
		}
		if active {
			msg := "Skipped at " + now.UTC().Format(time.RFC3339) + ": the previous work order was still running."
			_ = s.store.RecordScheduleOutcome(ctx, sc.ID, nil, &msg, false)
			if watched {
				s.recordCheck(ctx, w, model.WatchCheck{CheckedAt: now, Outcome: model.WatchCheckBusy,
					Message: "The previous work order was still running, so the source was not checked."})
			}
			return
		}
	}
	if watched {
		s.fireWatched(ctx, p, sc, w, now)
		return
	}
	wo, err := s.launch(ctx, p, sc, now, nil)
	var woID *string
	if wo.ID != "" {
		woID = &wo.ID
	}
	s.record(ctx, sc, woID, err, permanent(err))
}

// Launch creates and submits one work order for the schedule, as p (Run now). A schedule with a watch checks its
// source first and passes the changes on, whether or not anything changed.
func (s *Scheduler) Launch(ctx context.Context, p *auth.Principal, sc model.Schedule, now time.Time) (model.WorkOrder, error) {
	w, watched, err := s.watchOf(ctx, sc)
	if err != nil {
		return model.WorkOrder{}, err
	}
	if watched {
		return s.launchWatchedNow(ctx, p, sc, w, now)
	}
	return s.launch(ctx, p, sc, now, nil)
}

// launch creates and submits one work order, adding extra inputs (a watch's) to the schedule's.
func (s *Scheduler) launch(ctx context.Context, p *auth.Principal, sc model.Schedule, now time.Time, extra map[string]string) (model.WorkOrder, error) {
	inputs := make(map[string]string, len(sc.Inputs)+len(extra))
	for k, v := range sc.Inputs {
		inputs[k] = v
	}
	for k, v := range extra {
		inputs[k] = v
	}
	loc, err := time.LoadLocation(sc.Timezone)
	if err != nil {
		loc = time.UTC
	}
	ctx = store.WithActor(ctx, store.Actor{ID: p.UserID, Name: p.Name + " (schedule " + sc.Name + ")", APIKeyID: p.APIKeyID})
	created, err := s.launcher.CreateWorkOrder(ctx, p, model.WorkOrderInput{
		Title:  sc.Name + " · " + now.In(loc).Format("2006-01-02 15:04 MST"),
		Brief:  sc.Brief,
		CrewID: sc.CrewID,
		Inputs: inputs, BudgetUSD: sc.BudgetUSD, Submit: true,
	}, launch.WorkOrderSource{Kind: model.SourceSchedule, ID: sc.ID}, launch.Idempotency{}, nil)
	wo := created.WorkOrder
	if err == nil {
		s.log.Info("schedule fired", "schedule_id", sc.ID, "work_order", wo.Number)
	}
	return wo, err
}

// permanent reports errors that will not fix themselves, so the schedule should pause.
func permanent(err error) bool {
	var forbidden *launch.ForbiddenError
	var problems *launch.ProblemsError
	return errors.As(err, &forbidden) || errors.As(err, &problems) || errors.Is(err, store.ErrNotFound)
}

func (s *Scheduler) record(ctx context.Context, sc model.Schedule, woID *string, err error, disable bool) {
	var msg *string
	if err != nil {
		text := describe(err)
		if disable {
			text = "Paused: " + text
		}
		msg = &text
		s.log.Warn("schedule did not start a work order", "schedule_id", sc.ID, "paused", disable, "err", err)
	}
	if rerr := s.store.RecordScheduleOutcome(ctx, sc.ID, woID, msg, disable); rerr != nil {
		s.log.Error("record schedule outcome", "schedule_id", sc.ID, "err", rerr)
	}
}

func describe(err error) string {
	var forbidden *launch.ForbiddenError
	var problems *launch.ProblemsError
	switch {
	case errors.As(err, &forbidden):
		return "the person it runs as no longer has the " + forbidden.Permission + " permission."
	case errors.As(err, &problems):
		return problems.Message + " " + strings.Join(problems.Details, " ")
	case errors.Is(err, store.ErrNotFound):
		return "its task or crew is outside the areas of the person it runs as."
	}
	return err.Error()
}
