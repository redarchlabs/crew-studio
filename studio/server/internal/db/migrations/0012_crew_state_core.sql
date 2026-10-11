-- Crew state, phase 1 (docs/crew-state-and-pmo-design.md): connections to outside systems (secrets encrypted with
-- STUDIO_SECRETS_KEY), which connections each crew may use, per-run tokens for the internal run-state API, the
-- action log and versioned notes. Adds the connections.* and state.* permissions.

-- +goose Up
-- +goose StatementBegin
CREATE TABLE connections (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name              text NOT NULL UNIQUE,
    kind              text NOT NULL CHECK (kind IN ('jira', 'linear', 'km2', 'http', 'slack', 'teams', 'email')),
    -- Non-secret settings such as a site URL or from-address
    config            jsonb NOT NULL DEFAULT '{}',
    -- AES-256-GCM of the secret fields as JSON, bound to the connection id; NULL when none are set
    secret_cipher     bytea,
    -- Names of the secret fields that are set (never their values), so lists need no decryption
    secret_keys       text[] NOT NULL DEFAULT '{}',
    secret_updated_at timestamptz,
    area_ids          uuid[] NOT NULL DEFAULT '{}',
    enabled           boolean NOT NULL DEFAULT true,
    created_by        uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX connections_area_ids_idx ON connections USING gin (area_ids);

-- Which connections a crew's tools may use; a run also needs an area in common with the connection
CREATE TABLE crew_connections (
    crew_id       uuid NOT NULL REFERENCES crews (id) ON DELETE CASCADE,
    connection_id uuid NOT NULL REFERENCES connections (id) ON DELETE CASCADE,
    PRIMARY KEY (crew_id, connection_id)
);
CREATE INDEX crew_connections_connection_idx ON crew_connections (connection_id);

-- One token per run for /api/run-state; only its SHA-256 is kept
CREATE TABLE run_tokens (
    run_id     uuid PRIMARY KEY REFERENCES runs (id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL
);
CREATE INDEX run_tokens_expires_idx ON run_tokens (expires_at);

-- The crew's exact record of what it did. Keep and back up.
CREATE TABLE action_log (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    crew_id         uuid NOT NULL REFERENCES crews (id) ON DELETE CASCADE,
    schedule_id     uuid REFERENCES schedules (id) ON DELETE SET NULL,
    subject         text NOT NULL,
    action          text NOT NULL,
    run_id          uuid REFERENCES runs (id) ON DELETE SET NULL,
    dry_run         boolean NOT NULL DEFAULT false,
    details         jsonb NOT NULL DEFAULT '{}',
    idempotency_key text,
    at              timestamptz NOT NULL DEFAULT now(),
    UNIQUE (crew_id, idempotency_key)
);
CREATE INDEX action_log_lookup ON action_log (crew_id, schedule_id, subject, action, at DESC);
CREATE INDEX action_log_recent ON action_log (crew_id, at DESC);
-- Counts a run's writes against its quota
CREATE INDEX action_log_run_idx ON action_log (run_id) WHERE run_id IS NOT NULL;

-- Versioned working memory. Every write is a new row; the highest version is the note.
CREATE TABLE notes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    crew_id     uuid NOT NULL REFERENCES crews (id) ON DELETE CASCADE,
    schedule_id uuid REFERENCES schedules (id) ON DELETE CASCADE,
    subject     text NOT NULL DEFAULT '',
    name        text NOT NULL,
    version     integer NOT NULL CHECK (version > 0),
    content     text NOT NULL,
    run_id      uuid REFERENCES runs (id) ON DELETE SET NULL,
    -- 'run' or the name of the person who edited it
    written_by  text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- Crew-wide notes have no schedule; NULLS NOT DISTINCT keeps their versions unique too
    UNIQUE NULLS NOT DISTINCT (crew_id, schedule_id, subject, name, version)
);
CREATE INDEX notes_run_idx ON notes (run_id) WHERE run_id IS NOT NULL;

INSERT INTO permissions (key, feature, label, description, position) VALUES
    ('state.view',   'Crew state', 'View crew state',
     'See the action log and notes (with their versions) of crews in your areas.', 16),
    ('state.manage', 'Crew state', 'Manage crew state',
     'Edit, roll back and delete notes, and delete action log entries, for crews in your areas.', 17),
    ('connections.view',   'Connections', 'View connections',
     'See connections to outside systems in your areas (never their secrets), and which crews may use them.', 95),
    ('connections.manage', 'Connections', 'Manage connections',
     'Create, change, test, disable and delete connections in your areas, and replace their secrets.', 96);

-- Administrators hold everything; Builders see connections and look after their crews' state; Operators and
-- Viewers can read state. Roles people have since deleted are skipped.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
FROM roles r
JOIN (VALUES
    ('66666666-0000-4000-8000-000000000001'::uuid, 'state.view'),
    ('66666666-0000-4000-8000-000000000001'::uuid, 'state.manage'),
    ('66666666-0000-4000-8000-000000000001'::uuid, 'connections.view'),
    ('66666666-0000-4000-8000-000000000001'::uuid, 'connections.manage'),
    ('66666666-0000-4000-8000-000000000002'::uuid, 'state.view'),
    ('66666666-0000-4000-8000-000000000002'::uuid, 'state.manage'),
    ('66666666-0000-4000-8000-000000000002'::uuid, 'connections.view'),
    ('66666666-0000-4000-8000-000000000003'::uuid, 'state.view'),
    ('66666666-0000-4000-8000-000000000004'::uuid, 'state.view')
) AS p (role_id, permission) ON p.role_id = r.id
ON CONFLICT DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deletes every connection (with its encrypted secrets), action log entry and note.
DELETE FROM permissions WHERE key IN ('state.view', 'state.manage', 'connections.view', 'connections.manage');
DROP TABLE notes, action_log, run_tokens, crew_connections, connections;
-- +goose StatementEnd
