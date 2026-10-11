package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

// targetAreas is the SQL array of the areas of a schedule's or trigger's crew (alias x).
const targetAreas = `ARRAY(SELECT area_id::text FROM crew_areas WHERE crew_id = x.crew_id ORDER BY area_id)`

const targetScope = `($1::boolean
	OR EXISTS (SELECT 1 FROM crew_areas a WHERE a.crew_id = x.crew_id AND a.area_id = ANY($2::uuid[])))`

const targetJoins = ` JOIN crews c ON c.id = x.crew_id`

// Schedules

const scheduleSelect = `SELECT x.id, x.name, x.description, x.crew_id, c.name,
	x.cron, x.timezone, x.brief, x.inputs, x.budget_usd, x.enabled, x.overlap_policy, x.run_as, x.run_as_name,
	` + targetAreas + `, x.next_run_at, x.last_run_at, x.last_work_order_id, x.last_error, x.created_at, x.updated_at
	FROM schedules x` + targetJoins

func scanSchedule(row pgx.Row) (model.Schedule, error) {
	var s model.Schedule
	err := row.Scan(&s.ID, &s.Name, &s.Description, &s.CrewID, &s.TargetName, &s.Cron,
		&s.Timezone, &s.Brief, &s.Inputs, &s.BudgetUSD, &s.Enabled, &s.OverlapPolicy, &s.RunAs, &s.RunAsName, &s.AreaIDs,
		&s.NextRunAt, &s.LastRunAt, &s.LastWorkOrderID, &s.LastError, &s.CreatedAt, &s.UpdatedAt)
	if s.Inputs == nil {
		s.Inputs = map[string]string{}
	}
	s.AreaIDs = orEmpty(s.AreaIDs)
	return s, err
}

func (s *Store) ListSchedules(ctx context.Context, scope Scope) ([]model.Schedule, error) {
	all, areas := scope.areaArgs()
	rows, err := s.pool.Query(ctx, scheduleSelect+` WHERE `+targetScope+` ORDER BY x.name`, all, areas)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Schedule, error) { return scanSchedule(r) })
	if err != nil {
		return nil, fmt.Errorf("read schedules: %w", err)
	}
	return orEmpty(items), nil
}

func getSchedule(ctx context.Context, q queryer, id string) (model.Schedule, error) {
	sc, err := scanSchedule(q.QueryRow(ctx, scheduleSelect+` WHERE x.id = $1`, id))
	return sc, mapErr(err, "schedule", "")
}

func (s *Store) GetSchedule(ctx context.Context, id string) (model.Schedule, error) {
	return getSchedule(ctx, s.pool, id)
}

// SaveSchedule creates (id "") or updates a schedule. It runs as the actor saving it.
func (s *Store) SaveSchedule(ctx context.Context, id string, in model.Schedule, nextRunAt *time.Time) (model.Schedule, error) {
	inputs, err := json.Marshal(orEmptyMap(in.Inputs))
	if err != nil {
		return model.Schedule{}, fmt.Errorf("encode inputs: %w", err)
	}
	actor := actorFrom(ctx)
	var out model.Schedule
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		action := "update"
		if id == "" {
			action = "create"
			err := tx.QueryRow(ctx,
				`INSERT INTO schedules (name, description, crew_id, cron, timezone, brief, inputs, budget_usd,
				 enabled, overlap_policy, run_as, run_as_name, next_run_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`,
				in.Name, in.Description, in.CrewID, in.Cron, in.Timezone, in.Brief, inputs, in.BudgetUSD,
				in.Enabled, in.OverlapPolicy, nullableUUID(actor.ID), actor.Name, nextRunAt).Scan(&id)
			if err != nil {
				return mapErr(err, "schedule", in.Name)
			}
		} else {
			tag, err := tx.Exec(ctx,
				`UPDATE schedules SET name = $2, description = $3, crew_id = $4, cron = $5,
				 timezone = $6, brief = $7, inputs = $8, budget_usd = $9, enabled = $10, overlap_policy = $11, run_as = $12,
				 run_as_name = $13, next_run_at = $14, last_error = NULL, updated_at = now() WHERE id = $1`,
				id, in.Name, in.Description, in.CrewID, in.Cron, in.Timezone, in.Brief, inputs,
				in.BudgetUSD, in.Enabled, in.OverlapPolicy, nullableUUID(actor.ID), actor.Name, nextRunAt)
			if err != nil {
				return mapErr(err, "schedule", in.Name)
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		}
		var err error
		if out, err = getSchedule(ctx, tx, id); err != nil {
			return err
		}
		return recordAreaRevision(ctx, tx, "schedule", id, action, out, out.AreaIDs)
	})
	return out, err
}

