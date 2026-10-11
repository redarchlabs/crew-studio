-- Memory (docs/crew-state-and-pmo-design.md, section 19): soft lessons a crew learns by doing its work, recalled
-- before each task by similarity. Studio owns the rows, the vectors and the scope; the runner keeps CrewAI's recall
-- and extraction logic and stores through the run-state API. Adds memory settings per crew, the memories themselves
-- and a per-run usage counter (so deleting rows cannot get a run around its caps).

-- +goose Up
-- +goose StatementBegin
-- Memory settings of a crew; no row means memory is off with the defaults below
CREATE TABLE crew_memory (
    crew_id                uuid PRIMARY KEY REFERENCES crews (id) ON DELETE CASCADE,
    mode                   text NOT NULL DEFAULT 'off' CHECK (mode IN ('off', 'recall_only', 'explicit', 'automatic')),
    -- The model for automatic extraction and deep recall; NULL is the crew's cheapest agent model at launch
    llm_id                 uuid REFERENCES llms (id) ON DELETE RESTRICT,
    recall_depth           text NOT NULL DEFAULT 'shallow' CHECK (recall_depth IN ('shallow', 'deep')),
    recall_limit           int NOT NULL DEFAULT 5 CHECK (recall_limit BETWEEN 1 AND 20),
    saves_per_run          int NOT NULL DEFAULT 20 CHECK (saves_per_run BETWEEN 0 AND 100),
    -- Unpinned memories older than this are deleted; NULL keeps them
    retention_days         int DEFAULT 365 CHECK (retention_days BETWEEN 1 AND 3650),
    -- Every memory a run saves (Remember tool, automatic extraction) waits as pending until a person approves it
    review_run_saves       boolean NOT NULL DEFAULT true,
    recency_weight         real NOT NULL DEFAULT 0.3 CHECK (recency_weight BETWEEN 0 AND 1),
    semantic_weight        real NOT NULL DEFAULT 0.5 CHECK (semantic_weight BETWEEN 0 AND 1),
    importance_weight      real NOT NULL DEFAULT 0.2 CHECK (importance_weight BETWEEN 0 AND 1),
    recency_half_life_days int NOT NULL DEFAULT 30 CHECK (recency_half_life_days BETWEEN 1 AND 3650),
    updated_by             uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at             timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crew_memory_llm_idx ON crew_memory (llm_id) WHERE llm_id IS NOT NULL;

-- One memory. scope is a path relative to the crew ('/', '/people/sam'); crew_id is the boundary
CREATE TABLE memories (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    crew_id         uuid NOT NULL REFERENCES crews (id) ON DELETE CASCADE,
    scope           text NOT NULL CHECK (length(scope) <= 200),
    content         text NOT NULL CHECK (length(content) BETWEEN 1 AND 2000),
    -- SHA-256 of the normalised content, for exact duplicates within a scope
    content_hash    text NOT NULL,
    categories      text[] NOT NULL DEFAULT '{}',
    importance      real NOT NULL DEFAULT 0.5 CHECK (importance BETWEEN 0 AND 1),
    metadata        jsonb NOT NULL DEFAULT '{}',
    -- run:<run id> or user:<name>
    source          text NOT NULL DEFAULT '',
    -- pending memories are not recalled by runs until a person approves them
    status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'pending')),
    pinned          boolean NOT NULL DEFAULT false,
    agent_role      text NOT NULL DEFAULT '',
    task_name       text NOT NULL DEFAULT '',
    -- The embedder's model and size (such as text-embedding-3-small:768); rows of another model are re-embedded
    embed_model     text NOT NULL,
    vector          real[] NOT NULL,
    run_id          uuid REFERENCES runs (id) ON DELETE SET NULL,
    written_by      text NOT NULL,
    km2_document_id text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_accessed   timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (crew_id, scope, content_hash)
);
CREATE INDEX memories_crew_scope ON memories (crew_id, scope text_pattern_ops);
CREATE INDEX memories_run ON memories (run_id) WHERE run_id IS NOT NULL;
CREATE INDEX memories_embed_model ON memories (embed_model);

-- What a run used: texts embedded (cache misses) and memories saved
CREATE TABLE run_memory_usage (
    run_id      uuid PRIMARY KEY REFERENCES runs (id) ON DELETE CASCADE,
    embed_texts int NOT NULL DEFAULT 0,
    saves       int NOT NULL DEFAULT 0
);

UPDATE permissions SET description = 'See the action log, notes (with their versions) and memories of crews in your areas, and their memory settings.'
WHERE key = 'state.view';
UPDATE permissions SET description = 'Edit, roll back and delete notes, delete action log entries, and add, edit, approve, delete and promote memories, for crews in your areas.'
WHERE key = 'state.manage';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deletes every memory and every crew's memory settings.
UPDATE permissions SET description = 'See the action log and notes (with their versions) of crews in your areas.' WHERE key = 'state.view';
UPDATE permissions SET description = 'Edit, roll back and delete notes, and delete action log entries, for crews in your areas.' WHERE key = 'state.manage';
DROP TABLE run_memory_usage, memories, crew_memory;
-- +goose StatementEnd
