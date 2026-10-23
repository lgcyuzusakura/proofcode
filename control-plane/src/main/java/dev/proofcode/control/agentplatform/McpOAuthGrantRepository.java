package dev.proofcode.control.agentplatform;

import jakarta.persistence.LockModeType;
import java.time.Instant;
import java.util.*;
import org.springframework.data.jpa.repository.*;

interface McpOAuthGrantRepository extends JpaRepository<McpOAuthGrant,UUID> {
 Optional<McpOAuthGrant> findFirstByServerIdAndServerVersionOrderByCreatedAtDesc(UUID serverId,long serverVersion);
 Optional<McpOAuthGrant> findFirstByServerIdAndServerVersionAndStatusOrderByCreatedAtDesc(UUID serverId,long serverVersion,String status);
 Optional<McpOAuthGrant> findByStateHash(String stateHash);
 @Modifying(clearAutomatically=true,flushAutomatically=true)
 @Query("update McpOAuthGrant g set g.stateHash = null, g.status = :nextStatus, g.pendingUntil = :exchangeUntil, g.version = g.version + 1 where g.id = :id and g.stateHash = :hash and g.status = 'PENDING' and g.pendingUntil > :now")
 int claim(UUID id,String hash,Instant now,Instant exchangeUntil,String nextStatus);
 @Lock(LockModeType.PESSIMISTIC_WRITE) @Query("select g from McpOAuthGrant g where g.id = :id") Optional<McpOAuthGrant> locked(UUID id);
 @Modifying @Query("update McpOAuthGrant g set g.stateHash = null, g.encryptedConfiguration = '', g.status = 'EXPIRED', g.version = g.version + 1 where g.pendingUntil < :now and g.status in ('PENDING','EXCHANGING')") int expire(Instant now);
}
