-- Agent chat (docs/agent-chat-design.md): people talk to one agent directly. The agent may ask its specialists (other
-- agents) inside the same chat run, and propose a work order on one of the crews it may start. Adds chat settings
-- per agent with their specialists and crews, lets a conversation belong to an agent instead of a crew, and records
-- on each chat run the agent it talked to (the per-person budget is counted from runs).

-- +goose Up
-- +goose StatementBegin
-- Chat settings of an agent; no row means chat with it is off
CREATE TABLE agent_chat (
    agent_id                uuid PRIMARY KEY REFERENCES agents (id) ON DELETE CASCADE,
    enabled                 boolean NOT NULL DEFAULT false,
    -- Optional cheaper model for the agent in chat instead of its own (specialists keep their own)
    llm_id                  uuid REFERENCES llms (id) ON DELETE RESTRICT,
    daily_budget_usd        double precision NOT NULL DEFAULT 1.0 CHECK (daily_budget_usd > 0 AND daily_budget_usd <= 100),
    person_daily_budget_usd double precision NOT NULL DEFAULT 5.0
                            CHECK (person_daily_budget_usd > 0 AND person_daily_budget_usd <= 1000),
    updated_by              uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_chat_llm_idx ON agent_chat (llm_id) WHERE llm_id IS NOT NULL;

-- Agents the chatting agent may ask, in order; removed with either agent
CREATE TABLE agent_chat_specialists (
    agent_id      uuid NOT NULL REFERENCES agent_chat (agent_id) ON DELETE CASCADE,
    specialist_id uuid NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    position      integer NOT NULL,
    PRIMARY KEY (agent_id, specialist_id),
    CHECK (agent_id <> specialist_id)
);
CREATE INDEX agent_chat_specialists_specialist_idx ON agent_chat_specialists (specialist_id);

-- Crews the chatting agent may propose a work order on, in order
CREATE TABLE agent_chat_crews (
    agent_id uuid NOT NULL REFERENCES agent_chat (agent_id) ON DELETE CASCADE,
    crew_id  uuid NOT NULL REFERENCES crews (id) ON DELETE CASCADE,
    position integer NOT NULL,
    PRIMARY KEY (agent_id, crew_id)
);
CREATE INDEX agent_chat_crews_crew_idx ON agent_chat_crews (crew_id);

-- A conversation is with a crew or with an agent, never both
ALTER TABLE conversations ALTER COLUMN crew_id DROP NOT NULL;
ALTER TABLE conversations ADD COLUMN agent_id uuid REFERENCES agents (id) ON DELETE CASCADE;
ALTER TABLE conversations ADD CONSTRAINT conversations_target_check CHECK ((crew_id IS NULL) <> (agent_id IS NULL));
-- A conversation about a work order is always with the work order's crew
ALTER TABLE conversations ADD CONSTRAINT conversations_work_order_crew_check CHECK (work_order_id IS NULL OR crew_id IS NOT NULL);
CREATE INDEX conversations_agent_idx ON conversations (agent_id) WHERE agent_id IS NOT NULL;

-- The agent an agent-chat turn talked to; kept when its conversation is deleted so the person's budget still counts it
ALTER TABLE runs ADD COLUMN chat_agent_id uuid REFERENCES agents (id) ON DELETE SET NULL;
CREATE INDEX runs_chat_agent_idx ON runs (chat_agent_id, created_by, created_at) WHERE chat_agent_id IS NOT NULL;

UPDATE permissions SET label = 'Chat with crews and agents',
    description = 'Chat with crews and agents in your areas that have chat turned on, and confirm the changes they propose. Chat turns spend money.'
WHERE key = 'crews.chat';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deletes every conversation with an agent and all agent chat settings. Their chat runs are kept as ordinary runs.
UPDATE permissions SET label = 'Chat with crews',
    description = 'Chat with crews in your areas that have chat turned on, and confirm the changes their front desk proposes. Chat turns spend money.'
WHERE key = 'crews.chat';
ALTER TABLE runs DROP COLUMN chat_agent_id;
DELETE FROM conversations WHERE agent_id IS NOT NULL;
ALTER TABLE conversations DROP CONSTRAINT conversations_work_order_crew_check;
ALTER TABLE conversations DROP CONSTRAINT conversations_target_check;
ALTER TABLE conversations DROP COLUMN agent_id;
ALTER TABLE conversations ALTER COLUMN crew_id SET NOT NULL;
DROP TABLE agent_chat_crews, agent_chat_specialists, agent_chat;
-- +goose StatementEnd
