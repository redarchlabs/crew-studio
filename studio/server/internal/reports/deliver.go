package reports

import (
	"context"
	"errors"
	"fmt"
	"time"

	"crewstudio/internal/delivery"
	"crewstudio/internal/duereport"
	"crewstudio/internal/km2"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

// deliveryTimeout bounds one delivery once started, and its record, independent of whoever asked (a run's tool call
// may give up waiting; the delivery and its log entry still finish).
const deliveryTimeout = 110 * time.Second

// deliverOne sends one delivery (or, in a dry run, records what it would send) and returns it updated.
func (s *Service) deliverOne(ctx context.Context, rep model.Report, d model.ReportDelivery, run deliveryRun) model.ReportDelivery {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryTimeout)
	defer cancel()
	status, message := s.attempt(ctx, rep, d, run)
	updated, err := s.store.FinishDelivery(ctx, d.ID, status, message, s.now())
	if err != nil {
		s.log.Error("record report delivery", "report_id", rep.ID, "delivery_id", d.ID, "status", status, "err", err)
		d.Status, d.Message = status, message
		return d
	}
	return updated
}

// attempt does the delivery and says how it went.
func (s *Service) attempt(ctx context.Context, rep model.Report, d model.ReportDelivery, run deliveryRun) (string, string) {
	if d.Kind == model.DestinationWorkOrder {
		s.logDelivery(ctx, rep, d, run, map[string]any{"outcome": "kept"})
		return model.DeliverySent, "Kept with the work order; it is also in the run's output."
	}
	if d.Kind == model.DestinationTracker {
		// Writing status back to Jira or Linear is deferred (design section 12); nothing is sent
		return model.DeliveryFailed, "Sending reports to the tracker is not available yet."
	}
	conn, problem := s.usableConnection(ctx, rep.CrewID, d)
	if problem != "" {
		return model.DeliveryFailed, problem
	}
	if run.dryRun {
		// KM2 records its own dry-run save (and counts it), as km_save_document does
		if d.Kind == model.DestinationKM2 {
			if _, err := s.sendKM2(ctx, conn, rep, d, run, true); err != nil {
				return model.DeliveryFailed, failureText(err)
			}
		}
		s.logDelivery(ctx, rep, d, run, map[string]any{"would_send": true})
		return model.DeliveryWouldSend, "Dry run: would send to " + d.Target() + ". Nothing was sent."
	}
	return s.sendReserved(ctx, conn, rep, d, run)
}

// sendReserved records the delivery in the action log as "sending" before anything goes out, then sends it and
// finishes the entry; a failed send removes the entry so it can be retried. A delivery whose entry already exists is
// never sent again: it was sent, or it was interrupted mid-send and may have arrived (at most once, not twice).
func (s *Service) sendReserved(ctx context.Context, conn model.Connection, rep model.Report, d model.ReportDelivery, run deliveryRun) (string, string) {
	scope := run.scope
	scope.DryRun = false
	entry, duplicate, err := s.store.RecordAction(ctx, scope, run.runID, deliveryRecord(rep, d, map[string]any{"outcome": outcomeSending}, run.snapshotNote))
	switch {
	case err != nil:
		s.log.Error("reserve report delivery", "report_id", rep.ID, "kind", d.Kind, "err", err)
		return model.DeliveryFailed, "The delivery could not be recorded before sending, so nothing was sent; try again."
	case duplicate && entry.Details["outcome"] == outcomeSending:
		return model.DeliverySent, "An earlier attempt was interrupted while sending, so it may have arrived; it is not sent again."
	case duplicate:
		return model.DeliverySent, "Already sent (recorded in the action log)."
	}
	extra, err := s.send1(ctx, conn, rep, d, run)
	if err != nil {
		if derr := s.store.DeleteAction(ctx, rep.CrewID, entry.ID); derr != nil {
			s.log.Error("drop report delivery reservation", "entry", entry.ID, "err", derr)
		}
		return model.DeliveryFailed, failureText(err)
	}
	extra["outcome"] = "sent"
	if err := s.store.SetActionDetails(ctx, rep.CrewID, entry.ID, deliveryRecord(rep, d, extra, run.snapshotNote).Details); err != nil {
		s.log.Error("finish report delivery in the action log", "entry", entry.ID, "err", err)
	}
	return model.DeliverySent, "Sent to " + d.Target() + "."
}

