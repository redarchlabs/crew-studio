-- Work orders (one execution of a task or crew, with a brief, attachments, journal and full output),
-- schedules and webhook triggers that create work orders, run callbacks, and stored files.

-- +goose Up
-- +goose StatementBegin
CREATE SEQUENCE work_order_number_seq;

CREATE TABLE work_orders (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number          bigint NOT NULL UNIQUE DEFAULT nextval('work_order_number_seq'),
    title           text NOT NULL,
    brief           text NOT NULL DEFAULT '',
    target_type     text NOT NULL CHECK (target_type IN ('task', 'crew')),
    task_id         uuid REFERENCES tasks (id) ON DELETE SET NULL,
    crew_id         uuid REFERENCES crews (id) ON DELETE SET NULL,
    target_name     text NOT NULL,
    inputs          jsonb NOT NULL DEFAULT '{}',
    budget_usd      double precision,
    -- Copied from the target when the work order is created, like runs
    area_ids        uuid[] NOT NULL DEFAULT '{}',
    source          text NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'api', 'schedule', 'webhook')),
    source_id       uuid,
    created_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    created_by_name text NOT NULL DEFAULT '',
    -- The latest run; the work order's status is that run's status ('draft' before the first submit)
    current_run_id  uuid REFERENCES runs (id) ON DELETE SET NULL,
    submitted_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX work_orders_created_idx ON work_orders (created_at DESC);
CREATE INDEX work_orders_area_ids_idx ON work_orders USING gin (area_ids);
CREATE INDEX work_orders_source_idx ON work_orders (source, source_id);

-- Where each run came from
ALTER TABLE runs
    ADD COLUMN trigger_type      text NOT NULL DEFAULT 'manual'
        CHECK (trigger_type IN ('manual', 'api', 'schedule', 'webhook', 'work_order')),
    ADD COLUMN work_order_id     uuid REFERENCES work_orders (id) ON DELETE CASCADE,
    -- Repeating a request with the same Idempotency-Key in the same scope returns the first run
    ADD COLUMN idempotency_scope text,
    ADD COLUMN idempotency_key   text;
