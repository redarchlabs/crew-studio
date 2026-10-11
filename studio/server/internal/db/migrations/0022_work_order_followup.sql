-- Work order follow-up: talk with the crew about a work order (a crew conversation linked to it, one per person),
-- and revise it with feedback (a new attempt given the previous output and the feedback; each feedback is kept).

-- +goose Up
-- +goose StatementBegin
ALTER TABLE conversations ADD COLUMN work_order_id uuid REFERENCES work_orders (id) ON DELETE CASCADE;
CREATE UNIQUE INDEX conversations_work_order_user_idx ON conversations (work_order_id, user_id) WHERE work_order_id IS NOT NULL;

-- Feedback someone gave on an attempt, and the attempt it started
CREATE TABLE work_order_feedback (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    work_order_id   uuid NOT NULL REFERENCES work_orders (id) ON DELETE CASCADE,
    -- The attempt the feedback is about
    run_id          uuid REFERENCES runs (id) ON DELETE SET NULL,
    -- The attempt started to act on it; NULL until it starts (or if starting failed)
    revision_run_id uuid REFERENCES runs (id) ON DELETE SET NULL,
    feedback        text NOT NULL CHECK (length(feedback) BETWEEN 1 AND 8000),
    -- Whether the conversation about the work order was passed to the revision
    with_conversation boolean NOT NULL DEFAULT false,
    created_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    created_by_name text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX work_order_feedback_wo_idx ON work_order_feedback (work_order_id, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deletes all work order feedback and every conversation about a work order.
DROP TABLE work_order_feedback;
DELETE FROM conversations WHERE work_order_id IS NOT NULL;
DROP INDEX conversations_work_order_user_idx;
ALTER TABLE conversations DROP COLUMN work_order_id;
-- +goose StatementEnd
