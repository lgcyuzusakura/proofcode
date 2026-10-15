ALTER TABLE task_events ADD COLUMN event_key varchar(255);
CREATE UNIQUE INDEX uq_task_event_key ON task_events(task_id, event_key) WHERE event_key IS NOT NULL;
