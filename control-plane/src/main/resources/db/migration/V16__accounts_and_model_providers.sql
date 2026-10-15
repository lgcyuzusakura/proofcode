CREATE TABLE accounts (id uuid PRIMARY KEY, username varchar(64) NOT NULL UNIQUE, password_hash varchar(100) NOT NULL, created_at timestamptz NOT NULL);
CREATE TABLE account_sessions (digest varchar(64) PRIMARY KEY, account_id uuid NOT NULL REFERENCES accounts(id), expires_at timestamptz NOT NULL);
CREATE INDEX account_sessions_expiry ON account_sessions(expires_at);
ALTER TABLE projects ADD COLUMN owner_id uuid REFERENCES accounts(id);
CREATE INDEX projects_owner ON projects(owner_id);
CREATE TABLE model_providers (id uuid PRIMARY KEY, owner_id uuid NOT NULL REFERENCES accounts(id), name varchar(120) NOT NULL, base_url varchar(2048) NOT NULL, wire_api varchar(16) NOT NULL, encrypted_key text NOT NULL, models_json text NOT NULL, aliases_json text NOT NULL, catalog_at timestamptz, created_at timestamptz NOT NULL, version bigint NOT NULL DEFAULT 0);
CREATE INDEX model_providers_owner ON model_providers(owner_id);
ALTER TABLE tasks ADD COLUMN provider_id uuid;
ALTER TABLE tasks ADD COLUMN provider_snapshot text;
