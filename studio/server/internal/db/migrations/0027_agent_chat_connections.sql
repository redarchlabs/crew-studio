-- Agent chat, phase 3, slice 3a (docs/agent-chat-handoffs-design.md, section 3): the connections an agent may read
-- through in chat. Agents have no crew, so they cannot borrow a crew's connections; tracker and KM2 reads in agent
-- chat go only through connections listed here that the person chatting can also see. Which tools only read is
-- declared in runner/tools_catalog.json ("effect"), not in the database.

-- +goose Up
-- +goose StatementBegin
-- Connections an agent may read through in chat, in order; removed with the agent's chat settings or the connection
CREATE TABLE agent_chat_connections (
    agent_id      uuid NOT NULL REFERENCES agent_chat (agent_id) ON DELETE CASCADE,
    connection_id uuid NOT NULL REFERENCES connections (id) ON DELETE CASCADE,
    position      integer NOT NULL,
    PRIMARY KEY (agent_id, connection_id)
);
CREATE INDEX agent_chat_connections_connection_idx ON agent_chat_connections (connection_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Agents lose the connections listed for chat; the connections themselves are kept.
DROP TABLE agent_chat_connections;
-- +goose StatementEnd
