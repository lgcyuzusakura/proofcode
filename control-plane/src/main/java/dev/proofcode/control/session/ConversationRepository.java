package dev.proofcode.control.session;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;
public interface ConversationRepository extends JpaRepository<ConversationEntity,UUID> {
    @org.springframework.data.jpa.repository.Lock(jakarta.persistence.LockModeType.PESSIMISTIC_WRITE)
    @org.springframework.data.jpa.repository.Query("select c from ConversationEntity c where c.id=:id")
    Optional<ConversationEntity> lockById(@org.springframework.data.repository.query.Param("id") UUID id);
    List<ConversationEntity> findByProjectIdAndWorkspaceIdOrderByCreatedAtAsc(UUID projectId,UUID workspaceId);
    Optional<ConversationEntity> findByProjectIdAndWorkspaceIdAndDefaultConversationTrue(UUID projectId,UUID workspaceId);
}
