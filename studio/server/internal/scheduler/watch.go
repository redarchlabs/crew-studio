package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/launch"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/watch"
)

// A schedule with a watch (docs/crew-state-and-pmo-design.md, section 5) fetches its source on every tick and
// compares it with the snapshot, with no AI involved. With nothing to do no work order is created and the check is
// recorded as "checked, no changes". Otherwise the work order gets the changes and the follow-ups due as inputs,
// and the snapshot is saved only after the work order exists, so a crash in between repeats the changes rather
// than losing them.

// watchOf returns the schedule's watch when watches are on and it has one.
func (s *Scheduler) watchOf(ctx context.Context, sc model.Schedule) (model.Watch, bool, error) {
	if s.watches == nil {
		return model.Watch{}, false, nil
	}
	w, err := s.store.WatchOfSchedule(ctx, sc.ID)
	if errors.Is(err, store.ErrNotFound) {
		return model.Watch{}, false, nil
	}
	if err != nil {
		return model.Watch{}, false, fmt.Errorf("read the schedule's watch: %w", err)
	}
	return w, true, nil
}

// fireWatched runs one tick of a watched schedule.
func (s *Scheduler) fireWatched(ctx context.Context, p *auth.Principal, sc model.Schedule, w model.Watch, now time.Time) {
	ev, err := s.evaluate(ctx, watch.Check{Def: w.Input(), Schedule: sc, Saved: &w}, now)
	if err != nil {
		msg := "Watch: " + s.describeWatchError(sc, err)
		s.recordCheck(ctx, w, model.WatchCheck{CheckedAt: now, Outcome: model.WatchCheckError, Message: msg})
		s.record(ctx, sc, nil, errors.New(msg), false)
		return
	}
	check := checkOf(ev, now)
	if !ev.Run {
		// New comment times (only the crew's own comments, say) are kept even without a run, so they are not read again
		if ev.SeenMoved && previousExists(w) {
			s.saveSnapshot(ctx, w, ev, now)
		} else if err := s.store.ConfirmSnapshot(ctx, w.ID, w.UpdatedAt, now); err != nil {
			s.logSnapshotError(w, "confirm watch snapshot", err)
		}
		check.Outcome = model.WatchCheckNoChanges
		s.recordCheck(ctx, w, check)
		s.record(ctx, sc, nil, nil, false)
		s.log.Info("schedule checked its watch: nothing to do", "schedule_id", sc.ID, "reason", ev.Reason)
		return
	}
	wo, err := s.launch(ctx, p, sc, now, ev.Inputs)
	var woID *string
	if wo.ID != "" {
		woID = &wo.ID
	}
	if err != nil {
		check.Outcome, check.WorkOrderID = model.WatchCheckError, woID
		check.Message = "The work order could not be started: " + describe(err)
		s.recordCheck(ctx, w, check)
		s.record(ctx, sc, woID, err, permanent(err))
		return
	}
	s.saveSnapshot(ctx, w, ev, now)
	check.Outcome, check.WorkOrderID = model.WatchCheckStarted, woID
	s.recordCheck(ctx, w, check)
	s.record(ctx, sc, woID, nil, false)
}

// launchWatchedNow is Run now for a watched schedule: it checks the source and always starts a work order.
func (s *Scheduler) launchWatchedNow(ctx context.Context, p *auth.Principal, sc model.Schedule, w model.Watch, now time.Time) (model.WorkOrder, error) {
	ev, err := s.evaluate(ctx, watch.Check{Def: w.Input(), Schedule: sc, Saved: &w, Force: true}, now)
	if err != nil {
		msg := s.describeWatchError(sc, err)
		s.recordCheck(ctx, w, model.WatchCheck{CheckedAt: now, Outcome: model.WatchCheckError, Message: "Watch (run now): " + msg})
		return model.WorkOrder{}, &launch.ProblemsError{Message: "The watch could not check its source.", Details: []string{msg}}
	}
	wo, err := s.launch(ctx, p, sc, now, ev.Inputs)
	if err != nil {
		return wo, err
	}
	// The work order exists: finish the bookkeeping even if the person's request ends now
	ctx = context.WithoutCancel(ctx)
	s.saveSnapshot(ctx, w, ev, now)
	check := checkOf(ev, now)
	check.Outcome, check.WorkOrderID = model.WatchCheckStarted, &wo.ID
	check.Message = "Run now by " + p.Name + ": " + check.Message
	s.recordCheck(ctx, w, check)
	return wo, nil
}

// evaluate checks a watch within watchFetchTimeout, so a source that hangs cannot hold a firing slot forever.
func (s *Scheduler) evaluate(ctx context.Context, c watch.Check, now time.Time) (watch.Evaluation, error) {
	ctx, cancel := context.WithTimeout(ctx, watchFetchTimeout)
	defer cancel()
	ev, err := s.watches.Evaluate(ctx, c, now)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ev, &watch.SourceError{Message: fmt.Sprintf("the source did not answer within %s", watchFetchTimeout)}
	}
	return ev, err
}

// saveSnapshot stores what this check saw. A failure is logged: the next check then sees the same changes again.
func (s *Scheduler) saveSnapshot(ctx context.Context, w model.Watch, ev watch.Evaluation, now time.Time) {
	if err := s.store.SaveSnapshot(ctx, w.ID, w.UpdatedAt, watch.SnapshotOf(ev.Items), now); err != nil {
		s.logSnapshotError(w, "save watch snapshot (the next check will repeat these changes)", err)
	}
}

func (s *Scheduler) logSnapshotError(w model.Watch, msg string, err error) {
	if errors.Is(err, store.ErrWatchChanged) {
		s.log.Info("watch changed during its check; its snapshot was not saved", "watch_id", w.ID)
		return
	}
	s.log.Error(msg, "watch_id", w.ID, "err", err)
}

func checkOf(ev watch.Evaluation, now time.Time) model.WatchCheck {
	due := 0
	if ev.Followups != nil {
		due = len(ev.Followups.Due)
	}
	return model.WatchCheck{CheckedAt: now, Added: len(ev.Changes.Added), Changed: len(ev.Changes.Changed),
		Removed: len(ev.Changes.Removed), Unchanged: ev.Changes.UnchangedCount, FollowupsDue: due, Message: ev.Reason}
}

func (s *Scheduler) recordCheck(ctx context.Context, w model.Watch, c model.WatchCheck) {
	if err := s.store.RecordWatchCheck(ctx, w.ID, c); err != nil {
		s.log.Error("record watch check", "watch_id", w.ID, "err", err)
	}
}

// describeWatchError is what people see: source problems as they are, anything else in general terms (logged).
func (s *Scheduler) describeWatchError(sc model.Schedule, err error) string {
	var serr *watch.SourceError
	var limit *watch.LimitError
	if errors.As(err, &serr) || errors.As(err, &limit) {
		return err.Error()
	}
	s.log.Error("watch check failed", "schedule_id", sc.ID, "err", err)
	return "the check failed on the server; see the service log"
}

// previousExists reports whether the watch already has a snapshot (a first check always starts a run anyway).
func previousExists(w model.Watch) bool { return w.SnapshotAt != nil }
