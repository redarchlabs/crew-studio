-- Crew state, phase 2 (docs/crew-state-and-pmo-design.md, section 5): watches. A watch is attached to a schedule
-- (at most one each). On every tick Studio fetches the watch's source, compares it with the stored snapshot and
-- works out which follow-up steps are due; when there is nothing to do no work order is created. Managing a watch
-- uses the existing schedules.manage permission, so no permissions are added.

-- +goose Up
-- +goose StatementBegin
CREATE TABLE watches (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id     uuid NOT NULL UNIQUE REFERENCES schedules (id) ON DELETE CASCADE,
    -- The connection a source reads through (http_json); NULL for sources that need none
    connection_id   uuid REFERENCES connections (id) ON DELETE RESTRICT,
    -- Source kind plus its query, such as {"kind": "http_json", "path": "/issues", "items_path": "data.items"}
    source          jsonb NOT NULL,
    id_field        text NOT NULL,
    tracked_fields  text[] NOT NULL DEFAULT '{}',
    run_when        text NOT NULL DEFAULT 'changed'
                    CHECK (run_when IN ('changed', 'always', 'changed_or_followups')),
    -- Follow-up ladder (section 6); NULL when there is none
    followups       jsonb,
    dry_run         boolean NOT NULL DEFAULT true,
    -- Past this many items a check fails instead of silently cutting the list
    max_items       integer NOT NULL DEFAULT 2000 CHECK (max_items BETWEEN 1 AND 10000),
    last_checked_at timestamptz,
    last_changed_at timestamptz,
    last_error      text,
    -- When the stored snapshot was last confirmed; NULL means the next check is the first (everything is added)
    snapshot_at     timestamptz,
    created_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX watches_connection_idx ON watches (connection_id) WHERE connection_id IS NOT NULL;

-- A cache: only the tracked fields of each item. Safe to delete; the next check rebuilds it.
CREATE TABLE watch_snapshots (
    watch_id uuid NOT NULL REFERENCES watches (id) ON DELETE CASCADE,
    item_id  text NOT NULL,
    fields   jsonb NOT NULL,
    seen_at  timestamptz NOT NULL,
    PRIMARY KEY (watch_id, item_id)
);

-- One row per check, so skipped ticks show in the schedule's history and savings can be counted.
CREATE TABLE watch_checks (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    watch_id      uuid NOT NULL REFERENCES watches (id) ON DELETE CASCADE,
    checked_at    timestamptz NOT NULL DEFAULT now(),
    -- started: a work order was created; no_changes: nothing to do, no run; busy: the previous work order was still
    -- running (overlap "skip"); error: the source could not be read or the work order could not be created
    outcome       text NOT NULL CHECK (outcome IN ('started', 'no_changes', 'busy', 'error')),
    added         integer NOT NULL DEFAULT 0,
    changed       integer NOT NULL DEFAULT 0,
    removed       integer NOT NULL DEFAULT 0,
    unchanged     integer NOT NULL DEFAULT 0,
    followups_due integer NOT NULL DEFAULT 0,
    work_order_id uuid REFERENCES work_orders (id) ON DELETE SET NULL,
    message       text NOT NULL DEFAULT ''
);
CREATE INDEX watch_checks_recent_idx ON watch_checks (watch_id, checked_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deletes every watch, its snapshot and its check history.
DROP TABLE watch_checks, watch_snapshots, watches;
-- +goose StatementEnd
