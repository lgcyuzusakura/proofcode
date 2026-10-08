package dev.proofcode.control.session;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;
public interface WorkspaceRepository extends JpaRepository<WorkspaceEntity,UUID> {
    List<WorkspaceEntity> findByProjectIdOrderByCreatedAtAsc(UUID projectId);
    Optional<WorkspaceEntity> findByProjectIdAndDefaultWorkspaceTrue(UUID projectId);
}
