CREATE TABLE task_approvals (
    id uuid PRIMARY KEY,
    task_id uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    call_id varchar(200) NOT NULL,
    tool varchar(120) NOT NULL,
    risk varchar(40) NOT NULL,
    arguments jsonb NOT NULL,
    status varchar(30) NOT NULL,
    decision varchar(30),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT uq_task_approval_call UNIQUE(task_id, call_id)
);
CREATE INDEX idx_task_approvals_task_status ON task_approvals(task_id, status, created_at DESC);
