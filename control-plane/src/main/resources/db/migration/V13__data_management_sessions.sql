ALTER TABLE project_data_sources ADD COLUMN name VARCHAR(120);
UPDATE project_data_sources SET name=provider || '-' || environment;
ALTER TABLE project_data_sources ALTER COLUMN name SET NOT NULL;
ALTER TABLE project_data_sources ADD COLUMN resource_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE project_data_sources ADD COLUMN version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE project_data_sources ADD COLUMN policy_version VARCHAR(40) NOT NULL DEFAULT 'data-policy-v2';

CREATE TABLE data_sessions (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id),
    name VARCHAR(120) NOT NULL,
    purpose VARCHAR(24) NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    version BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX ix_data_session_project ON data_sessions(project_id, created_at);
ALTER TABLE data_operation_plans ALTER COLUMN task_id DROP NOT NULL;
ALTER TABLE data_operation_plans ADD COLUMN data_session_id UUID REFERENCES data_sessions(id);
ALTER TABLE data_operation_plans ADD COLUMN resource_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE data_operation_plans ADD COLUMN policy_version VARCHAR(40) NOT NULL DEFAULT 'data-policy-v2';
ALTER TABLE data_operation_plans ADD CONSTRAINT ck_data_operation_context CHECK ((task_id IS NULL) <> (data_session_id IS NULL));
ALTER TABLE data_operation_plans ADD CONSTRAINT uq_data_session_plan_key UNIQUE(data_session_id, idempotency_key);
ALTER TABLE data_operation_audit ALTER COLUMN task_id DROP NOT NULL;
ALTER TABLE data_operation_audit ADD COLUMN data_session_id UUID REFERENCES data_sessions(id);
-- Existing v1 plans are intentionally invalidated by the v2 digest and namespace; they require a new preview and approval.
