package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// Report delivery (docs/crew-state-and-pmo-design.md, section 12): a schedule's report destinations, the reports
// runs delivered, and each delivery's outcome.

const destinationSelect = `SELECT d.id, d.schedule_id, d.position, d.kind, d.connection_id, COALESCE(x.name, ''), d.recipients,
	d.folder, d.created_at FROM report_destinations d LEFT JOIN connections x ON x.id = d.connection_id`

func scanDestination(row pgx.Row) (model.ReportDestination, error) {
	var d model.ReportDestination
	err := row.Scan(&d.ID, &d.ScheduleID, &d.Position, &d.Kind, &d.ConnectionID, &d.ConnectionName, &d.Recipients, &d.Folder, &d.CreatedAt)
	d.Recipients = orEmpty(d.Recipients)
	return d, err
}

// ReportDestinations returns a schedule's report destinations in order.
func (s *Store) ReportDestinations(ctx context.Context, scheduleID string) ([]model.ReportDestination, error) {
	return reportDestinations(ctx, s.pool, scheduleID)
}

func reportDestinations(ctx context.Context, q queryer, scheduleID string) ([]model.ReportDestination, error) {
	rows, err := q.Query(ctx, destinationSelect+` WHERE d.schedule_id = $1 ORDER BY d.position`, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("list report destinations: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ReportDestination, error) { return scanDestination(r) })
	if err != nil {
		return nil, fmt.Errorf("read report destinations: %w", err)
	}
	return orEmpty(items), nil
}

// SetReportDestinations replaces a schedule's report destinations (already validated) and returns them.
func (s *Store) SetReportDestinations(ctx context.Context, scheduleID string, in []model.ReportDestinationInput) ([]model.ReportDestination, error) {
	var out []model.ReportDestination
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if err := setReportDestinations(ctx, tx, scheduleID, in); err != nil {
			return err
		}
		var err error
		out, err = reportDestinations(ctx, tx, scheduleID)
		return err
	})
	return out, err
}

func setReportDestinations(ctx context.Context, tx pgx.Tx, scheduleID string, in []model.ReportDestinationInput) error {
	if _, err := tx.Exec(ctx, `DELETE FROM report_destinations WHERE schedule_id = $1`, scheduleID); err != nil {
		return fmt.Errorf("clear report destinations: %w", err)
	}
	for i, d := range in {
		if _, err := tx.Exec(ctx, `INSERT INTO report_destinations (schedule_id, position, kind, connection_id, recipients, folder)
			VALUES ($1, $2, $3, $4, $5, $6)`, scheduleID, i, d.Kind, d.ConnectionID, orEmpty(d.Recipients), d.Folder); err != nil {
			return mapErr(err, "report destination", "")
		}
	}
	return nil
}

const reportSelect = `SELECT id, crew_id, schedule_id, run_id, work_order_id, project, project_slug, week, cadence, period, title,
	markdown, length(markdown), dry_run, created_at, snapshot FROM reports`

func scanReport(row pgx.Row) (model.Report, error) {
	var r model.Report
	err := row.Scan(&r.ID, &r.CrewID, &r.ScheduleID, &r.RunID, &r.WorkOrderID, &r.Project, &r.ProjectSlug, &r.Week, &r.Cadence, &r.Period, &r.Title,
		&r.Markdown, &r.Bytes, &r.DryRun, &r.CreatedAt, &r.Snapshot)
	r.Deliveries = []model.ReportDelivery{}
	return r, err
}

const deliverySelect = `SELECT id, report_id, position, kind, connection_id, connection_name, recipients, folder, status, message,
	attempts, idempotency_key, sent_at, updated_at FROM report_deliveries`

func scanReportDelivery(row pgx.Row) (model.ReportDelivery, error) {
	var d model.ReportDelivery
	err := row.Scan(&d.ID, &d.ReportID, &d.Position, &d.Kind, &d.ConnectionID, &d.ConnectionName, &d.Recipients, &d.Folder,
		&d.Status, &d.Message, &d.Attempts, &d.IdempotencyKey, &d.SentAt, &d.UpdatedAt)
	d.Recipients = orEmpty(d.Recipients)
	return d, err
}

