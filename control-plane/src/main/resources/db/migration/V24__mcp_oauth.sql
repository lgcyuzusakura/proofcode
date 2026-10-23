CREATE TABLE mcp_oauth_grants (
 id UUID PRIMARY KEY,
 project_id UUID NOT NULL REFERENCES projects(id),
 server_id UUID NOT NULL,
 server_version BIGINT NOT NULL,
 url VARCHAR(2048) NOT NULL,
 state_hash VARCHAR(64) UNIQUE,
 pending_until TIMESTAMP WITH TIME ZONE,
 status VARCHAR(24) NOT NULL,
 encrypted_configuration TEXT NOT NULL,
 encrypted_tokens TEXT,
 token_expires_at TIMESTAMP WITH TIME ZONE,
 created_at TIMESTAMP WITH TIME ZONE NOT NULL,
 version BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX mcp_oauth_server_version ON mcp_oauth_grants(server_id, server_version, created_at);
