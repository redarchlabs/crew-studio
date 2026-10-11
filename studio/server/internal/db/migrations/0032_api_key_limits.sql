-- API keys can be limited below their account: to the REST API, MCP or both, to some of the account's permissions
-- and to some of its functional areas. A key never gets more than its account holds at the time of the request
-- (the server checks both), so taking access from the account takes it from its keys too. NULL permissions or
-- area_ids means everything the account holds, which is what keys made before this keep.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE api_keys
    ADD COLUMN allow_api   boolean NOT NULL DEFAULT true,
    ADD COLUMN allow_mcp   boolean NOT NULL DEFAULT true,
    ADD COLUMN permissions text[],
    ADD COLUMN area_ids    uuid[],
    ADD CONSTRAINT api_keys_some_channel CHECK (allow_api OR allow_mcp),
    ADD CONSTRAINT api_keys_permissions_not_empty CHECK (permissions IS NULL OR cardinality(permissions) > 0),
    ADD CONSTRAINT api_keys_areas_not_empty CHECK (area_ids IS NULL OR cardinality(area_ids) > 0);

-- Work a key sets up keeps the key's limits when it runs later: a run records the key that started it (agent chat
-- looks up the connections its starter may use while it runs), and a schedule or trigger the key that last saved it.
-- No foreign key on purpose: a key row only goes with its account, and a key that is missing must count as revoked,
-- never as "no limits".
ALTER TABLE runs ADD COLUMN api_key_id uuid;
ALTER TABLE schedules ADD COLUMN run_as_key uuid;
ALTER TABLE triggers ADD COLUMN run_as_key uuid;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE triggers DROP COLUMN run_as_key;
ALTER TABLE schedules DROP COLUMN run_as_key;
ALTER TABLE runs DROP COLUMN api_key_id;
ALTER TABLE api_keys
    DROP CONSTRAINT api_keys_areas_not_empty,
    DROP CONSTRAINT api_keys_permissions_not_empty,
    DROP CONSTRAINT api_keys_some_channel,
    DROP COLUMN area_ids,
    DROP COLUMN permissions,
    DROP COLUMN allow_mcp,
    DROP COLUMN allow_api;
-- +goose StatementEnd
