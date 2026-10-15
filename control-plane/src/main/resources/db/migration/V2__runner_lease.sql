ALTER TABLE tasks ADD COLUMN runner_id uuid;
ALTER TABLE tasks ADD COLUMN lease_until timestamptz;
