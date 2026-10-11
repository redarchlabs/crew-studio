// Package reports delivers a crew's finished reports (docs/crew-state-and-pmo-design.md, section 12): one report,
// sent to the work order output and to every destination set on the schedule that started the run (Slack, Teams,
// email, KM2). Each delivery is recorded with its outcome and, once sent, in the action log as report:<kind> under
// an idempotency key, so a failed one can be retried later without sending the others again. In a dry run nothing
// is sent: each delivery is recorded as "would send", with a dry-run action log entry.
package reports

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"crewstudio/internal/delivery"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

const (
	// maxParallel bounds how many deliveries of one report are sent at once
	maxParallel = 4
	// StaleAfter is when a delivery left "pending" (the server stopped mid-send) may be retried
	StaleAfter = 10 * time.Minute
)

// Sender sends a message through a slack, teams or email connection.
type Sender interface {
	Send(ctx context.Context, conn model.Connection, to delivery.Target, msg delivery.Message) error
}

// KM2 saves a report to KM2 through Studio's KM2 save path (the one km_save_document uses): the connection's allowed
// folder, a km2:save action log reservation with its per-run and per-connection caps, then KM2's document endpoint.
type KM2 interface {
	SaveReport(ctx context.Context, req KM2Request) (KM2Saved, error)
}

// KM2Request is one report to save to KM2.
type KM2Request struct {
	CrewID       string
	CrewName     string
	ScheduleID   *string
	ScheduleName string
	RunID        string
	DryRun       bool
	ConnectionID string
	Folder       string
	Title        string
	Content      string
	ExternalRef  string
	Metadata     map[string]any
}

// KM2Saved is how a KM2 save went.
type KM2Saved struct {
	DocumentID string
	Outcome    string
}

// SafeError is a refusal whose message is safe to store and show.
type SafeError struct{ Message string }

func (e *SafeError) Error() string { return e.Message }

// SnapshotRequest is a report about to be delivered, for its data snapshot.
type SnapshotRequest struct {
	CrewID     string
	ScheduleID *string
	Project    string
	Cadence    string
	Location   *time.Location
	Now        time.Time
}

// Snapshotter reads a project's tracked items for a report (internal/duereport). With no snapshot it says why, in
// words safe to record; the report is then delivered without one.
type Snapshotter interface {
	Snapshot(ctx context.Context, req SnapshotRequest) (json.RawMessage, string)
}

// Service delivers reports.
type Service struct {
	store *store.Store
	send  Sender
	km2   KM2
	snaps Snapshotter
	log   *slog.Logger
	now   func() time.Time
}

// WithSnapshots makes reports carry the data snapshot their designed page and exports are built from.
func (s *Service) WithSnapshots(sn Snapshotter) *Service {
	s.snaps = sn
	return s
}

// New builds the service. km2 may be nil (KM2 destinations then fail with a clear message).
func New(st *store.Store, send Sender, k KM2, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{store: st, send: send, km2: k, log: log, now: time.Now}
}

// CapError is a report refused because the run reached a cap.
type CapError struct{ Message string }

func (e *CapError) Error() string { return e.Message }

// Submission is one report from a run.
type Submission struct {
	Scope       store.RunScope
	WorkOrderID *string
	// WorkOrderNumber names the work order in footers; 0 when the run has none
	WorkOrderNumber int64
	// Location is the schedule's time zone, for the ISO week and a daily report's date
	Location *time.Location
	Report   model.ReportRequest
}

// Outcome is a delivered report; Existing is true when the run had already delivered this exact report.
type Outcome struct {
	Report   model.Report
	Existing bool
}

// Submit stores the report, keeps it as the project's note of its cadence (weekly-report or daily-report), and
// delivers it everywhere.
func (s *Service) Submit(ctx context.Context, sub Submission) (Outcome, error) {
	clean, err := model.NormalizeReport(sub.Report)
	if err != nil {
		return Outcome{}, err
	}
	dests, err := s.destinations(ctx, sub.Scope)
	if err != nil {
		return Outcome{}, err
	}
	if err := s.withinCaps(ctx, sub.Scope.RunID, len(dests)); err != nil {
		return Outcome{}, err
	}
	slug := model.ProjectSlug(clean.Project)
	now := s.now()
	week := model.ISOWeek(now, sub.Location)
	period := model.ReportPeriod(clean.Cadence, now, sub.Location)
	sum := sha256.Sum256([]byte(clean.Markdown))
	runID := sub.Scope.RunID
	var snapshot json.RawMessage
	snapshotNote := ""
	if s.snaps != nil {
		var why string
		snapshot, why = s.snaps.Snapshot(ctx, SnapshotRequest{CrewID: sub.Scope.CrewID, ScheduleID: sub.Scope.ScheduleID,
			Project: clean.Project, Cadence: clean.Cadence, Location: sub.Location, Now: now})
		snapshotNote = "included"
		if snapshot == nil {
			snapshotNote = "left out: " + why
		}
	}
	rep, existing, err := s.store.CreateReport(ctx, store.NewReport{
		CrewID: sub.Scope.CrewID, ScheduleID: sub.Scope.ScheduleID, RunID: runID, WorkOrderID: sub.WorkOrderID,
		Project: clean.Project, ProjectSlug: slug, Week: week, Cadence: clean.Cadence, Period: period,
		Title: model.ReportTitle(clean.Project, clean.Cadence, period), Markdown: clean.Markdown, SHA256: hex.EncodeToString(sum[:]),
		DryRun: sub.Scope.DryRun, Deliveries: plan(dests), Snapshot: snapshot,
	})
	if err != nil || existing {
		return Outcome{Report: rep, Existing: existing}, err
	}
	s.keepNote(ctx, sub.Scope, rep)
	run := deliveryRun{scope: stateScopeOf(sub.Scope, rep), runID: &runID, footer: footer(sub), dryRun: rep.DryRun,
		crewName: sub.Scope.CrewName, scheduleName: sub.Scope.ScheduleName, snapshotNote: snapshotNote}
	rep.Deliveries = s.deliverAll(ctx, rep, rep.Deliveries, run)
	return Outcome{Report: rep}, nil
}

