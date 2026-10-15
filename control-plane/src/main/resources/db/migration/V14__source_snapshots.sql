CREATE TABLE source_snapshots (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects(id),
    workspace_id uuid NOT NULL,
    manifest_hash varchar(64) NOT NULL,
    file_count integer NOT NULL,
    total_bytes bigint NOT NULL,
    content text NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT fk_source_workspace FOREIGN KEY(project_id,workspace_id) REFERENCES project_workspaces(project_id,id),
    CONSTRAINT uq_source_scope UNIQUE(project_id,workspace_id,manifest_hash),
    CONSTRAINT uq_source_project_id UNIQUE(project_id,id)
);
ALTER TABLE tasks ADD COLUMN source_snapshot_id uuid;
ALTER TABLE tasks ADD COLUMN result_source_snapshot_id uuid;
ALTER TABLE tasks ADD COLUMN execution_mode varchar(8) NOT NULL DEFAULT 'CODE' CHECK(execution_mode IN ('CODE','CHAT'));
ALTER TABLE tasks ADD CONSTRAINT fk_task_source FOREIGN KEY(project_id,source_snapshot_id) REFERENCES source_snapshots(project_id,id);
ALTER TABLE tasks ADD CONSTRAINT fk_task_result_source FOREIGN KEY(project_id,result_source_snapshot_id) REFERENCES source_snapshots(project_id,id);
