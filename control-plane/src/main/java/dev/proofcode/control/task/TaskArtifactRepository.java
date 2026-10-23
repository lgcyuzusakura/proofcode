package dev.proofcode.control.task;

import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;

public interface TaskArtifactRepository extends JpaRepository<TaskArtifactEntity, UUID> {
    interface Metadata { UUID getId();UUID getTaskId();int getAttempt();String getKind();String getCommitHash();String getBranch();java.time.Instant getCreatedAt(); }
    @org.springframework.data.jpa.repository.Query("select a.id as id, a.taskId as taskId, a.attempt as attempt, a.kind as kind, a.commitHash as commitHash, a.branch as branch, a.createdAt as createdAt from TaskArtifactEntity a where a.taskId=:taskId and a.attempt in :attempts and a.kind in ('checkpoint','recovery') order by a.attempt desc, a.createdAt desc")
    List<Metadata> historyMetadata(UUID taskId,List<Integer> attempts);
    List<TaskArtifactEntity> findByTaskIdOrderByAttemptDescCreatedAtDesc(UUID taskId);
    List<TaskArtifactEntity> findByTaskIdAndAttemptOrderByCreatedAtDesc(UUID taskId, int attempt);
    Optional<TaskArtifactEntity> findByTaskIdAndAttemptAndKind(UUID taskId, int attempt, String kind);
    Optional<TaskArtifactEntity> findFirstByTaskIdAndKindOrderByAttemptDescCreatedAtDesc(UUID taskId, String kind);
    default Optional<TaskArtifactEntity> findByTaskIdAndKind(UUID taskId, String kind) { return findFirstByTaskIdAndKindOrderByAttemptDescCreatedAtDesc(taskId, kind); }
}