func (s *Store) DeleteSchedule(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		old, err := getSchedule(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM schedules WHERE id = $1`, id); err != nil {
			return mapErr(err, "schedule", "")
		}
		return recordAreaRevision(ctx, tx, "schedule", id, "delete", old, old.AreaIDs)
	})
}

// DueSchedules returns enabled schedules whose next run time has passed.
func (s *Store) DueSchedules(ctx context.Context, now time.Time, limit int) ([]model.Schedule, error) {
	rows, err := s.pool.Query(ctx, scheduleSelect+` WHERE x.enabled AND x.next_run_at <= $1 ORDER BY x.next_run_at LIMIT $2`,
		now, limit)
	if err != nil {
		return nil, fmt.Errorf("list due schedules: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Schedule, error) { return scanSchedule(r) })
	if err != nil {
		return nil, fmt.Errorf("read due schedules: %w", err)
	}
	return orEmpty(items), nil
}

// ClaimSchedule moves a due schedule to its next time. Only one caller wins, so a schedule fires once even
// with several service processes.
func (s *Store) ClaimSchedule(ctx context.Context, id string, due time.Time, next *time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE schedules SET next_run_at = $3, last_run_at = now() WHERE id = $1 AND enabled AND next_run_at = $2`, id, due, next)
	if err != nil {
		return false, fmt.Errorf("claim schedule: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// RecordScheduleOutcome stores what happened when a schedule fired. disable pauses it (for errors that will
// not fix themselves, like the person it runs as losing access).
func (s *Store) RecordScheduleOutcome(ctx context.Context, id string, workOrderID *string, errMsg *string, disable bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE schedules SET last_work_order_id = COALESCE($2, last_work_order_id), last_error = $3,
		 enabled = enabled AND NOT $4 WHERE id = $1`, id, workOrderID, errMsg, disable)
	if err != nil {
		return fmt.Errorf("record schedule outcome: %w", err)
	}
	return nil
}

// Triggers

const triggerSelect = `SELECT x.id, x.name, x.description, x.crew_id, c.name,
	x.enabled, x.token_prefix, x.input_map, x.brief, x.include_payload, x.budget_usd, x.callback_url, x.signing_secret,
	x.run_as, x.run_as_name, ` + targetAreas + `, x.last_fired_at, x.fire_count, x.created_at, x.updated_at
	FROM triggers x` + targetJoins

func scanTrigger(row pgx.Row) (model.Trigger, error) {
	var t model.Trigger
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.CrewID, &t.TargetName, &t.Enabled,
		&t.TokenPrefix, &t.InputMap, &t.Brief, &t.IncludePayload, &t.BudgetUSD, &t.CallbackURL, &t.SigningSecret,
		&t.RunAs, &t.RunAsName, &t.AreaIDs, &t.LastFiredAt, &t.FireCount, &t.CreatedAt, &t.UpdatedAt)
	if t.InputMap == nil {
		t.InputMap = map[string]string{}
	}
	t.AreaIDs = orEmpty(t.AreaIDs)
	return t, err
}

func (s *Store) ListTriggers(ctx context.Context, scope Scope) ([]model.Trigger, error) {
	all, areas := scope.areaArgs()
	rows, err := s.pool.Query(ctx, triggerSelect+` WHERE `+targetScope+` ORDER BY x.name`, all, areas)
	if err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Trigger, error) { return scanTrigger(r) })
	if err != nil {
		return nil, fmt.Errorf("read triggers: %w", err)
	}
	return orEmpty(items), nil
}

func getTrigger(ctx context.Context, q queryer, id string) (model.Trigger, error) {
	t, err := scanTrigger(q.QueryRow(ctx, triggerSelect+` WHERE x.id = $1`, id))
	return t, mapErr(err, "trigger", "")
}

func (s *Store) GetTrigger(ctx context.Context, id string) (model.Trigger, error) {
	return getTrigger(ctx, s.pool, id)
}

// redactedTrigger is what revisions keep: never the signing secret.
func redactedTrigger(t model.Trigger) model.Trigger {
	t.SigningSecret = ""
	t.Token = ""
	return t
}

// SaveTrigger creates (id "") or updates a trigger. A new trigger gets a token (returned once) and a signing secret.
func (s *Store) SaveTrigger(ctx context.Context, id string, in model.Trigger) (model.Trigger, error) {
	inputMap, err := json.Marshal(in.InputMap)
	if err != nil {
		return model.Trigger{}, fmt.Errorf("encode input map: %w", err)
	}
	actor := actorFrom(ctx)
	var out model.Trigger
	var token string
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		action := "update"
		if id == "" {
			action = "create"
			var prefix string
			token, prefix = auth.NewKey(auth.TriggerPrefix)
			err := tx.QueryRow(ctx,
				`INSERT INTO triggers (name, description, crew_id, enabled, token_prefix, token_hash, input_map,
				 brief, include_payload, budget_usd, callback_url, signing_secret, run_as, run_as_name)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`,
				in.Name, in.Description, in.CrewID, in.Enabled, prefix, auth.HashToken(token), inputMap,
				in.Brief, in.IncludePayload, in.BudgetUSD, in.CallbackURL, auth.RandomToken(32), nullableUUID(actor.ID), actor.Name).
				Scan(&id)
			if err != nil {
				return mapErr(err, "trigger", in.Name)
			}
		} else {
			tag, err := tx.Exec(ctx,
				`UPDATE triggers SET name = $2, description = $3, crew_id = $4, enabled = $5,
				 input_map = $6, brief = $7, include_payload = $8, budget_usd = $9, callback_url = $10, run_as = $11,
				 run_as_name = $12, updated_at = now() WHERE id = $1`,
				id, in.Name, in.Description, in.CrewID, in.Enabled, inputMap, in.Brief,
				in.IncludePayload, in.BudgetUSD, in.CallbackURL, nullableUUID(actor.ID), actor.Name)
			if err != nil {
				return mapErr(err, "trigger", in.Name)
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		}
		var err error
		if out, err = getTrigger(ctx, tx, id); err != nil {
			return err
		}
		return recordAreaRevision(ctx, tx, "trigger", id, action, redactedTrigger(out), out.AreaIDs)
	})
	out.Token = token
	return out, err
}

// RotateTriggerSecrets issues a new token (returned once) and a new signing secret.
func (s *Store) RotateTriggerSecrets(ctx context.Context, id string) (model.Trigger, error) {
	token, prefix := auth.NewKey(auth.TriggerPrefix)
	var out model.Trigger
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE triggers SET token_prefix = $2, token_hash = $3, signing_secret = $4, updated_at = now() WHERE id = $1`,
			id, prefix, auth.HashToken(token), auth.RandomToken(32))
		if err != nil {
			return fmt.Errorf("rotate trigger token: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if out, err = getTrigger(ctx, tx, id); err != nil {
			return err
		}
		return recordAreaRevision(ctx, tx, "trigger", id, "update", redactedTrigger(out), out.AreaIDs)
	})
	out.Token = token
	return out, err
}

func (s *Store) DeleteTrigger(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		old, err := getTrigger(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM triggers WHERE id = $1`, id); err != nil {
			return mapErr(err, "trigger", "")
		}
		return recordAreaRevision(ctx, tx, "trigger", id, "delete", redactedTrigger(old), old.AreaIDs)
	})
}

// TriggerByToken resolves a presented trigger token; unknown or wrong tokens return ErrNotFound.
func (s *Store) TriggerByToken(ctx context.Context, id, token string) (model.Trigger, error) {
	prefix, ok := auth.SplitKey(auth.TriggerPrefix, token)
	if !ok {
		return model.Trigger{}, ErrNotFound
	}
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT token_hash FROM triggers WHERE id = $1 AND token_prefix = $2`, id, prefix).Scan(&hash)
	if err != nil {
		return model.Trigger{}, mapErr(err, "trigger", "")
	}
	if !auth.TokensEqual(hash, auth.HashToken(token)) {
		return model.Trigger{}, ErrNotFound
	}
	return s.GetTrigger(ctx, id)
}

func (s *Store) RecordTriggerFired(ctx context.Context, id string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE triggers SET last_fired_at = now(), fire_count = fire_count + 1 WHERE id = $1`, id); err != nil {
		return fmt.Errorf("record trigger fired: %w", err)
	}
	return nil
}

// TriggerSigningSecret returns the secret that signs a trigger's callbacks.
func (s *Store) TriggerSigningSecret(ctx context.Context, id string) (string, error) {
	var secret string
	err := s.pool.QueryRow(ctx, `SELECT signing_secret FROM triggers WHERE id = $1`, id).Scan(&secret)
	return secret, mapErr(err, "trigger", "")
}

// Callback deliveries

type Delivery struct {
	ID             string     `json:"id"`
	RunID          string     `json:"run_id"`
	URL            string     `json:"url"`
	SecretSource   string     `json:"-"`
	SecretOwnerID  string     `json:"-"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	LastStatusCode *int       `json:"last_status_code"`
	LastError      *string    `json:"last_error"`
	DeliveredAt    *time.Time `json:"delivered_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

const deliveryColumns = `d.id, d.run_id, d.url, d.secret_source, d.secret_owner_id, d.status, d.attempts, d.next_attempt_at,
	d.last_status_code, d.last_error, d.delivered_at, d.created_at`

func scanDelivery(row pgx.Row) (Delivery, error) {
	var d Delivery
	err := row.Scan(&d.ID, &d.RunID, &d.URL, &d.SecretSource, &d.SecretOwnerID, &d.Status, &d.Attempts, &d.NextAttemptAt,
		&d.LastStatusCode, &d.LastError, &d.DeliveredAt, &d.CreatedAt)
	return d, err
}

// ClaimDueDeliveries leases pending deliveries whose run has finished, so no other worker sends them meanwhile.
func (s *Store) ClaimDueDeliveries(ctx context.Context, limit int, lease time.Duration) ([]Delivery, error) {
	rows, err := s.pool.Query(ctx,
		`UPDATE callback_deliveries d SET next_attempt_at = now() + $2::interval
		 WHERE d.id IN (SELECT cd.id FROM callback_deliveries cd JOIN runs r ON r.id = cd.run_id
		                WHERE cd.status = 'pending' AND cd.next_attempt_at <= now()
		                AND r.status IN ('succeeded', 'failed', 'cancelled')
		                ORDER BY cd.next_attempt_at LIMIT $1 FOR UPDATE OF cd SKIP LOCKED)
		 RETURNING `+deliveryColumns, limit, fmt.Sprintf("%d seconds", int(lease.Seconds())))
	if err != nil {
		return nil, fmt.Errorf("claim deliveries: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Delivery, error) { return scanDelivery(r) })
	if err != nil {
		return nil, fmt.Errorf("read deliveries: %w", err)
	}
	return items, nil
}

// RecordDeliveryAttempt stores one attempt. next is when to retry; nil with a failure marks it failed for good.
func (s *Store) RecordDeliveryAttempt(ctx context.Context, id string, delivered bool, statusCode *int, errMsg *string, next *time.Time) error {
	status := "pending"
	switch {
	case delivered:
		status = "delivered"
	case next == nil:
		status = "failed"
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE callback_deliveries SET status = $2, attempts = attempts + 1, last_status_code = $3, last_error = $4,
		 next_attempt_at = COALESCE($5, next_attempt_at), delivered_at = CASE WHEN $6 THEN now() ELSE NULL END WHERE id = $1`,
		id, status, statusCode, errMsg, next, delivered)
	if err != nil {
		return fmt.Errorf("record delivery: %w", err)
	}
	return nil
}

func (s *Store) ListDeliveries(ctx context.Context, runID string) ([]Delivery, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+deliveryColumns+` FROM callback_deliveries d WHERE d.run_id = $1 ORDER BY d.created_at`, runID)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Delivery, error) { return scanDelivery(r) })
	if err != nil {
		return nil, fmt.Errorf("read deliveries: %w", err)
	}
	return orEmpty(items), nil
}
