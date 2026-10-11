-- Agent chat, phase 3, slice 3b (docs/agent-chat-handoffs-design.md, sections 3 to 5): hand-offs. Work too long for
-- one chat turn goes to a background run of kind 'handoff' that the person starts by confirming a card. Its result is
-- an assistant message created empty with the hand-off and filled from the run's output, as replies are.

-- +goose Up
-- +goose StatementBegin
-- A third kind of run: a hand-off from an agent chat, worked in the background
ALTER TABLE runs DROP CONSTRAINT runs_kind_check;
ALTER TABLE runs ADD CONSTRAINT runs_kind_check CHECK (kind IN ('crew', 'chat', 'handoff'));

-- One hand-off: the background run and what it was asked to do. Its result is the assistant message whose run_id is
-- run_id. Deleting the conversation deletes the hand-off; the run is kept, as chat runs are, so budgets still count it.
-- Deleting the run (after a day, from the Runs page) keeps the hand-off and its result, as a chat reply is kept.
CREATE TABLE chat_handoffs (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id    uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    -- The reply whose Confirm card started it
    request_message_id uuid REFERENCES conversation_messages (id) ON DELETE SET NULL,
    run_id             uuid UNIQUE REFERENCES runs (id) ON DELETE SET NULL,
    worker_agent_id    uuid REFERENCES agents (id) ON DELETE SET NULL,
    worker_name        text NOT NULL DEFAULT '',
    worker_role        text NOT NULL CHECK (length(worker_role) <= 200),
    title              text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    brief              text NOT NULL CHECK (length(brief) BETWEEN 1 AND 10000),
    budget_usd         double precision NOT NULL CHECK (budget_usd > 0),
    timeout_minutes    integer NOT NULL CHECK (timeout_minutes BETWEEN 1 AND 180),
    -- created_at plus the time limit while queued; set again from when the run starts, so waiting for a slot is free
    deadline_at        timestamptz NOT NULL,
    created_by         uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX chat_handoffs_conversation_idx ON chat_handoffs (conversation_id, created_at DESC);

-- The result message of a hand-off (run_id = the hand-off's run)
ALTER TABLE conversation_messages ADD COLUMN handoff_id uuid REFERENCES chat_handoffs (id) ON DELETE CASCADE;
CREATE INDEX conversation_messages_handoff_idx ON conversation_messages (handoff_id) WHERE handoff_id IS NOT NULL;
-- Replaces the role check of 0014 (as changed by 0024 for summaries, which a run writes but which never propose):
-- only assistant messages carry a proposal or a hand-off
ALTER TABLE conversation_messages DROP CONSTRAINT conversation_messages_check1;
ALTER TABLE conversation_messages ADD CONSTRAINT conversation_messages_assistant_only_check
    CHECK ((role = 'assistant' OR (proposal IS NULL AND handoff_id IS NULL)) AND (role IN ('assistant', 'summary') OR run_id IS NULL));

-- Hand-off settings of an agent
ALTER TABLE agent_chat
    ADD COLUMN handoffs_enabled        boolean NOT NULL DEFAULT false,
    -- What one hand-off may spend; also bounded by the conversation's and the person's daily budgets
    ADD COLUMN handoff_budget_usd      double precision NOT NULL DEFAULT 0.5 CHECK (handoff_budget_usd > 0 AND handoff_budget_usd <= 100),
    ADD COLUMN handoff_timeout_minutes integer NOT NULL DEFAULT 20 CHECK (handoff_timeout_minutes BETWEEN 1 AND 180),
    -- Hand-offs running at once in one conversation
    ADD COLUMN max_active_handoffs     integer NOT NULL DEFAULT 2 CHECK (max_active_handoffs BETWEEN 1 AND 5);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deletes every hand-off and its result message. Hand-off runs are kept as chat runs (so budgets still count them).
ALTER TABLE agent_chat
    DROP COLUMN max_active_handoffs,
    DROP COLUMN handoff_timeout_minutes,
    DROP COLUMN handoff_budget_usd,
    DROP COLUMN handoffs_enabled;
ALTER TABLE conversation_messages DROP CONSTRAINT conversation_messages_assistant_only_check;
DELETE FROM conversation_messages WHERE handoff_id IS NOT NULL;
ALTER TABLE conversation_messages ADD CONSTRAINT conversation_messages_check1
    CHECK ((role = 'assistant' OR proposal IS NULL) AND (role IN ('assistant', 'summary') OR run_id IS NULL));
ALTER TABLE conversation_messages DROP COLUMN handoff_id;
DROP TABLE chat_handoffs;
UPDATE runs SET kind = 'chat' WHERE kind = 'handoff';
ALTER TABLE runs DROP CONSTRAINT runs_kind_check;
ALTER TABLE runs ADD CONSTRAINT runs_kind_check CHECK (kind IN ('crew', 'chat'));
-- +goose StatementEnd
