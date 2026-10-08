CREATE TABLE project_data_sources (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id),
    provider VARCHAR(24) NOT NULL,
    environment VARCHAR(24) NOT NULL,
    secret_ref VARCHAR(160) NOT NULL,
    allowed_schemas TEXT NOT NULL DEFAULT '[]',
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT uq_project_data_source UNIQUE (project_id, id)
);

CREATE TABLE data_schema_snapshots (
    id UUID PRIMARY KEY,
    connection_id UUID NOT NULL REFERENCES project_data_sources(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    schema_version VARCHAR(64) NOT NULL,
    metadata TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE data_operation_plans (
    id UUID PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES tasks(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    attempt INTEGER NOT NULL,
    connection_id UUID NOT NULL REFERENCES project_data_sources(id),
    kind VARCHAR(32) NOT NULL,
    canonical_ir TEXT NOT NULL,
    digest VARCHAR(64) NOT NULL,
    schema_version VARCHAR(64) NOT NULL,
    preview TEXT NOT NULL,
    status VARCHAR(32) NOT NULL,
    capability_hash VARCHAR(64),
    capability_expires_at TIMESTAMPTZ,
    capability_used_at TIMESTAMPTZ,
    idempotency_key VARCHAR(200) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    result_summary TEXT,
    private_snapshot TEXT,
    version BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT uq_data_operation_idempotency UNIQUE (task_id, attempt, idempotency_key)
);
CREATE INDEX ix_data_operation_task ON data_operation_plans(task_id, attempt, created_at);

CREATE TABLE data_operation_audit (
    id UUID PRIMARY KEY,
    operation_id UUID NOT NULL REFERENCES data_operation_plans(id),
    project_id UUID NOT NULL,
    task_id UUID NOT NULL,
    attempt INTEGER NOT NULL,
    action VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL,
    details TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX ix_data_audit_operation ON data_operation_audit(operation_id, created_at);
