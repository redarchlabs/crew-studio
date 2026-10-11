-- Agent chat, phase 3, slice 3e (docs/agent-chat-handoffs-design.md, sections 3 and 7): connections to outside MCP
-- servers (Streamable HTTP; config: url, auth_header; secret: auth_value), and the tools each one offers. Not to be
-- confused with Studio's own MCP server (POST /mcp), which serves Claude Code and needs no connection.
--
-- An administrator discovers a server's tools and says what each one does. Agent chat may use a tool only when its
-- effect is 'reads' and it is enabled; the server, never the runner, calls the MCP server with the stored secret.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE connections DROP CONSTRAINT connections_kind_check;
ALTER TABLE connections ADD CONSTRAINT connections_kind_check
    CHECK (kind IN ('jira', 'linear', 'km2_tracker', 'km2', 'http', 'slack', 'teams', 'email', 'mcp'));

-- The tools an MCP server offers, as Discover tools last found them, with the effect an administrator gave each one.
-- The name, description and input schema are the MCP server's words: untrusted, shown as text, fenced in prompts.
CREATE TABLE connection_tools (
    connection_id uuid NOT NULL REFERENCES connections (id) ON DELETE CASCADE,
    name          text NOT NULL CHECK (name ~ '^[A-Za-z0-9_.-]{1,128}$'),
    description   text NOT NULL DEFAULT '' CHECK (length(description) <= 4000),
    input_schema  jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(input_schema) = 'object'),
    -- Set from the server's readOnlyHint annotation on discovery; an administrator may change it. Discovery keeps it
    -- while the tool's description and input schema stay the same, and takes the hint again when they change.
    effect        text NOT NULL DEFAULT 'changes' CHECK (effect IN ('reads', 'changes')),
    enabled       boolean NOT NULL DEFAULT true,
    discovered_at timestamptz NOT NULL DEFAULT now(),
    updated_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, name)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- MCP connections go with their tools (and from every list that names them)
DROP TABLE connection_tools;
DELETE FROM connections WHERE kind = 'mcp';
ALTER TABLE connections DROP CONSTRAINT connections_kind_check;
ALTER TABLE connections ADD CONSTRAINT connections_kind_check
    CHECK (kind IN ('jira', 'linear', 'km2_tracker', 'km2', 'http', 'slack', 'teams', 'email'));
-- +goose StatementEnd
