CREATE TABLE agent_resources (
 id UUID PRIMARY KEY,
 project_id UUID NOT NULL REFERENCES projects(id),
 kind VARCHAR(16) NOT NULL,
 name VARCHAR(160) NOT NULL,
 body TEXT NOT NULL,
 version BIGINT NOT NULL DEFAULT 0,
 UNIQUE(project_id, kind, name)
);
ALTER TABLE tasks ADD COLUMN agent_snapshot TEXT;
