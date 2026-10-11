-- Prompt cache writes: some providers charge more to write a prompt to their cache than to read it as input
-- (Anthropic: 1.25 times the input price for the default 5-minute cache). Models get a cache-write price, used for the
-- tokens a call wrote to the cache; NULL means the input price. LLM calls record how many tokens they wrote.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE model_prices ADD COLUMN cache_write_per_mtok double precision
    CHECK (cache_write_per_mtok IS NULL OR cache_write_per_mtok >= 0);
UPDATE model_prices SET cache_write_per_mtok = round((input_per_mtok * 1.25)::numeric, 6)
WHERE (model ILIKE 'claude-%' OR model ILIKE 'anthropic/%') AND cache_write_per_mtok IS NULL;

ALTER TABLE llm_calls ADD COLUMN cache_write_tokens integer NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE llm_calls DROP COLUMN cache_write_tokens;
ALTER TABLE model_prices DROP COLUMN cache_write_per_mtok;
-- +goose StatementEnd