CREATE INDEX runs_work_order_idx ON runs (work_order_id, created_at DESC);
CREATE UNIQUE INDEX runs_idempotency_idx ON runs (idempotency_scope, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TABLE schedules (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name               text NOT NULL UNIQUE,
    description        text NOT NULL DEFAULT '',
    target_type        text NOT NULL CHECK (target_type IN ('task', 'crew')),
    task_id            uuid REFERENCES tasks (id) ON DELETE RESTRICT,
    crew_id            uuid REFERENCES crews (id) ON DELETE RESTRICT,
    cron               text NOT NULL,
    timezone           text NOT NULL DEFAULT 'UTC',
    brief              text NOT NULL DEFAULT '',
    inputs             jsonb NOT NULL DEFAULT '{}',
    budget_usd         double precision,
    enabled            boolean NOT NULL DEFAULT true,
    -- skip: do not start while the previous work order from this schedule is still queued or running
    overlap_policy     text NOT NULL DEFAULT 'skip' CHECK (overlap_policy IN ('skip', 'allow')),
    -- Work orders run with this person's permissions and areas (whoever last saved the schedule)
    run_as             uuid REFERENCES users (id) ON DELETE SET NULL,
    run_as_name        text NOT NULL DEFAULT '',
    next_run_at        timestamptz,
    last_run_at        timestamptz,
    last_work_order_id uuid REFERENCES work_orders (id) ON DELETE SET NULL,
    last_error         text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CHECK ((target_type = 'task') = (task_id IS NOT NULL) AND (target_type = 'crew') = (crew_id IS NOT NULL))
);
CREATE INDEX schedules_due_idx ON schedules (next_run_at) WHERE enabled;

CREATE TABLE triggers (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text NOT NULL UNIQUE,
    description    text NOT NULL DEFAULT '',
    target_type    text NOT NULL CHECK (target_type IN ('task', 'crew')),
    task_id        uuid REFERENCES tasks (id) ON DELETE RESTRICT,
    crew_id        uuid REFERENCES crews (id) ON DELETE RESTRICT,
    enabled        boolean NOT NULL DEFAULT true,
    -- The caller presents the token; only its SHA-256 is kept
    token_prefix   text NOT NULL UNIQUE,
    token_hash     text NOT NULL,
    -- Run input key -> dotted path into the JSON payload, e.g. {"company": "customer.name"}
    input_map      jsonb NOT NULL DEFAULT '{}',
    brief          text NOT NULL DEFAULT '',
    include_payload boolean NOT NULL DEFAULT true,
    budget_usd     double precision,
    callback_url   text,
    -- Signs callbacks (HMAC-SHA256); shown to people who can manage the trigger
    signing_secret text NOT NULL,
    run_as         uuid REFERENCES users (id) ON DELETE SET NULL,
    run_as_name    text NOT NULL DEFAULT '',
    last_fired_at  timestamptz,
    fire_count     integer NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CHECK ((target_type = 'task') = (task_id IS NOT NULL) AND (target_type = 'crew') = (crew_id IS NOT NULL))
);

-- A POST to the caller's callback URL when a run finishes, retried with backoff
CREATE TABLE callback_deliveries (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id           uuid NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    url              text NOT NULL,
    secret_source    text NOT NULL CHECK (secret_source IN ('trigger', 'service_account')),
    secret_owner_id  uuid NOT NULL,
    status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts         integer NOT NULL DEFAULT 0,
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),
    last_status_code integer,
    last_error       text,
    delivered_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX callback_deliveries_due_idx ON callback_deliveries (next_attempt_at) WHERE status = 'pending';
CREATE INDEX callback_deliveries_run_idx ON callback_deliveries (run_id);

-- Files in object storage: inputs people attach to work orders, files agents write during a run,
-- and each run's complete raw output
CREATE TABLE attachments (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    work_order_id   uuid REFERENCES work_orders (id) ON DELETE CASCADE,
    run_id          uuid REFERENCES runs (id) ON DELETE CASCADE,
    kind            text NOT NULL CHECK (kind IN ('input', 'artifact', 'transcript')),
    filename        text NOT NULL,
    content_type    text NOT NULL,
    size_bytes      bigint NOT NULL,
    sha256          text NOT NULL,
    storage_key     text NOT NULL UNIQUE,
    created_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    created_by_name text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (work_order_id IS NOT NULL OR run_id IS NOT NULL),
    CHECK ((kind = 'input') = (run_id IS NULL))
);
CREATE INDEX attachments_work_order_idx ON attachments (work_order_id, created_at);
CREATE INDEX attachments_run_idx ON attachments (run_id, created_at);

-- When an attachment row goes (directly or by cascade), its stored object is queued for deletion
CREATE TABLE storage_deletions (
    storage_key text PRIMARY KEY,
    attempts    integer NOT NULL DEFAULT 0,
    last_error  text,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION queue_storage_deletion() RETURNS trigger AS $$
BEGIN
    INSERT INTO storage_deletions (storage_key) VALUES (OLD.storage_key) ON CONFLICT DO NOTHING;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER attachments_queue_deletion AFTER DELETE ON attachments
    FOR EACH ROW EXECUTE FUNCTION queue_storage_deletion();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drops work orders, schedules, triggers, callbacks and the file records. Files already in object storage are not
-- deleted; remove the containers yourself if you need the space back.
DROP TRIGGER attachments_queue_deletion ON attachments;
DROP FUNCTION queue_storage_deletion();
DROP TABLE storage_deletions, attachments, callback_deliveries, triggers, schedules;
ALTER TABLE runs
    DROP COLUMN idempotency_key,
    DROP COLUMN idempotency_scope,
    DROP COLUMN work_order_id,
    DROP COLUMN trigger_type;
DROP TABLE work_orders;
DROP SEQUENCE work_order_number_seq;
-- +goose StatementEnd
