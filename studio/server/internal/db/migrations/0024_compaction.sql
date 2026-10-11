-- Compacting context, to cut the tokens every LLM call resends.
-- Chat: a conversation can be compacted into a summary message (role 'summary'); later turns are given the latest
-- summary and the messages after it instead of the full history.
-- Runs: people can ask a running attempt to compact its agents' context (runs.compact_requests counts the requests;
-- the runner compacts each agent once per new request), and settings.compact_context_tokens compacts an agent's
-- context by itself once it passes that many tokens (0 = never).

-- +goose Up
-- +goose StatementBegin
ALTER TABLE conversation_messages DROP CONSTRAINT conversation_messages_role_check;
ALTER TABLE conversation_messages ADD CONSTRAINT conversation_messages_role_check
    CHECK (role IN ('user', 'assistant', 'summary'));
-- A summary is written by a run, like a reply, but never carries a proposal
ALTER TABLE conversation_messages DROP CONSTRAINT conversation_messages_check1;
ALTER TABLE conversation_messages ADD CONSTRAINT conversation_messages_check1
    CHECK ((role = 'assistant' OR proposal IS NULL) AND (role IN ('assistant', 'summary') OR run_id IS NULL));

ALTER TABLE runs ADD COLUMN compact_requests integer NOT NULL DEFAULT 0;

ALTER TABLE settings ADD COLUMN compact_context_tokens integer NOT NULL DEFAULT 0
    CHECK (compact_context_tokens = 0 OR compact_context_tokens BETWEEN 10000 AND 1000000);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE settings DROP COLUMN compact_context_tokens;
ALTER TABLE runs DROP COLUMN compact_requests;
DELETE FROM conversation_messages WHERE role = 'summary';
ALTER TABLE conversation_messages DROP CONSTRAINT conversation_messages_check1;
ALTER TABLE conversation_messages ADD CONSTRAINT conversation_messages_check1
    CHECK (role = 'assistant' OR (proposal IS NULL AND run_id IS NULL));
ALTER TABLE conversation_messages DROP CONSTRAINT conversation_messages_role_check;
ALTER TABLE conversation_messages ADD CONSTRAINT conversation_messages_role_check
    CHECK (role IN ('user', 'assistant'));
-- +goose StatementEnd
