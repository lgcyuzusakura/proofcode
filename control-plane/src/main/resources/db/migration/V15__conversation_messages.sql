CREATE TABLE conversation_messages (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    sequence bigint NOT NULL,
    role varchar(12) NOT NULL CHECK(role IN ('user','assistant')),
    content text NOT NULL,
    task_id uuid NOT NULL REFERENCES tasks(id),
    attempt integer NOT NULL,
    status varchar(24) NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT fk_message_scope FOREIGN KEY(project_id,workspace_id,conversation_id) REFERENCES conversations(project_id,workspace_id,id),
    CONSTRAINT uq_message_turn UNIQUE(task_id,attempt,role),
    CONSTRAINT uq_message_sequence UNIQUE(conversation_id,sequence)
);
CREATE INDEX idx_message_scope_sequence ON conversation_messages(conversation_id,sequence);
-- Preserve existing user and final assistant messages as ordered turns.
INSERT INTO conversation_messages(id,project_id,workspace_id,conversation_id,sequence,role,content,task_id,attempt,status,created_at)
SELECT id,project_id,workspace_id,conversation_id,2*row_number() OVER(PARTITION BY conversation_id ORDER BY created_at,id)-1,'user',prompt,id,attempt,'COMPLETED',created_at FROM tasks;
INSERT INTO conversation_messages(id,project_id,workspace_id,conversation_id,sequence,role,content,task_id,attempt,status,created_at)
SELECT md5(id::text||':assistant')::uuid,project_id,workspace_id,conversation_id,2*row_number() OVER(PARTITION BY conversation_id ORDER BY created_at,id),'assistant',coalesce(result,''),id,attempt,status,created_at FROM tasks;
