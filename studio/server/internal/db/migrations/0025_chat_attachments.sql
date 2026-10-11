-- Files sent with a chat message (pasted screenshots, images, PDFs). They belong to the message, so to one person's
-- private conversation: kind 'chat' with message_id set and no work order or run. Deleting the conversation deletes
-- them (the existing trigger queues the stored objects for deletion).

-- +goose Up
-- +goose StatementBegin
ALTER TABLE attachments ADD COLUMN message_id uuid REFERENCES conversation_messages (id) ON DELETE CASCADE;
ALTER TABLE attachments DROP CONSTRAINT attachments_kind_check;
ALTER TABLE attachments ADD CONSTRAINT attachments_kind_check CHECK (kind IN ('input', 'artifact', 'transcript', 'chat'));
ALTER TABLE attachments DROP CONSTRAINT attachments_check;
ALTER TABLE attachments DROP CONSTRAINT attachments_check1;
ALTER TABLE attachments ADD CONSTRAINT attachments_owner_check CHECK (
    CASE WHEN kind = 'chat' THEN message_id IS NOT NULL AND work_order_id IS NULL AND run_id IS NULL
    ELSE message_id IS NULL AND (work_order_id IS NOT NULL OR run_id IS NOT NULL) AND ((kind = 'input') = (run_id IS NULL))
    END);
CREATE INDEX attachments_message_idx ON attachments (message_id) WHERE message_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM attachments WHERE kind = 'chat';
DROP INDEX attachments_message_idx;
ALTER TABLE attachments DROP CONSTRAINT attachments_owner_check;
ALTER TABLE attachments ADD CONSTRAINT attachments_check CHECK (work_order_id IS NOT NULL OR run_id IS NOT NULL);
ALTER TABLE attachments ADD CONSTRAINT attachments_check1 CHECK ((kind = 'input') = (run_id IS NULL));
ALTER TABLE attachments DROP CONSTRAINT attachments_kind_check;
ALTER TABLE attachments ADD CONSTRAINT attachments_kind_check CHECK (kind IN ('input', 'artifact', 'transcript'));
ALTER TABLE attachments DROP COLUMN message_id;
-- +goose StatementEnd
