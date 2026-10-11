-- Human input: a crew's escalation rules say when its agents should stop and ask a person. An agent asks with the
-- "Ask a human" tool; the question waits here until the person who started the run answers it or it times out,
-- while the run keeps its process and waits (no new run status: an open question is what "waiting for you" means).

-- +goose Up
-- +goose StatementBegin
-- Escalation settings of a crew; no row means escalation is off with the defaults below
CREATE TABLE crew_escalation (
    crew_id         uuid PRIMARY KEY REFERENCES crews (id) ON DELETE CASCADE,
    enabled         boolean NOT NULL DEFAULT false,
    -- When to ask, in plain language; it goes into every agent's instructions
    rules           text NOT NULL DEFAULT '' CHECK (length(rules) <= 4000),
    timeout_minutes int NOT NULL DEFAULT 30 CHECK (timeout_minutes BETWEEN 1 AND 180),
    -- What the agent does when nobody answers in time: carry on and state its assumption, or stop the run
    on_timeout      text NOT NULL DEFAULT 'proceed' CHECK (on_timeout IN ('proceed', 'fail')),
    -- Questions per run, so a looping agent cannot flood the person
    max_questions   int NOT NULL DEFAULT 5 CHECK (max_questions BETWEEN 1 AND 20),
    updated_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- One question an agent asked during a run
CREATE TABLE human_questions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id        uuid NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    -- The person asked: the human who started the run. NULL when a service account, trigger or schedule started it;
    -- then anyone who may cancel work orders in the run's areas can answer
    assignee_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    agent_role    text NOT NULL DEFAULT '' CHECK (length(agent_role) <= 200),
    task_name     text NOT NULL DEFAULT '' CHECK (length(task_name) <= 200),
    question      text NOT NULL CHECK (length(question) BETWEEN 1 AND 2000),
    context       text NOT NULL DEFAULT '' CHECK (length(context) <= 4000),
    status        text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'answered', 'timed_out', 'closed')),
    answer        text NOT NULL DEFAULT '' CHECK (length(answer) <= 4000),
    answered_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    asked_at      timestamptz NOT NULL DEFAULT now(),
    deadline_at   timestamptz NOT NULL,
    answered_at   timestamptz,
    CHECK ((status = 'answered') = (answered_at IS NOT NULL))
);
CREATE INDEX human_questions_run_idx ON human_questions (run_id, asked_at);
CREATE INDEX human_questions_open_idx ON human_questions (assignee_id) WHERE status = 'open';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deletes every question and every crew's escalation settings.
DROP TABLE human_questions, crew_escalation;
-- +goose StatementEnd
