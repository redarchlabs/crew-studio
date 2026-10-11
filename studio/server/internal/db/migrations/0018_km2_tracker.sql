-- KM2 issues as a tracker (docs/trackers.md, "KM2 issues"): a km2_tracker connection reads the trk_* custom entities
-- of a KM2 organisation with an org-wide records key. It is its own kind, not km2: km2 connections hold a key scoped
-- to a KM2 profile, which KM2 never lets read or write records, and it counts as a tracker everywhere.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE connections DROP CONSTRAINT connections_kind_check;
ALTER TABLE connections ADD CONSTRAINT connections_kind_check
    CHECK (kind IN ('jira', 'linear', 'km2_tracker', 'km2', 'http', 'slack', 'teams', 'email'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deletes every km2_tracker connection with the watches and tracking-list items that use it (their secrets live in
-- the row; crew links and people maps cascade; report deliveries keep their history with the connection unset).
-- Report destinations never use a tracker connection, but any that did would block the delete, so they go too.
DELETE FROM watches WHERE connection_id IN (SELECT id FROM connections WHERE kind = 'km2_tracker');
DELETE FROM tracking_items WHERE connection_id IN (SELECT id FROM connections WHERE kind = 'km2_tracker');
DELETE FROM report_destinations WHERE connection_id IN (SELECT id FROM connections WHERE kind = 'km2_tracker');
DELETE FROM connections WHERE kind = 'km2_tracker';
ALTER TABLE connections DROP CONSTRAINT connections_kind_check;
ALTER TABLE connections ADD CONSTRAINT connections_kind_check
    CHECK (kind IN ('jira', 'linear', 'km2', 'http', 'slack', 'teams', 'email'));
-- +goose StatementEnd
