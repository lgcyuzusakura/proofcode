CREATE TABLE account_preferences (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    version BIGINT NOT NULL DEFAULT 0
);
