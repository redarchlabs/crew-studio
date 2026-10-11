-- Crew state, phase 3 (docs/crew-state-and-pmo-design.md, section 8): crew chat. A crew with chat on names one of
-- its agents as the front desk; each message a person sends is a short single-agent run of kind 'chat'. Changes the
-- front desk wants to make are stored as proposals on its reply and only applied when that person confirms them.
-- Adds runs.kind, chat settings per crew, conversations and their messages, the 'chat' work order source and the
-- crews.chat permission.

-- +goose Up
-- +goose StatementBegin
-- 'crew' is every run before this change; 'chat' is one turn of a conversation
ALTER TABLE runs ADD COLUMN kind text NOT NULL DEFAULT 'crew' CHECK (kind IN ('crew', 'chat'));

-- A work order started by confirming a chat proposal; source_id is the conversation
ALTER TABLE work_orders DROP CONSTRAINT work_orders_source_check;
ALTER TABLE work_orders ADD CONSTRAINT work_orders_source_check
    CHECK (source IN ('manual', 'api', 'schedule', 'webhook', 'chat'));

-- Chat settings of a crew; no row means chat is off
CREATE TABLE crew_chat (
    crew_id             uuid PRIMARY KEY REFERENCES crews (id) ON DELETE CASCADE,
    enabled             boolean NOT NULL DEFAULT false,
    -- One of the crew's agents; checked when a turn starts, since agents can leave the crew later
    front_desk_agent_id uuid REFERENCES agents (id) ON DELETE SET NULL,
    -- Optional cheaper model for chat turns instead of the front desk agent's own
    llm_id              uuid REFERENCES llms (id) ON DELETE RESTRICT,
    -- What one conversation may spend in any 24 hours
    daily_budget_usd    double precision NOT NULL DEFAULT 1.0 CHECK (daily_budget_usd > 0 AND daily_budget_usd <= 100),
    -- What one person may spend on this crew's chat in any 24 hours, across all their conversations (deleting a
    -- conversation or starting another does not reset it)
    person_daily_budget_usd double precision NOT NULL DEFAULT 5.0
                        CHECK (person_daily_budget_usd > 0 AND person_daily_budget_usd <= 1000),
    updated_by          uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crew_chat_llm_idx ON crew_chat (llm_id) WHERE llm_id IS NOT NULL;

-- Conversations belong to one person and one crew
CREATE TABLE conversations (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    crew_id    uuid NOT NULL REFERENCES crews (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX conversations_user_idx ON conversations (user_id, updated_at DESC);

CREATE TABLE conversation_messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Message order; a person's message and the reply it starts share one created_at
    seq             bigint GENERATED ALWAYS AS IDENTITY,
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    role            text NOT NULL CHECK (role IN ('user', 'assistant')),
    -- An assistant reply is empty until its run succeeds (its output is copied in then)
    content         text NOT NULL,
    -- A change waiting for Confirm: {"type": ..., "summary": ..., "payload": {...}}
    proposal        jsonb,
    proposal_state  text CHECK (proposal_state IN ('pending', 'confirmed', 'declined', 'expired')),
    -- When the front desk made the proposal; it expires 24 hours later
    proposed_at     timestamptz,
    -- What applying the proposal did (such as the work order it started), and whether it was undone
    proposal_result jsonb,
    decided_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    decided_by_name text NOT NULL DEFAULT '',
    decided_at      timestamptz,
    undone_at       timestamptz,
    -- The chat run that wrote an assistant reply
    run_id          uuid REFERENCES runs (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CHECK ((proposal IS NULL) = (proposal_state IS NULL) AND (proposal IS NULL) = (proposed_at IS NULL)),
    CHECK (role = 'assistant' OR (proposal IS NULL AND run_id IS NULL))
);
CREATE INDEX conversation_messages_conversation_idx ON conversation_messages (conversation_id, seq);
CREATE UNIQUE INDEX conversation_messages_run_idx ON conversation_messages (run_id) WHERE run_id IS NOT NULL;

INSERT INTO permissions (key, feature, label, description, position) VALUES
    ('crews.chat', 'Crews', 'Chat with crews',
     'Chat with crews in your areas that have chat turned on, and confirm the changes their front desk proposes. Chat turns spend money.', 15);

-- Administrators hold everything; Builders and Operators may chat. Roles people have since deleted are skipped.
INSERT INTO role_permissions (role_id, permission)
SELECT id, 'crews.chat' FROM roles
WHERE id IN ('66666666-0000-4000-8000-000000000001', '66666666-0000-4000-8000-000000000002',
             '66666666-0000-4000-8000-000000000003')
ON CONFLICT DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deletes every conversation and the chat settings. Chat runs are kept as ordinary runs; work orders started from
-- chat are kept and shown as manual.
DELETE FROM permissions WHERE key = 'crews.chat';
DROP TABLE conversation_messages, conversations, crew_chat;
UPDATE work_orders SET source = 'manual' WHERE source = 'chat';
ALTER TABLE work_orders DROP CONSTRAINT work_orders_source_check;
ALTER TABLE work_orders ADD CONSTRAINT work_orders_source_check
    CHECK (source IN ('manual', 'api', 'schedule', 'webhook'));
ALTER TABLE runs DROP COLUMN kind;
-- +goose StatementEnd
