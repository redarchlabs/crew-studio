-- Crew Studio schema: configuration (LLMs, prices, agents, tasks, crews, settings),
-- run history (runs, events, LLM calls, task outputs) and an edit history (revisions).

-- +goose Up
-- +goose StatementBegin
CREATE TABLE settings (
    id                             boolean PRIMARY KEY DEFAULT true CHECK (id),
    default_budget_usd             double precision NOT NULL DEFAULT 2.0,
    hard_stop_multiplier           double precision NOT NULL DEFAULT 1.25,
    search_model                   text NOT NULL DEFAULT 'gemini-2.5-flash',
    grounded_search_cost_per_1000  double precision NOT NULL DEFAULT 35.0,
    grounded_searches_free_per_day integer NOT NULL DEFAULT 1500,
    max_concurrent_runs            integer NOT NULL DEFAULT 2,
    updated_at                     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE model_prices (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    model                 text NOT NULL UNIQUE,
    input_per_mtok        double precision NOT NULL,
    cached_input_per_mtok double precision NOT NULL,
    output_per_mtok       double precision NOT NULL,
    notes                 text NOT NULL DEFAULT '',
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE llms (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                  text NOT NULL UNIQUE,
    model                 text NOT NULL,
    temperature           double precision,
    max_completion_tokens integer,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE agents (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name             text NOT NULL UNIQUE,
    role             text NOT NULL,
    goal             text NOT NULL,
    backstory        text NOT NULL,
    llm_id           uuid REFERENCES llms (id) ON DELETE RESTRICT,
    tools            text[] NOT NULL DEFAULT '{}',
    max_iter         integer NOT NULL DEFAULT 25,
    allow_delegation boolean NOT NULL DEFAULT false,
    verbose_output   boolean NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tasks (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name            text NOT NULL UNIQUE,
    description     text NOT NULL,
    expected_output text NOT NULL,
    agent_id        uuid REFERENCES agents (id) ON DELETE RESTRICT,
    markdown        boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE task_context (
    task_id         uuid NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    context_task_id uuid NOT NULL REFERENCES tasks (id) ON DELETE RESTRICT,
    position        integer NOT NULL,
    PRIMARY KEY (task_id, context_task_id),
    CHECK (task_id <> context_task_id)
);

CREATE TABLE crews (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text NOT NULL UNIQUE,
    description    text NOT NULL DEFAULT '',
    process        text NOT NULL DEFAULT 'sequential' CHECK (process IN ('sequential', 'hierarchical')),
    manager_llm_id uuid REFERENCES llms (id) ON DELETE RESTRICT,
    inputs         jsonb NOT NULL DEFAULT '[]',
    budget_usd     double precision NOT NULL DEFAULT 2.0,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE crew_agents (
    crew_id  uuid NOT NULL REFERENCES crews (id) ON DELETE CASCADE,
    agent_id uuid NOT NULL REFERENCES agents (id) ON DELETE RESTRICT,
    position integer NOT NULL,
    PRIMARY KEY (crew_id, agent_id)
);

CREATE TABLE crew_tasks (
    crew_id  uuid NOT NULL REFERENCES crews (id) ON DELETE CASCADE,
    task_id  uuid NOT NULL REFERENCES tasks (id) ON DELETE RESTRICT,
    position integer NOT NULL,
    PRIMARY KEY (crew_id, task_id)
);

CREATE TABLE runs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    crew_id     uuid REFERENCES crews (id) ON DELETE SET NULL,
    crew_name   text NOT NULL,
    status      text NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    inputs      jsonb NOT NULL DEFAULT '{}',
    budget_usd  double precision NOT NULL,
    spec        jsonb NOT NULL,
    output      text,
    error       text,
    cost_usd    double precision,
    usage       jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    started_at  timestamptz,
    finished_at timestamptz
);
CREATE INDEX runs_created_at_idx ON runs (created_at DESC);
CREATE INDEX runs_crew_id_idx ON runs (crew_id, created_at DESC);
CREATE INDEX runs_status_idx ON runs (status) WHERE status IN ('queued', 'running');

CREATE TABLE run_events (
    run_id     uuid NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    seq        integer NOT NULL,
    type       text NOT NULL,
    payload    jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, seq)
);
CREATE INDEX run_events_type_idx ON run_events (type, created_at);

-- Full prompts and responses live here so the event stream stays small
CREATE TABLE llm_calls (
    run_id               uuid NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    seq                  integer NOT NULL,
    agent_role           text NOT NULL DEFAULT '',
    task_name            text NOT NULL DEFAULT '',
    model                text NOT NULL DEFAULT '',
    prompt_tokens        integer NOT NULL DEFAULT 0,
    completion_tokens    integer NOT NULL DEFAULT 0,
    cached_prompt_tokens integer NOT NULL DEFAULT 0,
    cost_usd             double precision NOT NULL DEFAULT 0,
    messages             jsonb NOT NULL DEFAULT '[]',
    response             text NOT NULL DEFAULT '',
    created_at           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, seq)
);

CREATE TABLE run_task_outputs (
    run_id     uuid NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    position   integer NOT NULL,
    task_name  text NOT NULL,
    agent_role text NOT NULL DEFAULT '',
    raw        text NOT NULL,
    PRIMARY KEY (run_id, position)
);

CREATE TABLE revisions (
    id          bigserial PRIMARY KEY,
    entity_type text NOT NULL CHECK (entity_type IN ('llm', 'pricing', 'agent', 'task', 'crew', 'settings')),
    entity_id   text NOT NULL,
    action      text NOT NULL CHECK (action IN ('create', 'update', 'delete')),
    snapshot    jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX revisions_entity_idx ON revisions (entity_type, entity_id, id DESC);

INSERT INTO settings DEFAULT VALUES;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drops every table this migration created, with all configuration and run history in them.
DROP TABLE revisions, run_task_outputs, llm_calls, run_events, runs, crew_tasks, crew_agents, crews,
    task_context, tasks, agents, llms, model_prices, settings;
-- +goose StatementEnd
