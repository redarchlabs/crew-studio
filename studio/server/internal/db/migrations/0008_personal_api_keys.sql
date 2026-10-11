-- Personal API keys: people holding account.api_keys can create keys that act as themselves, with their own
-- permissions and areas. Builders (and Administrators, who hold everything) get it; grant it to other roles
-- on the Roles page.

-- +goose Up
-- +goose StatementBegin
INSERT INTO permissions (key, feature, label, description, position) VALUES
    ('account.api_keys', 'Account', 'Create personal API keys',
     'Create API keys that act as you, with your permissions and areas. Taking this away stops your keys working.', 150);

-- Roles people have since deleted are skipped
INSERT INTO role_permissions (role_id, permission)
SELECT id, 'account.api_keys' FROM roles
WHERE id IN ('66666666-0000-4000-8000-000000000001', '66666666-0000-4000-8000-000000000002')
ON CONFLICT DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM permissions WHERE key = 'account.api_keys';
-- +goose StatementEnd