// outcomeSending marks a reserved delivery that has not finished sending.
const outcomeSending = "sending"

// usableConnection loads a delivery's connection and checks the crew may still use it.
func (s *Service) usableConnection(ctx context.Context, crewID string, d model.ReportDelivery) (model.Connection, string) {
	if d.ConnectionID == nil {
		return model.Connection{}, "The connection " + d.ConnectionName + " was deleted."
	}
	use, err := s.store.ConnectionForCrew(ctx, *d.ConnectionID, crewID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return model.Connection{}, "The connection " + d.ConnectionName + " was deleted."
	case err != nil:
		s.log.Error("check report connection", "connection_id", *d.ConnectionID, "err", err)
		return model.Connection{}, "The connection could not be checked; try again."
	case use.Connection.Kind != d.Kind:
		return model.Connection{}, fmt.Sprintf("%s is a %s connection, not a %s one.", use.Connection.Name, use.Connection.Kind, d.Kind)
	case !use.Connection.Enabled:
		return model.Connection{}, "The connection " + use.Connection.Name + " is turned off."
	case !use.Linked:
		return model.Connection{}, "The crew may no longer use the connection " + use.Connection.Name + " (choose it on the crew's State tab)."
	case !use.SharesArea:
		return model.Connection{}, "The connection " + use.Connection.Name + " shares no functional area with the crew."
	}
	return use.Connection, ""
}

// send1 sends one delivery; extra is what to add to its action log details.
func (s *Service) send1(ctx context.Context, conn model.Connection, rep model.Report, d model.ReportDelivery, run deliveryRun) (map[string]any, error) {
	if d.Kind == model.DestinationKM2 {
		return s.sendKM2(ctx, conn, rep, d, run, false)
	}
	msg := s.message(rep, d.Kind, run.footer)
	return map[string]any{}, s.send.Send(ctx, conn, delivery.Target{Recipients: d.Recipients}, msg)
}

func (s *Service) sendKM2(ctx context.Context, conn model.Connection, rep model.Report, d model.ReportDelivery, run deliveryRun, dryRun bool) (map[string]any, error) {
	if s.km2 == nil {
		return nil, ErrNoKM2
	}
	if rep.RunID == nil {
		return nil, &SafeError{Message: "The run that wrote this report was deleted, so it cannot be saved to KM2."}
	}
	ref := model.ReportExternalRef(rep.ProjectSlug, rep.Cadence, rep.Period)
	meta := map[string]any{"kind": rep.Cadence + "-report", "project": rep.ProjectSlug, "week": rep.Week, "period": rep.Period, "report_id": rep.ID}
	if rep.Cadence == model.CadenceDaily {
		meta["date"] = rep.Period
	}
	saved, err := s.km2.SaveReport(ctx, KM2Request{CrewID: rep.CrewID, CrewName: run.crewName, ScheduleID: rep.ScheduleID,
		ScheduleName: run.scheduleName, RunID: *rep.RunID, DryRun: dryRun,
		ConnectionID: conn.ID, Folder: d.Folder, Title: rep.Title, Content: s.km2Content(rep), ExternalRef: ref,
		Metadata: meta})
	if err != nil {
		return nil, err
	}
	return map[string]any{"external_ref": ref, "document_id": saved.DocumentID, "outcome": saved.Outcome}, nil
}

// exportOf is what the duereport exports need of a report.
func exportOf(rep model.Report) duereport.Report {
	return duereport.Report{Title: rep.Title, Project: rep.Project, Cadence: rep.Cadence, Period: rep.Period, DryRun: rep.DryRun, Narrative: rep.Markdown}
}

