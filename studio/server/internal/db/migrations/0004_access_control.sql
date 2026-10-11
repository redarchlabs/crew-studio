-- Access control: users (people and service accounts), roles built from a fixed permission catalog,
-- functional areas that crews, agents and tasks belong to, sign-in sessions, SSO identities and API keys.

-- +goose Up
-- +goose StatementBegin
CREATE TABLE functional_areas (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- The catalog is fixed by the application (internal/auth/permissions.go); a test keeps the two in step.
CREATE TABLE permissions (
    key         text PRIMARY KEY,
    feature     text NOT NULL,
    label       text NOT NULL,
    description text NOT NULL,
    position    integer NOT NULL
);

CREATE TABLE roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    -- The Administrator role always holds every permission and cannot be edited or deleted
    builtin     boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE role_permissions (
    role_id    uuid NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission text NOT NULL REFERENCES permissions (key) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission)
);

CREATE TABLE users (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind           text NOT NULL DEFAULT 'human' CHECK (kind IN ('human', 'service')),
    email          text,
    name           text NOT NULL,
    status         text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    all_areas      boolean NOT NULL DEFAULT false,
    -- Local sign-in is for administrators only; NULL means the account signs in through SSO
    password_hash  text,
    failed_logins  integer NOT NULL DEFAULT 0,
    locked_until   timestamptz,
    -- Service accounts sign the run callbacks they request with this secret
    signing_secret text,
    last_login_at  timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CHECK (kind = 'service' OR email IS NOT NULL),
    CHECK (kind = 'human' OR (password_hash IS NULL AND email IS NULL))
);
CREATE UNIQUE INDEX users_email_idx ON users (lower(email)) WHERE email IS NOT NULL;
CREATE UNIQUE INDEX users_service_name_idx ON users (lower(name)) WHERE kind = 'service';

CREATE TABLE user_roles (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id uuid NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE user_permissions (
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    permission text NOT NULL REFERENCES permissions (key) ON DELETE CASCADE,
    PRIMARY KEY (user_id, permission)
);

CREATE TABLE user_areas (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    area_id uuid NOT NULL REFERENCES functional_areas (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, area_id)
);

-- One row per SSO account linked to a user (Clerk or Microsoft subject)
CREATE TABLE user_identities (
    provider      text NOT NULL,
    subject       text NOT NULL,
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email         text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject)
);
CREATE INDEX user_identities_user_idx ON user_identities (user_id);

