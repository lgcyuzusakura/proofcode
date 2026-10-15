CREATE TABLE project_workspaces (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects(id),
    name varchar(120) NOT NULL,
    kind varchar(30) NOT NULL CHECK (kind IN ('REMOTE_REPOSITORY', 'LOCAL_FOLDER', 'SCRATCH')),
    is_default boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    CONSTRAINT uq_workspace_project_id UNIQUE (project_id, id)
);
CREATE UNIQUE INDEX uq_workspace_project_default ON project_workspaces(project_id) WHERE is_default;
CREATE INDEX idx_workspace_project_created ON project_workspaces(project_id, created_at);

CREATE TABLE conversations (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects(id),
    workspace_id uuid NOT NULL,
    title varchar(200) NOT NULL,
    is_default boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    CONSTRAINT fk_conversation_workspace_scope FOREIGN KEY (project_id, workspace_id) REFERENCES project_workspaces(project_id, id),
    CONSTRAINT uq_conversation_scope_id UNIQUE (project_id, workspace_id, id)
);
CREATE UNIQUE INDEX uq_conversation_workspace_default ON conversations(project_id, workspace_id) WHERE is_default;
CREATE INDEX idx_conversation_workspace_created ON conversations(project_id, workspace_id, created_at);

-- The existing project UUID provides a deterministic legacy scope; no UUID extension is needed.
INSERT INTO project_workspaces(id, project_id, name, kind, is_default, created_at)
SELECT id, id, 'Legacy', 'REMOTE_REPOSITORY', true, created_at FROM projects;
INSERT INTO conversations(id, project_id, workspace_id, title, is_default, created_at)
SELECT id, id, id, 'Legacy', true, created_at FROM projects;

CREATE TABLE experiments (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects(id),
    workspace_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    name varchar(160) NOT NULL,
    prompt text NOT NULL,
    model varchar(200) NOT NULL,
    repository_url text NOT NULL,
    source_revision varchar(64) NOT NULL CHECK (source_revision ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
    max_steps integer NOT NULL CHECK (max_steps BETWEEN 1 AND 100),
    test_command text NOT NULL,
    temperature double precision NOT NULL CHECK (temperature BETWEEN 0 AND 2),
    repetitions integer NOT NULL CHECK (repetitions BETWEEN 1 AND 20),
    profile_version varchar(80) NOT NULL,
    idempotency_key varchar(200),
    request_hash varchar(64) NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT fk_experiment_scope FOREIGN KEY (project_id, workspace_id, conversation_id) REFERENCES conversations(project_id, workspace_id, id),
    CONSTRAINT uq_experiment_project_idempotency UNIQUE (project_id, idempotency_key),
    CONSTRAINT uq_experiment_project_id UNIQUE (project_id, id)
);
CREATE INDEX idx_experiment_project_created ON experiments(project_id, created_at DESC);

ALTER TABLE tasks ADD COLUMN workspace_id uuid;
ALTER TABLE tasks ADD COLUMN conversation_id uuid;
ALTER TABLE tasks ADD COLUMN source_revision varchar(64);
ALTER TABLE tasks ADD COLUMN experiment_id uuid;
ALTER TABLE tasks ADD COLUMN experiment_run_id uuid;
ALTER TABLE tasks ADD COLUMN experiment_group varchar(1);
ALTER TABLE tasks ADD COLUMN profile_version varchar(80);
ALTER TABLE tasks ADD COLUMN max_steps integer;
ALTER TABLE tasks ADD COLUMN test_command text;
ALTER TABLE tasks ADD COLUMN temperature double precision;
UPDATE tasks SET workspace_id = project_id, conversation_id = project_id;
ALTER TABLE tasks ALTER COLUMN workspace_id SET NOT NULL;
ALTER TABLE tasks ALTER COLUMN conversation_id SET NOT NULL;
ALTER TABLE tasks ADD CONSTRAINT fk_task_scope FOREIGN KEY (project_id, workspace_id, conversation_id) REFERENCES conversations(project_id, workspace_id, id);
ALTER TABLE tasks ADD CONSTRAINT fk_task_experiment_scope FOREIGN KEY (project_id, experiment_id) REFERENCES experiments(project_id, id);
ALTER TABLE tasks ADD CONSTRAINT ck_task_source_revision CHECK (source_revision IS NULL OR source_revision ~* '^([0-9a-f]{40}|[0-9a-f]{64})$');
ALTER TABLE tasks ADD CONSTRAINT ck_task_max_steps CHECK (max_steps IS NULL OR max_steps BETWEEN 1 AND 100);
ALTER TABLE tasks ADD CONSTRAINT ck_task_temperature CHECK (temperature IS NULL OR temperature BETWEEN 0 AND 2);
ALTER TABLE tasks ADD CONSTRAINT ck_task_experiment_group CHECK (experiment_group IS NULL OR experiment_group IN ('A','B','C','D','E','F'));
ALTER TABLE tasks ADD CONSTRAINT ck_task_experiment_identity CHECK (
    (experiment_id IS NULL AND experiment_run_id IS NULL AND experiment_group IS NULL AND profile_version IS NULL)
    OR (experiment_id IS NOT NULL AND experiment_run_id IS NOT NULL AND experiment_group IS NOT NULL AND profile_version IS NOT NULL)
);
ALTER TABLE tasks ADD CONSTRAINT uq_task_project_id UNIQUE (project_id, id);
CREATE UNIQUE INDEX uq_task_experiment_run ON tasks(experiment_run_id) WHERE experiment_run_id IS NOT NULL;
CREATE INDEX idx_task_conversation_created ON tasks(project_id, conversation_id, created_at DESC);
CREATE INDEX idx_task_experiment ON tasks(experiment_id);

CREATE TABLE experiment_runs (
    id uuid PRIMARY KEY,
    experiment_id uuid NOT NULL,
    project_id uuid NOT NULL,
    task_id uuid NOT NULL,
    experiment_group varchar(1) NOT NULL CHECK (experiment_group IN ('A','B','C','D','E','F')),
    repetition integer NOT NULL CHECK (repetition BETWEEN 1 AND 20),
    created_at timestamptz NOT NULL,
    CONSTRAINT fk_experiment_run_scope FOREIGN KEY (project_id, experiment_id) REFERENCES experiments(project_id, id),
    CONSTRAINT fk_experiment_run_task_scope FOREIGN KEY (project_id, task_id) REFERENCES tasks(project_id, id),
    CONSTRAINT uq_experiment_group_repetition UNIQUE (experiment_id, experiment_group, repetition),
    CONSTRAINT uq_experiment_run_task UNIQUE (task_id),
    CONSTRAINT uq_experiment_run_identity UNIQUE (project_id, experiment_id, id, task_id, experiment_group)
);
ALTER TABLE tasks ADD CONSTRAINT fk_task_experiment_run FOREIGN KEY (project_id, experiment_id, experiment_run_id, id, experiment_group)
    REFERENCES experiment_runs(project_id, experiment_id, id, task_id, experiment_group) DEFERRABLE INITIALLY DEFERRED;
