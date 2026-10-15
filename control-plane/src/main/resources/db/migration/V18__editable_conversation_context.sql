-- Transcript remains intact; the model projection and task snapshot are separate.
ALTER TABLE conversations ADD COLUMN context_version bigint NOT NULL DEFAULT 0;
ALTER TABLE conversations ADD COLUMN context_view text;
ALTER TABLE tasks ADD COLUMN context_snapshot text;
CREATE TABLE context_packs (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    source_conversation_id uuid NOT NULL,
    name varchar(120) NOT NULL,
    manifest text NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT fk_context_pack_scope FOREIGN KEY(project_id,workspace_id,source_conversation_id)
        REFERENCES conversations(project_id,workspace_id,id)
);
CREATE INDEX idx_context_pack_scope ON context_packs(project_id,workspace_id,created_at);
