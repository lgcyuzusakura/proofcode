ALTER TABLE tasks ADD COLUMN IF NOT EXISTS idempotency_key varchar(200);
CREATE UNIQUE INDEX IF NOT EXISTS uq_tasks_project_idempotency ON tasks(project_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
