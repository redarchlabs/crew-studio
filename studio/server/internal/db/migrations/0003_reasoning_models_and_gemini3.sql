-- High-reasoning OpenAI and Anthropic models need reasoning effort and, for tool calling, the Responses API.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE llms
    ADD COLUMN reasoning_effort text CHECK (reasoning_effort IN ('none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max')),
    ADD COLUMN api text NOT NULL DEFAULT 'completions' CHECK (api IN ('completions', 'responses'));

-- Gemini 3 grounding has a monthly free allowance (shared across Gemini 3 models) and bills per search query.
ALTER TABLE settings RENAME COLUMN grounded_searches_free_per_day TO grounded_searches_free;
ALTER TABLE settings
    ADD COLUMN grounding_free_period text NOT NULL DEFAULT 'day' CHECK (grounding_free_period IN ('day', 'month'));

-- Prices from the official pricing pages, October 2026.
INSERT INTO model_prices (model, input_per_mtok, cached_input_per_mtok, output_per_mtok, notes) VALUES
    ('gemini-3.8-flash', 0.75, 0.075, 3.75,
     'Introductory price through 2026-12-31; doubles to 1.50 / 0.15 / 7.50 on 2027-01-01. https://ai.google.dev/gemini-api/docs/pricing'),
    ('gpt-6-astra', 10.00, 1.00, 50.00,
     'Reasoning tokens bill as output. Cache writes 12.50. https://developers.openai.com/api/docs/models/gpt-6-astra'),
    ('gpt-6.1-sol', 2.00, 0.10, 10.00,
     'Reasoning tokens bill as output. Cache writes 2.50. https://developers.openai.com/api/docs/models/gpt-6.1-sol'),
    ('claude-opus-5-5', 4.00, 0.20, 20.00,
     'Thinking tokens bill as output. https://platform.claude.com/docs/en/about-claude/pricing'),
    ('claude-fable-5-1', 10.00, 0.25, 50.00,
     'Thinking tokens bill as output. https://platform.claude.com/docs/en/about-claude/pricing'),
    ('claude-sonnet-5-5', 2.00, 0.20, 10.00,
     'Thinking tokens bill as output. https://platform.claude.com/docs/en/about-claude/pricing'),
    ('claude-haiku-4-5', 1.00, 0.10, 5.00,
     'Cache reads at 0.1x input. https://platform.claude.com/docs/en/about-claude/pricing')
ON CONFLICT (model) DO NOTHING;

INSERT INTO llms (name, model, temperature, max_completion_tokens, reasoning_effort, api) VALUES
    ('GPT-6 Astra (high reasoning)', 'openai/gpt-6-astra', NULL, 32000, 'high', 'responses'),
    ('GPT-6.1 Sol (high reasoning)', 'openai/gpt-6.1-sol', NULL, 32000, 'high', 'responses'),
    -- Claude: effort sets reasoning depth (thinking is always on for the 5.x models). 16k keeps requests
    -- under the SDK's non-streaming size limit.
    ('Claude Opus 5.5 (high effort)', 'anthropic/claude-opus-5-5', NULL, 16000, 'high', 'completions'),
    ('Claude Fable 5.1 (high effort)', 'anthropic/claude-fable-5-1', NULL, 16000, 'high', 'completions'),
    ('Claude Sonnet 5.5 (high effort)', 'anthropic/claude-sonnet-5-5', NULL, 16000, 'high', 'completions'),
    ('Claude Haiku 4.5', 'anthropic/claude-haiku-4-5', NULL, 16000, NULL, 'completions')
ON CONFLICT (name) DO NOTHING;

-- Search moves from Gemini 2.5 Flash to Gemini 3.8 Flash and its grounding terms.
UPDATE settings SET
    search_model = 'gemini-3.8-flash',
    grounded_search_cost_per_1000 = 14.0,
    grounded_searches_free = 5000,
    grounding_free_period = 'month';

DELETE FROM model_prices WHERE model = 'gemini-2.5-flash';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Back to Gemini 2.5 Flash search and its daily free allowance, without the reasoning models. Fails if an agent
-- or crew still uses one of the LLMs this migration added. The Up skipped prices and LLMs that already existed
-- under these names, and they have no fixed ids, so this also removes such rows if you had made them yourself.
DELETE FROM llms WHERE name IN (
    'GPT-6 Astra (high reasoning)', 'GPT-6.1 Sol (high reasoning)', 'Claude Opus 5.5 (high effort)',
    'Claude Fable 5.1 (high effort)', 'Claude Sonnet 5.5 (high effort)', 'Claude Haiku 4.5');
DELETE FROM model_prices WHERE model IN (
    'gemini-3.8-flash', 'gpt-6-astra', 'gpt-6.1-sol', 'claude-opus-5-5', 'claude-fable-5-1', 'claude-sonnet-5-5',
    'claude-haiku-4-5');
INSERT INTO model_prices (model, input_per_mtok, cached_input_per_mtok, output_per_mtok, notes) VALUES
    ('gemini-2.5-flash', 0.30, 0.03, 2.50, 'https://cloud.google.com/vertex-ai/generative-ai/pricing')
ON CONFLICT (model) DO NOTHING;
UPDATE settings SET
    search_model = 'gemini-2.5-flash',
    grounded_search_cost_per_1000 = 35.0,
    grounded_searches_free = 1500;
ALTER TABLE settings DROP COLUMN grounding_free_period;
ALTER TABLE settings RENAME COLUMN grounded_searches_free TO grounded_searches_free_per_day;
ALTER TABLE llms DROP COLUMN api, DROP COLUMN reasoning_effort;
-- +goose StatementEnd
