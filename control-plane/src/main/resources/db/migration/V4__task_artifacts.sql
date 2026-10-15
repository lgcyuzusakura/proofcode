CREATE TABLE task_artifacts (
    id uuid PRIMARY KEY,
    task_id uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    kind varchar(40) NOT NULL,
    commit_hash varchar(80),
    branch varchar(255),
    patch text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    CONSTRAINT uq_task_artifact_kind UNIQUE(task_id, kind)
);
CREATE INDEX idx_task_artifacts_task_created ON task_artifacts(task_id, created_at DESC);
