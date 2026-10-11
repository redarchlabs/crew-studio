-- Whether an LLM accepts images and PDFs. Work orders pass their pasted images and PDFs only to agents whose
-- model can read them; CrewAI's own list of vision models predates GPT-6 and Claude 5, so this is explicit.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE llms ADD COLUMN vision boolean NOT NULL DEFAULT false;

UPDATE llms SET vision = true
WHERE model ILIKE ANY (ARRAY['%gpt-6%', '%gpt-5%', '%gpt-4o%', '%gpt-4.1%', '%claude%', '%gemini%']);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE llms DROP COLUMN vision;
-- +goose StatementEnd
