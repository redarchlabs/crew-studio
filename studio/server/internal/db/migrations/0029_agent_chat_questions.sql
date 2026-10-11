-- Agent chat, phase 3, slice 3c (docs/agent-chat-handoffs-design.md, section 6): questions from a specialist to the
-- lead and, during a hand-off, from the lead to the person. The settings mirror crew_escalation (0021), so the runner's
-- Ask a human tool and the human_questions table are reused as they are. A chat question is found through its run
-- (human_questions.run_id = chat_handoffs.run_id) and is addressed to the run's creator, the conversation's owner.

-- +goose Up
-- +goose StatementBegin
-- When the lead may bring a specialist's question to the person during a hand-off
ALTER TABLE agent_chat
    ADD COLUMN questions_enabled        boolean NOT NULL DEFAULT false,
    ADD COLUMN question_rules           text NOT NULL DEFAULT '' CHECK (length(question_rules) <= 4000),
    ADD COLUMN question_timeout_minutes integer NOT NULL DEFAULT 30 CHECK (question_timeout_minutes BETWEEN 1 AND 180),
    ADD COLUMN question_on_timeout      text NOT NULL DEFAULT 'proceed' CHECK (question_on_timeout IN ('proceed', 'fail')),
    ADD COLUMN max_questions            integer NOT NULL DEFAULT 3 CHECK (max_questions BETWEEN 1 AND 20);

-- The agent a question was asked for: in a hand-off the lead asks the person on behalf of the specialist that asked it
-- (the card says "asked by <specialist> through <lead>"). Empty when the asking agent asked for itself, as crews do.
ALTER TABLE human_questions ADD COLUMN asked_for text NOT NULL DEFAULT '' CHECK (length(asked_for) <= 200);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE human_questions DROP COLUMN asked_for;
ALTER TABLE agent_chat
    DROP COLUMN max_questions,
    DROP COLUMN question_on_timeout,
    DROP COLUMN question_timeout_minutes,
    DROP COLUMN question_rules,
    DROP COLUMN questions_enabled;
-- +goose StatementEnd
