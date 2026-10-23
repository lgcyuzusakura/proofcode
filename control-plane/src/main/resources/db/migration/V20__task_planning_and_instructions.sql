ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_execution_mode_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_execution_mode_check CHECK (execution_mode IN ('CODE','CHAT','PLAN'));
CREATE TABLE task_instructions (
 id UUID PRIMARY KEY,
 task_id UUID NOT NULL REFERENCES tasks(id),
 attempt INTEGER NOT NULL,
 sequence BIGINT NOT NULL,
 content TEXT NOT NULL,
 client_key VARCHAR(80) NOT NULL,
 created_at TIMESTAMP WITH TIME ZONE NOT NULL,
 delivered_at TIMESTAMP WITH TIME ZONE,
 UNIQUE(task_id,attempt,sequence),
 UNIQUE(task_id,attempt,client_key)
);
