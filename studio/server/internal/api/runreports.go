package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"crewstudio/internal/model"
	"crewstudio/internal/reports"
	"crewstudio/internal/store"
)

// deliver_report (design section 12): a run hands Studio one project's finished report, and Studio sends it to the
// work order output and every destination on the run's schedule. Everything delivery must respect is enforced
// here and in internal/reports, not by the prompt: dry run (nothing is sent; each delivery is "would send" with a
// dry-run log entry), the size cap, the caps per run, the crew's right to use each connection, and one report per
// project per run.

// reportRunRoutes hangs off /run-state (see runStateRoutes).
func (s *Server) reportRunRoutes(r chi.Router) {
	r.Post("/reports", s.refuseChat("deliver_report", s.runDeliverReport))
}

// deliveryView is one delivery as the agent sees it.
type deliveryView struct {
	Destination string `json:"destination"`
	Target      string `json:"target"`
	Status      string `json:"status"`
	Message     string `json:"message"`
}

// reportResult answers deliver_report.
type reportResult struct {
	ReportID   string         `json:"report_id"`
	Project    string         `json:"project"`
	Week       string         `json:"week"`
	Cadence    string         `json:"cadence"`
	Period     string         `json:"period"`
	DryRun     bool           `json:"dry_run"`
	Deliveries []deliveryView `json:"deliveries"`
	Message    string         `json:"message"`
}

func (s *Server) runDeliverReport(w http.ResponseWriter, r *http.Request) {
	const tool = "deliver_report"
	var in model.ReportRequest
	if !decode(w, r, &in) {
		return
	}
	if c := strings.ToLower(strings.TrimSpace(in.Cadence)); c != "" && !model.ValidCadence(c) {
		s.journal(r, tool, "Report: refused: the cadence is not weekly or daily", "")
		fail(w, http.StatusBadRequest, "bad_cadence", `cadence must be "weekly" (the default) or "daily".`)
		return
	}
	clean, err := model.NormalizeReport(in)
	if err != nil {
		s.journal(r, tool, "Report: refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	if !s.withinWriteQuota(w, r, tool) {
		return
	}
	sub, err := s.reportSubmission(r, clean)
	var out reports.Outcome
	if err == nil {
		out, err = s.reports.Submit(r.Context(), sub)
	}
	var capErr *reports.CapError
	switch {
	case errors.As(err, &capErr):
		s.journal(r, tool, "Report: refused: "+capErr.Message, "")
		fail(w, http.StatusTooManyRequests, "report_cap", capErr.Message)
		return
	case err != nil:
		s.journal(r, tool, "Report: "+clean.Project+" refused: "+problemText(err), "")
		failErr(w, r, s.log, err)
		return
	}
	res := reportResultOf(out)
	if out.Existing {
		s.journal(r, tool, "Report: "+clean.Project+" was already delivered in this run; nothing was sent again", "")
	} else {
		s.journalDeliveries(r, out.Report)
	}
	if out.Existing {
		ok(w, res)
		return
	}
	created(w, res)
}

// reportSubmission gathers what a report needs from the run: its work order and its schedule's time zone.
func (s *Server) reportSubmission(r *http.Request, report model.ReportRequest) (reports.Submission, error) {
	call := runCallOf(r)
	sub := reports.Submission{Scope: call.scope, Report: report, Location: time.UTC}
	run, err := s.store.GetRun(r.Context(), call.scope.RunID)
	if err != nil {
		return sub, err
	}
	if run.WorkOrderID != nil {
		wo, err := s.store.GetWorkOrder(r.Context(), *run.WorkOrderID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return sub, err
		}
		if err == nil {
			sub.WorkOrderID, sub.WorkOrderNumber = &wo.ID, wo.Number
		}
	}
	if call.scope.ScheduleID != nil {
		sc, err := s.store.GetSchedule(r.Context(), *call.scope.ScheduleID)
		if err != nil {
			return sub, err
		}
		if loc, err := time.LoadLocation(sc.Timezone); err == nil {
			sub.Location = loc
		}
	}
	return sub, nil
}

func reportResultOf(out reports.Outcome) reportResult {
	rep := out.Report
	res := reportResult{ReportID: rep.ID, Project: rep.Project, Week: rep.Week, Cadence: rep.Cadence, Period: rep.Period, DryRun: rep.DryRun,
		Deliveries: []deliveryView{}}
	sent, failed := 0, 0
	for _, d := range rep.Deliveries {
		res.Deliveries = append(res.Deliveries, deliveryView{Destination: d.Kind, Target: d.Target(), Status: d.Status, Message: d.Message})
		switch d.Status {
		case model.DeliverySent, model.DeliveryWouldSend:
			sent++
		case model.DeliveryFailed:
			failed++
		}
	}
	switch {
	case out.Existing:
		res.Message = "This report was already delivered in this run; nothing was sent again."
	case rep.DryRun:
		res.Message = "Dry run: nothing was sent. List the deliveries under \"Would post\" in your output, and include the report there too."
	case failed > 0:
		res.Message = fmt.Sprintf("Delivered to %d of %d destinations; %d failed and can be retried from the work order. "+
			"Include the report and these results in your output.", sent, len(rep.Deliveries), failed)
	default:
		res.Message = "Delivered. Include the report and these results in your output."
	}
	return res
}

// journalDeliveries writes one line per delivery in the run's journal.
func (s *Server) journalDeliveries(r *http.Request, rep model.Report) {
	for _, d := range rep.Deliveries {
		line := fmt.Sprintf("Report: %s %s %s", rep.Project, statusVerb(d.Status), d.Target())
		if d.Status == model.DeliveryFailed {
			line += ": " + d.Message
		}
		s.journal(r, "deliver_report", line, "")
	}
}

func statusVerb(status string) string {
	switch status {
	case model.DeliverySent:
		return "delivered to"
	case model.DeliveryWouldSend:
		return "(dry run) would be sent to"
	case model.DeliveryFailed:
		return "failed for"
	}
	return "pending for"
}