// NewReport is a report about to be delivered, with where each delivery goes.
type NewReport struct {
	CrewID      string
	ScheduleID  *string
	RunID       string
	WorkOrderID *string
	Project     string
	ProjectSlug string
	Week        string
	// Cadence is weekly or daily; Period what the report covers
	Cadence string
	Period  string
	// Snapshot is the encoded duereport snapshot, nil without one
	Snapshot []byte
	Title    string
	Markdown string
	SHA256   string
	DryRun   bool
	// Deliveries in order; their status starts as pending
	Deliveries []model.ReportDelivery
}

// CreateReport stores a report and its pending deliveries. When the run already delivered a report for the project,
// nothing is stored and that report comes back with existing true.
func (s *Store) CreateReport(ctx context.Context, in NewReport) (model.Report, bool, error) {
	var id, sum string
	existing := false
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO reports (crew_id, schedule_id, run_id, work_order_id, project, project_slug, week, cadence,
			period, title, markdown, content_sha256, dry_run, snapshot) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (run_id, project_slug, cadence) DO NOTHING RETURNING id`,
			in.CrewID, in.ScheduleID, in.RunID, in.WorkOrderID, in.Project, in.ProjectSlug, in.Week, in.Cadence, in.Period, in.Title,
			in.Markdown, in.SHA256, in.DryRun, nullJSON(in.Snapshot)).Scan(&id)
		if isNoRows(err) {
			existing = true
			return tx.QueryRow(ctx, `SELECT id, content_sha256 FROM reports WHERE run_id = $1 AND project_slug = $2 AND cadence = $3`,
				in.RunID, in.ProjectSlug, in.Cadence).Scan(&id, &sum)
		}
		if err != nil {
			return mapErr(err, "report", "")
		}
		for _, d := range in.Deliveries {
			if _, err := tx.Exec(ctx, `INSERT INTO report_deliveries (report_id, position, kind, connection_id, connection_name, recipients,
				folder, status, idempotency_key) VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', $8)`,
				id, d.Position, d.Kind, d.ConnectionID, d.ConnectionName, orEmpty(d.Recipients), d.Folder, model.DeliveryKey(id, d.Position)); err != nil {
				return mapErr(err, "report delivery", "")
			}
		}
		return nil
	})
	if err != nil {
		return model.Report{}, false, err
	}
	r, err := s.GetReport(ctx, id)
	if existing && err == nil && sum != in.SHA256 {
		return r, true, &ConflictError{Code: "report_exists",
			Message: fmt.Sprintf("This run already delivered a %s report for %s; a run delivers each project's report of each cadence once.", r.Cadence, r.Project)}
	}
	return r, existing, err
}

// nullJSON stores an empty snapshot as SQL NULL.
func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// ReportSnapshot is an earlier report's stored snapshot.
type ReportSnapshot struct {
	ReportID string
	Snapshot []byte
}

