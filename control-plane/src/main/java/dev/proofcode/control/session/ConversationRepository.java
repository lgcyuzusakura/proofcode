package dev.proofcode.control.session;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;
public interface ConversationRepository extends JpaRepository<ConversationEntity,UUID> {
    List<ConversationEntity> findByProjectIdAndWorkspaceIdOrderByCreatedAtAsc(UUID projectId,UUID workspaceId);
    Optional<ConversationEntity> findByProjectIdAndWorkspaceIdAndDefaultConversationTrue(UUID projectId,UUID workspaceId);
}
