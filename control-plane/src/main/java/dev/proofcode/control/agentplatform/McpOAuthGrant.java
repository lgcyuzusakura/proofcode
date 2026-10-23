package dev.proofcode.control.agentplatform;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

@Entity @Table(name="mcp_oauth_grants")
class McpOAuthGrant {
 @Id UUID id;
 @Column(name="project_id",nullable=false) UUID projectId;
 @Column(name="server_id",nullable=false) UUID serverId;
 @Column(name="server_version",nullable=false) long serverVersion;
 @Column(nullable=false,length=2048) String url;
 @Column(name="state_hash",length=64,unique=true) String stateHash;
 @Column(name="pending_until") Instant pendingUntil;
 @Column(nullable=false,length=24) String status;
 @Column(name="encrypted_configuration",nullable=false,columnDefinition="text") String encryptedConfiguration;
 @Column(name="encrypted_tokens",columnDefinition="text") String encryptedTokens;
 @Column(name="token_expires_at") Instant tokenExpiresAt;
 @Column(name="created_at",nullable=false) Instant createdAt;
 @Version long version;
 protected McpOAuthGrant(){}
}
