-- Help search: vectors for each help section, so the articles are embedded once per model and content change
-- rather than on every start. Rows are a cache; deleting them only costs a re-embed.

-- +goose Up
-- +goose StatementBegin
CREATE TABLE help_embeddings (
    model        text        NOT NULL,
    content_hash text        NOT NULL,
    vector       real[]      NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (model, content_hash)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE help_embeddings;
-- +goose StatementEnd
