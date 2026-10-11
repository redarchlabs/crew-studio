-- Agent chat, phase 3, slice 3d (docs/agent-chat-handoffs-design.md, section 7): specialists that propose. A tool that
-- changes things (a tracker comment, suggestion or new issue, a KM2 save) never reaches agent chat; its proposing
-- counterpart does, which only makes a Confirm card. The lead always gets the counterparts of its own changing tools;
-- its specialists get theirs only when the agent's builder turns this on. A hand-off's proposals are messages of their
-- own (handoff_id set, run_id null), which need no schema change.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE agent_chat ADD COLUMN specialists_may_propose boolean NOT NULL DEFAULT false;
-- Finding a hand-off's messages in order (its result first, then the cards it proposed); replaces 0028's index on
-- handoff_id alone
CREATE INDEX conversation_messages_handoff_seq_idx ON conversation_messages (handoff_id, seq) WHERE handoff_id IS NOT NULL;
DROP INDEX conversation_messages_handoff_idx;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deletes the cards hand-offs proposed (messages of a hand-off without a run): before 0030 a hand-off's only message
-- without a run is its result once that run is deleted, and the older code would mistake a card for it
DELETE FROM conversation_messages m WHERE m.handoff_id IS NOT NULL AND m.run_id IS NULL AND m.proposal IS NOT NULL
  AND EXISTS (SELECT 1 FROM conversation_messages e WHERE e.handoff_id = m.handoff_id AND e.seq < m.seq);
CREATE INDEX conversation_messages_handoff_idx ON conversation_messages (handoff_id) WHERE handoff_id IS NOT NULL;
DROP INDEX conversation_messages_handoff_seq_idx;
ALTER TABLE agent_chat DROP COLUMN specialists_may_propose;
-- +goose StatementEnd