// snapshotOf reads a report's snapshot; a broken one is logged and treated as none.
func (s *Service) snapshotOf(rep model.Report) *duereport.Snapshot {
	snap, err := duereport.Parse(rep.Snapshot)
	if err != nil {
		s.log.Warn("read report snapshot", "report_id", rep.ID, "err", err)
		return nil
	}
	return snap
}

// km2Content is what KM2 keeps: the Markdown built from the snapshot and the narrative, or the report as the model
// wrote it when it has no snapshot.
func (s *Service) km2Content(rep model.Report) string {
	if snap := s.snapshotOf(rep); snap != nil {
		return duereport.Markdown(exportOf(rep), snap)
	}
	return rep.Markdown
}

// message is what a Slack, Teams or email delivery sends: with a snapshot, chat gets the compact summary (counts,
// the most overdue items, the narrative) and email the whole report as text plus its styled page.
func (s *Service) message(rep model.Report, kind, footer string) delivery.Message {
	snap := s.snapshotOf(rep)
	msg := delivery.Message{Title: rep.Title, Markdown: rep.Markdown, Footer: footer}
	if snap == nil {
		return msg
	}
	r := exportOf(rep)
	switch kind {
	case "email":
		msg.Markdown = duereport.Markdown(r, snap)
		if html, err := duereport.HTML(r, snap); err == nil {
			msg.HTML = html
		} else {
			s.log.Warn("render report html", "report_id", rep.ID, "err", err)
		}
	default:
		msg.Markdown = duereport.Summary(r, snap)
	}
	return msg
}

// failureText is what a failed delivery says, safe to store and show.
func failureText(err error) string {
	var fe *km2.FolderError
	var de *delivery.Error
	var se *SafeError
	switch {
	case errors.As(err, &se):
		return se.Message
	case errors.As(err, &de):
		return capitalize(de.Message) + "."
	case errors.As(err, &fe):
		return fe.Message
	case errors.Is(err, ErrNoKM2):
		return "KM2 is not available on this server."
	}
	return capitalize(km2.SafeMessage(err)) + "."
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	if c := s[0]; c >= 'a' && c <= 'z' {
		return string(c-'a'+'A') + s[1:]
	}
	return s
}

// logDelivery records a delivery (or, in a dry run, a would-send) in the action log under its idempotency key. A
// failure is logged; the delivery itself stands.
func (s *Service) logDelivery(ctx context.Context, rep model.Report, d model.ReportDelivery, run deliveryRun, extra map[string]any) {
	scope := run.scope
	scope.DryRun = run.dryRun
	if _, _, err := s.store.RecordAction(ctx, scope, run.runID, deliveryRecord(rep, d, extra, run.snapshotNote)); err != nil {
		s.log.Error("record report delivery in the action log", "report_id", rep.ID, "kind", d.Kind, "err", err)
	}
}

// deliveryRecord is a delivery's action log entry: report:<kind> about project:<slug>, under the delivery's key.
func deliveryRecord(rep model.Report, d model.ReportDelivery, extra map[string]any, snapshotNote string) model.LogRecord {
	details := map[string]any{"report_id": rep.ID, "project": rep.Project, "week": rep.Week, "cadence": rep.Cadence, "period": rep.Period,
		"destination": d.Kind,
		"target":      d.Target(), "bytes": len(rep.Markdown)}
	if d.ConnectionID != nil {
		details["connection_id"], details["connection"] = *d.ConnectionID, d.ConnectionName
	}
	if snapshotNote != "" {
		details["snapshot"] = snapshotNote
	}
	for k, v := range extra {
		details[k] = v
	}
	return model.LogRecord{Subject: model.ProjectSubject(rep.ProjectSlug), Action: model.ActionReport(d.Kind), Details: details,
		IdempotencyKey: d.IdempotencyKey}
}
