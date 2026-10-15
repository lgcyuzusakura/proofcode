CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE projects (
    id uuid PRIMARY KEY,
    name varchar(120) NOT NULL,
    repository_url text NOT NULL,
    default_branch varchar(200) NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE TABLE tasks (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects(id),
    prompt text NOT NULL,
    model varchar(200) NOT NULL,
    status varchar(40) NOT NULL,
    result text,
    error text,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 0
    ,idempotency_key varchar(200)
);
CREATE INDEX idx_tasks_project_created ON tasks(project_id, created_at DESC);
CREATE UNIQUE INDEX uq_tasks_project_idempotency ON tasks(project_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

CREATE TABLE task_events (
    id bigserial PRIMARY KEY,
    task_id uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    sequence bigint NOT NULL,
    type varchar(80) NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT uq_task_event_sequence UNIQUE(task_id, sequence)
);

CREATE TABLE outbox_messages (
    id uuid PRIMARY KEY,
    aggregate_id uuid NOT NULL,
    destination varchar(200) NOT NULL,
    payload jsonb NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL,
    delivered_at timestamptz,
    created_at timestamptz NOT NULL
);
CREATE INDEX idx_outbox_pending ON outbox_messages(next_attempt_at) WHERE delivered_at IS NULL;

CREATE TABLE code_chunks (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path text NOT NULL,
    symbol text,
    start_line integer NOT NULL,
    end_line integer NOT NULL,
    content text NOT NULL,
    content_hash varchar(64) NOT NULL,
    search_vector tsvector GENERATED ALWAYS AS (to_tsvector('simple', coalesce(path,'') || ' ' || coalesce(symbol,'') || ' ' || content)) STORED,
    embedding vector(1536),
    updated_at timestamptz NOT NULL,
    CONSTRAINT uq_code_chunk_hash UNIQUE(project_id, path, content_hash)
);
CREATE INDEX idx_code_chunks_fts ON code_chunks USING gin(search_vector);
CREATE INDEX idx_code_chunks_path_trgm ON code_chunks USING gin(path gin_trgm_ops);
CREATE INDEX idx_code_chunks_embedding ON code_chunks USING hnsw (embedding vector_cosine_ops);

CREATE TABLE symbol_edges (
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_symbol text NOT NULL,
    target_symbol text NOT NULL,
    relation varchar(40) NOT NULL,
    path text NOT NULL,
    PRIMARY KEY(project_id, source_symbol, target_symbol, relation, path)
);
