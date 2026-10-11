package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"crewstudio/internal/auth"
	"crewstudio/internal/duereport"
	"crewstudio/internal/model"
	"crewstudio/internal/reports"
)

// The reports a work order's runs delivered, with each delivery's status, and "Retry failed deliveries", which
// sends only what failed: the action log keeps what was sent, so nothing goes out twice.

const (
	reportRetriesPerMinute = 6
	reportRetryBurst       = 3
)

var reportRetries = newKeyedLimiter(rate.Every(time.Minute/reportRetriesPerMinute), reportRetryBurst)

// workOrderReportRoutes hangs off /work-orders (see workOrderRoutes).
func (s *Server) workOrderReportRoutes(r chi.Router) {
	r.With(s.need(auth.WorkOrdersView)).Get("/{id}/reports", s.workOrderReports)
	r.With(s.need(auth.WorkOrdersCreate)).Post("/{id}/reports/retry", s.retryWorkOrderReports)
	r.With(s.need(auth.WorkOrdersView)).Get("/{id}/reports/{reportId}/html", s.workOrderReportHTML)
}

// reportView is a report with whether each delivery can be retried.
type reportView struct {
	model.Report
	// Retryable counts the deliveries Retry would try again
	Retryable int `json:"retryable"`
}

type workOrderReportsView struct {
	Reports []reportView `json:"reports"`
	// Retried and Sent are set by a retry
	Retried int `json:"retried"`
	Sent    int `json:"sent"`
}

func viewReports(items []model.Report) []reportView {
	out := make([]reportView, len(items))
	now := time.Now()
	for i, rep := range items {
		out[i] = reportView{Report: rep}
		for _, d := range rep.Deliveries {
			if reports.Retryable(rep, d, now) {
				out[i].Retryable++
			}
		}
	}
	return out
}

func (s *Server) workOrderReports(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	items, err := s.store.WorkOrderReports(r.Context(), wo.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, workOrderReportsView{Reports: viewReports(items)})
}

func (s *Server) retryWorkOrderReports(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	p := principal(r)
	if !reportRetries.allow(p.UserID) {
		tooMany(w, "Deliveries are being retried too quickly; wait a moment.")
		return
	}
	res, err := s.reports.Retry(r.Context(), wo.ID, wo.Number)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.audit(r, "report_deliveries_retried", p.UserID, p.Name, map[string]any{"work_order_id": wo.ID, "work_order_number": wo.Number,
		"retried": res.Retried, "sent": res.Sent})
	ok(w, workOrderReportsView{Reports: viewReports(res.Reports), Retried: res.Retried, Sent: res.Sent})
}

// workOrderReportHTML downloads a report as its self-contained HTML page (the email's HTML part), with the same
// access check as the work order's Reports list. The page has no scripts; a strict CSP keeps it from loading anything.
func (s *Server) workOrderReportHTML(w http.ResponseWriter, r *http.Request) {
	wo, found := s.loadWorkOrder(w, r)
	if !found {
		return
	}
	reportID := chi.URLParam(r, "reportId")
	items, err := s.store.WorkOrderReports(r.Context(), wo.ID)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	i := slices.IndexFunc(items, func(rep model.Report) bool { return rep.ID == reportID })
	if i < 0 {
		fail(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	rep := items[i]
	snap, err := duereport.Parse(rep.Snapshot)
	if err != nil {
		s.log.Warn("read report snapshot", "report_id", rep.ID, "err", err)
		snap = nil
	}
	page, err := duereport.HTML(duereport.Report{Title: rep.Title, Project: rep.Project, Cadence: rep.Cadence, Period: rep.Period,
		DryRun: rep.DryRun, Narrative: rep.Markdown}, snap)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	name := fmt.Sprintf("%s-%s-%s.html", rep.ProjectSlug, rep.Cadence, rep.Period)
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+strings.Map(safeFileRune, name)+`"`)
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(page))
}

// safeFileRune keeps a download name to letters, digits, dashes and dots.
func safeFileRune(r rune) rune {
	if r == '-' || r == '.' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
		return r
	}
	return '-'
}
