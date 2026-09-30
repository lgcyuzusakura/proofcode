ALTER TABLE tasks ADD COLUMN attempt integer NOT NULL DEFAULT 1;

ALTER TABLE task_events ADD COLUMN attempt integer NOT NULL DEFAULT 1;

ALTER TABLE task_artifacts ADD COLUMN attempt integer NOT NULL DEFAULT 1;
ALTER TABLE task_artifacts DROP CONSTRAINT uq_task_artifact_kind;
ALTER TABLE task_artifacts ADD CONSTRAINT uq_task_artifact_attempt_kind UNIQUE(task_id, attempt, kind);

ALTER TABLE task_approvals ADD COLUMN attempt integer NOT NULL DEFAULT 1;
ALTER TABLE task_approvals DROP CONSTRAINT uq_task_approval_call;
ALTER TABLE task_approvals ADD CONSTRAINT uq_task_approval_attempt_call UNIQUE(task_id, attempt, call_id);
