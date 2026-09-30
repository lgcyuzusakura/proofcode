package dev.proofcode.control.task;

import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;

public interface TaskArtifactRepository extends JpaRepository<TaskArtifactEntity, UUID> {
    List<TaskArtifactEntity> findByTaskIdOrderByAttemptDescCreatedAtDesc(UUID taskId);
    List<TaskArtifactEntity> findByTaskIdAndAttemptOrderByCreatedAtDesc(UUID taskId, int attempt);
    Optional<TaskArtifactEntity> findByTaskIdAndAttemptAndKind(UUID taskId, int attempt, String kind);
    Optional<TaskArtifactEntity> findFirstByTaskIdAndKindOrderByAttemptDescCreatedAtDesc(UUID taskId, String kind);
    default Optional<TaskArtifactEntity> findByTaskIdAndKind(UUID taskId, String kind) { return findFirstByTaskIdAndKindOrderByAttemptDescCreatedAtDesc(taskId, kind); }
}