// destinations are the run's schedule's destinations (none for a run no schedule started).
func (s *Service) destinations(ctx context.Context, scope store.RunScope) ([]model.ReportDestination, error) {
	if scope.ScheduleID == nil {
		return []model.ReportDestination{}, nil
	}
	return s.store.ReportDestinations(ctx, *scope.ScheduleID)
}

func (s *Service) withinCaps(ctx context.Context, runID string, destinations int) error {
	reports, sends, err := s.store.RunReportCounts(ctx, runID)
	if err != nil {
		return err
	}
	switch {
	case reports >= model.MaxReportsPerRun:
		return &CapError{Message: fmt.Sprintf("This run has already delivered %d reports, the most one run may. Put the rest in your output.", reports)}
	case sends+destinations > model.MaxDeliveriesPerRun:
		return &CapError{Message: fmt.Sprintf("This run has already sent %d report deliveries; another report would pass the limit of %d per run. "+
			"Put the rest in your output.", sends, model.MaxDeliveriesPerRun)}
	}
	return nil
}

// plan is where a report goes: the work order output first, then each destination as it is set now.
func plan(dests []model.ReportDestination) []model.ReportDelivery {
	out := []model.ReportDelivery{{Position: 0, Kind: model.DestinationWorkOrder, Recipients: []string{}}}
	for i, d := range dests {
		connID := d.ConnectionID
		out = append(out, model.ReportDelivery{Position: i + 1, Kind: d.Kind, ConnectionID: &connID, ConnectionName: d.ConnectionName,
			Recipients: d.Recipients, Folder: d.Folder})
	}
	return out
}

func footer(sub Submission) string {
	where := "Sent by Crew Studio (" + sub.Scope.CrewName
	if sub.Scope.ScheduleName != "" {
		where += ", " + sub.Scope.ScheduleName
	}
	where += ")"
	if sub.WorkOrderNumber > 0 {
		where += fmt.Sprintf(", work order #%d", sub.WorkOrderNumber)
	}
	return where + "."
}

func stateScopeOf(scope store.RunScope, rep model.Report) store.StateScope {
	return store.StateScope{CrewID: rep.CrewID, ScheduleID: scope.ScheduleID, DryRun: rep.DryRun}
}

// keepNote saves the report as the project's note of its cadence (weekly-report or daily-report), so the next
// report of that cadence builds on it; a daily report never touches the weekly note. A failure is logged; the
// report is still delivered.
func (s *Service) keepNote(ctx context.Context, scope store.RunScope, rep model.Report) {
	key := store.NoteKey{StateScope: stateScopeOf(scope, rep), Subject: model.ProjectSubject(rep.ProjectSlug), Name: model.ReportNoteOf(rep.Cadence)}
	runID := scope.RunID
	if _, err := s.store.PutNote(ctx, key, rep.Markdown, nil, store.NoteWriter{RunID: &runID, Name: "run"}); err != nil {
		s.log.Warn("keep report note", "run_id", runID, "project", rep.ProjectSlug, "err", err)
	}
}

// deliveryRun is what every delivery of one attempt shares.
type deliveryRun struct {
	scope  store.StateScope
	runID  *string
	footer string
	dryRun bool
	// crewName and scheduleName go into KM2 metadata, as km_save_document adds them
	crewName     string
	scheduleName string
	// snapshotNote says whether the report's data snapshot was included, or why not ("" on a retry)
	snapshotNote string
}

