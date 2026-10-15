ALTER TABLE projects ALTER COLUMN repository_url DROP NOT NULL;
ALTER TABLE projects ADD COLUMN source_kind varchar(24) NOT NULL DEFAULT 'REMOTE_REPOSITORY';
ALTER TABLE projects ADD COLUMN local_handle varchar(200);
ALTER TABLE projects ADD COLUMN bootstrap_id varchar(120);
ALTER TABLE projects ADD CONSTRAINT ck_project_source_kind CHECK (source_kind IN ('REMOTE_REPOSITORY', 'LOCAL_FOLDER', 'SCRATCH'));
ALTER TABLE projects ADD CONSTRAINT ck_project_source_repository CHECK (
    (source_kind = 'REMOTE_REPOSITORY' AND repository_url IS NOT NULL)
    OR (source_kind IN ('LOCAL_FOLDER', 'SCRATCH'))
);
CREATE UNIQUE INDEX uq_project_bootstrap_id ON projects(bootstrap_id) WHERE bootstrap_id IS NOT NULL;
CREATE INDEX ix_project_source_kind ON projects(source_kind);