// ReportSnapshots returns the newest snapshots of a crew's reports for a project and cadence, newest first.
func (s *Store) ReportSnapshots(ctx context.Context, crewID, projectSlug, cadence string, limit int) ([]ReportSnapshot, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, snapshot FROM reports WHERE crew_id = $1 AND project_slug = $2 AND cadence = $3
		AND snapshot IS NOT NULL ORDER BY created_at DESC LIMIT $4`, crewID, projectSlug, cadence, limit)
	if err != nil {
		return nil, fmt.Errorf("list report snapshots: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ReportSnapshot, error) {
		var rs ReportSnapshot
		err := r.Scan(&rs.ReportID, &rs.Snapshot)
		return rs, err
	})
	if err != nil {
		return nil, fmt.Errorf("read report snapshots: %w", err)
	}
	return out, nil
}

// GetReport returns a report with its deliveries.
func (s *Store) GetReport(ctx context.Context, id string) (model.Report, error) {
	r, err := scanReport(s.pool.QueryRow(ctx, reportSelect+` WHERE id = $1`, id))
	if err != nil {
		return r, mapErr(err, "report", "")
	}
	if r.Deliveries, err = s.reportDeliveries(ctx, []string{id}); err != nil {
		return r, err
	}
	return r, nil
}

func (s *Store) reportDeliveries(ctx context.Context, reportIDs []string) ([]model.ReportDelivery, error) {
	rows, err := s.pool.Query(ctx, deliverySelect+` WHERE report_id = ANY($1::uuid[]) ORDER BY report_id, position`, reportIDs)
	if err != nil {
		return nil, fmt.Errorf("list report deliveries: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ReportDelivery, error) { return scanReportDelivery(r) })
	if err != nil {
		return nil, fmt.Errorf("read report deliveries: %w", err)
	}
	return orEmpty(items), nil
}

// WorkOrderReports returns the reports a work order's runs delivered, oldest first, with their deliveries.
func (s *Store) WorkOrderReports(ctx context.Context, workOrderID string) ([]model.Report, error) {
	rows, err := s.pool.Query(ctx, reportSelect+` WHERE work_order_id = $1 ORDER BY created_at, project`, workOrderID)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	reports, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Report, error) { return scanReport(r) })
	if err != nil {
		return nil, fmt.Errorf("read reports: %w", err)
	}
	if len(reports) == 0 {
		return []model.Report{}, nil
	}
	ids := make([]string, len(reports))
	byID := map[string]int{}
	for i, r := range reports {
		ids[i], byID[r.ID] = r.ID, i
	}
	deliveries, err := s.reportDeliveries(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, d := range deliveries {
		i := byID[d.ReportID]
		reports[i].Deliveries = append(reports[i].Deliveries, d)
	}
	return reports, nil
}

// RunReportCounts says how many reports a run delivered and how many sends (not counting the work order output)
// they made.
func (s *Store) RunReportCounts(ctx context.Context, runID string) (reports, sends int, err error) {
	err = s.pool.QueryRow(ctx, `SELECT count(DISTINCT r.id), count(d.id) FILTER (WHERE d.kind <> 'work_order')
		FROM reports r LEFT JOIN report_deliveries d ON d.report_id = r.id WHERE r.run_id = $1`, runID).Scan(&reports, &sends)
	if err != nil {
		return 0, 0, fmt.Errorf("count run reports: %w", err)
	}
	return reports, sends, nil
}

// FinishDelivery records the outcome of one attempt at a delivery.
func (s *Store) FinishDelivery(ctx context.Context, id, status, message string, at time.Time) (model.ReportDelivery, error) {
	var sentAt *time.Time
	if status == model.DeliverySent || status == model.DeliveryWouldSend {
		sentAt = &at
	}
	d, err := scanReportDelivery(s.pool.QueryRow(ctx, `UPDATE report_deliveries SET status = $2, message = $3, sent_at = COALESCE($4, sent_at),
		attempts = attempts + CASE WHEN $2 IN ('sent', 'failed') THEN 1 ELSE 0 END, updated_at = $5
		WHERE id = $1 RETURNING id, report_id, position, kind, connection_id, connection_name, recipients, folder, status, message,
		attempts, idempotency_key, sent_at, updated_at`, id, status, message, sentAt, at))
	return d, mapErr(err, "report delivery", "")
}

// WithReportLock runs fn while holding a database advisory lock for one report, so a retry cannot race another
// retry (or the first delivery) and send the same message twice.
func (s *Store) WithReportLock(ctx context.Context, reportID string, fn func() error) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("lock report: %w", err)
	}
	defer conn.Release()
	const key = `hashtextextended('report:' || $1, 0)`
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(`+key+`)`, reportID); err != nil {
		return fmt.Errorf("lock report: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(`+key+`)`, reportID); err != nil {
			_ = conn.Conn().Close(unlockCtx)
		}
	}()
	return fn()
}

// SetActionDetails replaces an action log entry's details (a reserved delivery, once sent).
func (s *Store) SetActionDetails(ctx context.Context, crewID, id string, details map[string]any) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode action details: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE action_log SET details = $3 WHERE crew_id = $1 AND id = $2`, crewID, id, raw)
	if err != nil {
		return fmt.Errorf("update action details: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