-- Only a SHA-256 of each session token is stored
CREATE TABLE sessions (
    token_hash   text PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider     text NOT NULL,
    ip           text NOT NULL DEFAULT '',
    user_agent   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- An SSO sign-in in progress: single use, a few minutes long
CREATE TABLE login_states (
    state_hash    text PRIMARY KEY,
    provider      text NOT NULL,
    nonce         text NOT NULL,
    code_verifier text NOT NULL,
    return_to     text NOT NULL DEFAULT '/',
    expires_at    timestamptz NOT NULL
);

-- API keys belong to service accounts. The key is shown once; only its SHA-256 is kept.
CREATE TABLE api_keys (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text NOT NULL,
    prefix       text NOT NULL UNIQUE,
    secret_hash  text NOT NULL,
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz
);
CREATE INDEX api_keys_user_idx ON api_keys (user_id);

CREATE TABLE access_settings (
    id                     boolean PRIMARY KEY DEFAULT true CHECK (id),
    -- When on, someone who signs in through SSO without an account gets one (with the role below, no areas)
    sso_auto_provision     boolean NOT NULL DEFAULT false,
    auto_provision_role_id uuid REFERENCES roles (id) ON DELETE SET NULL,
    -- Run callbacks may go to private addresses only on these hosts
    callback_allowed_hosts text[] NOT NULL DEFAULT '{}',
    updated_at             timestamptz NOT NULL DEFAULT now()
);
INSERT INTO access_settings DEFAULT VALUES;

-- Sign-ins, sign-outs and failed attempts
CREATE TABLE audit_events (
    id         bigserial PRIMARY KEY,
    action     text NOT NULL,
    actor_id   uuid,
    actor_name text NOT NULL DEFAULT '',
    ip         text NOT NULL DEFAULT '',
    details    jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_created_idx ON audit_events (created_at DESC);

-- Crews, agents and tasks belong to one or more functional areas
CREATE TABLE agent_areas (
    agent_id uuid NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    area_id  uuid NOT NULL REFERENCES functional_areas (id) ON DELETE RESTRICT,
    PRIMARY KEY (agent_id, area_id)
);
CREATE INDEX agent_areas_area_idx ON agent_areas (area_id);

CREATE TABLE task_areas (
    task_id uuid NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    area_id uuid NOT NULL REFERENCES functional_areas (id) ON DELETE RESTRICT,
    PRIMARY KEY (task_id, area_id)
);
CREATE INDEX task_areas_area_idx ON task_areas (area_id);

CREATE TABLE crew_areas (
    crew_id uuid NOT NULL REFERENCES crews (id) ON DELETE CASCADE,
    area_id uuid NOT NULL REFERENCES functional_areas (id) ON DELETE RESTRICT,
    PRIMARY KEY (crew_id, area_id)
);
CREATE INDEX crew_areas_area_idx ON crew_areas (area_id);

-- Runs keep a copy of their crew's areas, so access to history does not change when the crew does
ALTER TABLE runs
    ADD COLUMN area_ids        uuid[] NOT NULL DEFAULT '{}',
    ADD COLUMN created_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN created_by_name text NOT NULL DEFAULT '';
CREATE INDEX runs_area_ids_idx ON runs USING gin (area_ids);

-- Every revision records who made it and the areas the item was in
ALTER TABLE revisions DROP CONSTRAINT revisions_entity_type_check;
ALTER TABLE revisions
    ADD CONSTRAINT revisions_entity_type_check CHECK (entity_type IN (
        'llm', 'pricing', 'agent', 'task', 'crew', 'settings',
        'area', 'role', 'user', 'api_key', 'access_settings', 'schedule', 'trigger', 'work_order')),
    ADD COLUMN actor_id   uuid,
    ADD COLUMN actor_name text NOT NULL DEFAULT '',
    ADD COLUMN area_ids   uuid[] NOT NULL DEFAULT '{}';
CREATE INDEX revisions_created_idx ON revisions (id DESC);

-- Permission catalog
INSERT INTO permissions (key, feature, label, description, position) VALUES
    ('overview.view',   'Overview',  'View overview',      'See the dashboard (counts and recent runs in your areas).', 1),
    ('crews.view',      'Crews',     'View crews',         'See crews in your areas and whether they can run.', 10),
    ('crews.create',    'Crews',     'Create crews',       'Create crews in your areas.', 11),
    ('crews.edit',      'Crews',     'Edit crews',         'Change crews in your areas.', 12),
    ('crews.delete',    'Crews',     'Delete crews',       'Delete crews in your areas.', 13),
    ('crews.run',       'Crews',     'Run crews',          'Start runs of crews in your areas. Runs spend money.', 14),
    ('agents.view',     'Agents',    'View agents',        'See agents in your areas.', 20),
    ('agents.create',   'Agents',    'Create agents',      'Create agents in your areas.', 21),
    ('agents.edit',     'Agents',    'Edit agents',        'Change agents in your areas.', 22),
    ('agents.delete',   'Agents',    'Delete agents',      'Delete agents in your areas.', 23),
    ('tasks.view',      'Tasks',     'View tasks',         'See tasks in your areas.', 30),
    ('tasks.create',    'Tasks',     'Create tasks',       'Create tasks in your areas.', 31),
    ('tasks.edit',      'Tasks',     'Edit tasks',         'Change tasks in your areas.', 32),
    ('tasks.delete',    'Tasks',     'Delete tasks',       'Delete tasks in your areas.', 33),
    ('work_orders.view',   'Work orders', 'View work orders',   'See work orders for tasks and crews in your areas, with their journal, files and full output.', 35),
    ('work_orders.create', 'Work orders', 'Create work orders', 'Create and submit work orders for tasks and crews in your areas (this runs them and spends money; it does not need Run crews) and attach files.', 36),
    ('work_orders.cancel', 'Work orders', 'Cancel work orders', 'Stop work orders in your areas that are queued or running.', 37),
    ('work_orders.delete', 'Work orders', 'Delete work orders', 'Delete finished work orders in your areas, with their files.', 38),
    ('runs.view',       'Runs',      'View runs',          'See runs of crews in your areas, with every prompt, response and cost.', 40),
    ('runs.cancel',     'Runs',      'Cancel runs',        'Stop queued or running runs in your areas.', 41),
    ('runs.delete',     'Runs',      'Delete runs',        'Delete finished runs in your areas.', 42),
    ('schedules.view',  'Schedules', 'View schedules',     'See schedules for crews in your areas.', 50),
    ('schedules.manage','Schedules', 'Manage schedules',   'Create, change, pause and delete schedules (also needs Create work orders, since schedules start work orders).', 51),
    ('triggers.view',   'Triggers',  'View triggers',      'See webhook triggers for crews in your areas.', 60),
    ('triggers.manage', 'Triggers',  'Manage triggers',    'Create, change, rotate and delete webhook triggers (also needs Create work orders).', 61),
    ('llms.view',       'LLMs',      'View LLMs',          'See LLM configurations.', 70),
    ('llms.manage',     'LLMs',      'Manage LLMs',        'Create, change and delete LLM configurations.', 71),
    ('pricing.view',    'Pricing',   'View pricing',       'See model prices.', 80),
    ('pricing.manage',  'Pricing',   'Manage pricing',     'Create, change and delete model prices.', 81),
    ('settings.view',   'Settings',  'View settings',      'See budget, search and concurrency settings.', 90),
    ('settings.manage', 'Settings',  'Manage settings',    'Change budget, search and concurrency settings.', 91),
    ('history.view',    'History',   'View history',       'See the edit history of items in your areas and of LLMs, prices and settings.', 100),
    ('admin.users',     'Admin',     'Manage users',       'Invite, change and disable people, and set administrator passwords.', 200),
    ('admin.service_accounts', 'Admin', 'Manage service accounts', 'Create service accounts and issue or revoke their API keys.', 201),
    ('admin.roles',     'Admin',     'Manage roles',       'Create and change roles and the permissions they grant.', 202),
    ('admin.areas',     'Admin',     'Manage functional areas', 'Create, rename and delete functional areas.', 203),
    ('admin.access',    'Admin',     'Manage sign-in settings', 'Change SSO auto-provisioning and the callback host allow-list.', 204),
    ('admin.audit',     'Admin',     'View audit log',     'See sign-ins and every change to users, roles, areas and keys.', 205);

-- Functional areas and built-in roles (fixed ids so later migrations and docs can refer to them)
INSERT INTO functional_areas (id, name, description) VALUES
    ('55555555-0000-4000-8000-000000000001', 'Finance',  'Finance, investment and due diligence work.'),
    ('55555555-0000-4000-8000-000000000002', 'IT',       'Technology research and IT operations.'),
    ('55555555-0000-4000-8000-000000000003', 'Training', 'Learning and training content.'),
    ('55555555-0000-4000-8000-000000000004', 'HR',       'People, hiring and HR operations.');

INSERT INTO roles (id, name, description, builtin) VALUES
    ('66666666-0000-4000-8000-000000000001', 'Administrator', 'Every permission. Pair with "all areas" for full access.', true),
    ('66666666-0000-4000-8000-000000000002', 'Builder',  'Builds and runs crews, agents, tasks, schedules in their areas.', false),
    ('66666666-0000-4000-8000-000000000003', 'Operator', 'Runs crews and watches runs in their areas; cannot change them.', false),
    ('66666666-0000-4000-8000-000000000004', 'Viewer',   'Read-only access to their areas.', false);

INSERT INTO role_permissions (role_id, permission)
SELECT '66666666-0000-4000-8000-000000000001', key FROM permissions;

INSERT INTO role_permissions (role_id, permission)
SELECT '66666666-0000-4000-8000-000000000002', unnest(ARRAY[
    'overview.view', 'crews.view', 'crews.create', 'crews.edit', 'crews.delete', 'crews.run',
    'agents.view', 'agents.create', 'agents.edit', 'agents.delete',
    'tasks.view', 'tasks.create', 'tasks.edit', 'tasks.delete',
    'work_orders.view', 'work_orders.create', 'work_orders.cancel',
    'runs.view', 'runs.cancel', 'schedules.view', 'schedules.manage', 'triggers.view', 'triggers.manage',
    'llms.view', 'pricing.view', 'settings.view', 'history.view']);

INSERT INTO role_permissions (role_id, permission)
SELECT '66666666-0000-4000-8000-000000000003', unnest(ARRAY[
    'overview.view', 'crews.view', 'crews.run', 'agents.view', 'tasks.view', 'runs.view', 'runs.cancel',
    'work_orders.view', 'work_orders.create', 'work_orders.cancel',
    'schedules.view', 'triggers.view', 'llms.view', 'pricing.view']);

INSERT INTO role_permissions (role_id, permission)
SELECT '66666666-0000-4000-8000-000000000004', unnest(ARRAY[
    'overview.view', 'crews.view', 'agents.view', 'tasks.view', 'runs.view', 'schedules.view', 'triggers.view',
    'work_orders.view', 'llms.view', 'pricing.view', 'history.view']);

-- Place the imported crews: PE due diligence is Finance work, AI research is IT
INSERT INTO crew_areas (crew_id, area_id)
SELECT id, '55555555-0000-4000-8000-000000000001'::uuid FROM crews WHERE id = '44444444-0000-4000-8000-000000000001'
UNION ALL
SELECT id, '55555555-0000-4000-8000-000000000002'::uuid FROM crews WHERE id = '44444444-0000-4000-8000-000000000002';

INSERT INTO agent_areas (agent_id, area_id)
SELECT id, CASE WHEN id = '22222222-0000-4000-8000-000000000006'
               THEN '55555555-0000-4000-8000-000000000002'::uuid ELSE '55555555-0000-4000-8000-000000000001'::uuid END
FROM agents WHERE id::text LIKE '22222222-0000-4000-8000-%';

INSERT INTO task_areas (task_id, area_id)
SELECT id, CASE WHEN id IN ('33333333-0000-4000-8000-000000000006', '33333333-0000-4000-8000-000000000007')
               THEN '55555555-0000-4000-8000-000000000002'::uuid ELSE '55555555-0000-4000-8000-000000000001'::uuid END
FROM tasks WHERE id::text LIKE '33333333-0000-4000-8000-%';

-- Anything created before areas existed goes into a General area so every item has one
INSERT INTO functional_areas (id, name, description)
SELECT '55555555-0000-4000-8000-000000000005', 'General', 'Items created before functional areas existed.'
WHERE EXISTS (SELECT 1 FROM agents a WHERE NOT EXISTS (SELECT 1 FROM agent_areas x WHERE x.agent_id = a.id))
   OR EXISTS (SELECT 1 FROM tasks t WHERE NOT EXISTS (SELECT 1 FROM task_areas x WHERE x.task_id = t.id))
   OR EXISTS (SELECT 1 FROM crews c WHERE NOT EXISTS (SELECT 1 FROM crew_areas x WHERE x.crew_id = c.id));
INSERT INTO agent_areas (agent_id, area_id)
SELECT a.id, '55555555-0000-4000-8000-000000000005'::uuid FROM agents a
WHERE NOT EXISTS (SELECT 1 FROM agent_areas x WHERE x.agent_id = a.id);
INSERT INTO task_areas (task_id, area_id)
SELECT t.id, '55555555-0000-4000-8000-000000000005'::uuid FROM tasks t
WHERE NOT EXISTS (SELECT 1 FROM task_areas x WHERE x.task_id = t.id);
INSERT INTO crew_areas (crew_id, area_id)
SELECT c.id, '55555555-0000-4000-8000-000000000005'::uuid FROM crews c
WHERE NOT EXISTS (SELECT 1 FROM crew_areas x WHERE x.crew_id = c.id);

UPDATE runs r SET area_ids = COALESCE((SELECT array_agg(area_id) FROM crew_areas ca WHERE ca.crew_id = r.crew_id), '{}');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drops people, roles, areas, sessions, API keys and the audit log. Edit history of the kinds that only exist
-- from here on (areas, users, roles, ...) is deleted so the original entity_type check can come back.
DELETE FROM revisions WHERE entity_type NOT IN ('llm', 'pricing', 'agent', 'task', 'crew', 'settings');
DROP INDEX revisions_created_idx;
ALTER TABLE revisions
    DROP CONSTRAINT revisions_entity_type_check,
    DROP COLUMN area_ids,
    DROP COLUMN actor_name,
    DROP COLUMN actor_id,
    ADD CONSTRAINT revisions_entity_type_check CHECK (entity_type IN ('llm', 'pricing', 'agent', 'task', 'crew', 'settings'));
ALTER TABLE runs DROP COLUMN created_by_name, DROP COLUMN created_by, DROP COLUMN area_ids;
DROP TABLE crew_areas, task_areas, agent_areas, audit_events, access_settings, api_keys, login_states, sessions,
    user_identities, user_areas, user_permissions, user_roles, users, role_permissions, roles, permissions,
    functional_areas;
-- +goose StatementEnd