// deliverAll sends the given deliveries of a report, a few at a time, and returns them updated.
func (s *Service) deliverAll(ctx context.Context, rep model.Report, todo []model.ReportDelivery, run deliveryRun) []model.ReportDelivery {
	out := make([]model.ReportDelivery, len(todo))
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for i, d := range todo {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// A bug in one sender must not take the server down: that delivery fails instead
			defer func() {
				if p := recover(); p != nil {
					s.log.Error("report delivery panicked", "report_id", rep.ID, "kind", d.Kind, "panic", fmt.Sprint(p))
					d.Status, d.Message = model.DeliveryFailed, "The delivery failed on the server; see the service log."
					out[i] = d
				}
			}()
			out[i] = s.deliverOne(ctx, rep, d, run)
		}()
	}
	wg.Wait()
	return mergeDeliveries(rep.Deliveries, out)
}

// mergeDeliveries puts updated deliveries in place of the report's earlier copies.
func mergeDeliveries(all, updated []model.ReportDelivery) []model.ReportDelivery {
	out := make([]model.ReportDelivery, len(all))
	copy(out, all)
	for _, u := range updated {
		for i := range out {
			if out[i].ID == u.ID {
				out[i] = u
			}
		}
	}
	return out
}

// Retryable reports whether a delivery may be tried again: it failed, or it was left pending long ago. Dry-run
// reports, tracker deliveries (not built) and deliveries whose connection was deleted can never succeed.
func Retryable(rep model.Report, d model.ReportDelivery, now time.Time) bool {
	if rep.DryRun || d.Kind == model.DestinationTracker || (d.Kind != model.DestinationWorkOrder && d.ConnectionID == nil) {
		return false
	}
	return d.Status == model.DeliveryFailed || (d.Status == model.DeliveryPending && now.Sub(d.UpdatedAt) > StaleAfter)
}

// RetryResult is how a retry went.
type RetryResult struct {
	Reports []model.Report `json:"reports"`
	// Retried counts the deliveries tried again; Sent how many of them went out
	Retried int `json:"retried"`
	Sent    int `json:"sent"`
}

// retryTimeout bounds a whole retry, which keeps going when the person's request gives up waiting
const retryTimeout = 5 * time.Minute

// Retry tries a work order's failed deliveries again (never dry-run reports, never sent ones). It runs on its own
// context, so a slow destination cannot leave it half done when the request times out; a report that cannot be
// retried (its lock or record cannot be read) is skipped and logged.
func (s *Service) Retry(ctx context.Context, workOrderID string, workOrderNumber int64) (RetryResult, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), retryTimeout)
	defer cancel()
	reports, err := s.store.WorkOrderReports(ctx, workOrderID)
	if err != nil {
		return RetryResult{}, err
	}
	res := RetryResult{Reports: reports}
	for i, rep := range reports {
		err := s.store.WithReportLock(ctx, rep.ID, func() error {
			fresh, err := s.store.GetReport(ctx, rep.ID)
			if err != nil {
				return err
			}
			res.Reports[i] = s.retryReport(ctx, fresh, workOrderNumber, &res)
			return nil
		})
		if err != nil {
			s.log.Error("retry report deliveries", "report_id", rep.ID, "err", err)
		}
	}
	return res, nil
}

func (s *Service) retryReport(ctx context.Context, rep model.Report, workOrderNumber int64, res *RetryResult) model.Report {
	var todo []model.ReportDelivery
	now := s.now()
	for _, d := range rep.Deliveries {
		if Retryable(rep, d, now) {
			todo = append(todo, d)
		}
	}
	if len(todo) == 0 {
		return rep
	}
	ft := "Sent by Crew Studio (retried)."
	if workOrderNumber > 0 {
		ft = fmt.Sprintf("Sent by Crew Studio, work order #%d (retried).", workOrderNumber)
	}
	run := deliveryRun{scope: store.StateScope{CrewID: rep.CrewID, ScheduleID: rep.ScheduleID}, runID: rep.RunID, footer: ft}
	run.crewName, run.scheduleName = s.namesOf(ctx, rep)
	rep.Deliveries = s.deliverAll(ctx, rep, todo, run)
	res.Retried += len(todo)
	for _, d := range rep.Deliveries {
		if d.Status == model.DeliverySent && slicesContainsID(todo, d.ID) {
			res.Sent++
		}
	}
	return rep
}

func slicesContainsID(ds []model.ReportDelivery, id string) bool {
	for _, d := range ds {
		if d.ID == id {
			return true
		}
	}
	return false
}

// ErrNoKM2 is a KM2 destination on a server without the KM2 service.
var ErrNoKM2 = errors.New("KM2 is not available on this server")

// namesOf are the crew's and schedule's names for KM2 metadata on a retry ("" when gone).
func (s *Service) namesOf(ctx context.Context, rep model.Report) (string, string) {
	crew, schedule := "", ""
	if c, err := s.store.GetCrew(ctx, rep.CrewID); err == nil {
		crew = c.Name
	}
	if rep.ScheduleID != nil {
		if sc, err := s.store.GetSchedule(ctx, *rep.ScheduleID); err == nil {
			schedule = sc.Name
		}
	}
	return crew, schedule
}
