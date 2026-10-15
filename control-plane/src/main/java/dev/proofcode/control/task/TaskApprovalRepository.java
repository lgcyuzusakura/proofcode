package dev.proofcode.control.task;

import java.util.List;
import java.util.Optional;
import java.util.UUID;
import jakarta.persistence.LockModeType;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

public interface TaskApprovalRepository extends JpaRepository<TaskApprovalEntity, UUID> {
    List<TaskApprovalEntity> findByTaskIdOrderByAttemptDescCreatedAtDesc(UUID taskId);
    List<TaskApprovalEntity> findByTaskIdAndAttemptOrderByCreatedAtDesc(UUID taskId, int attempt);
    List<TaskApprovalEntity> findByTaskIdAndAttemptAndStatus(UUID taskId, int attempt, ApprovalStatus status);
    Optional<TaskApprovalEntity> findByTaskIdAndAttemptAndCallId(UUID taskId, int attempt, String callId);
    Optional<TaskApprovalEntity> findFirstByTaskIdAndCallIdOrderByAttemptDescCreatedAtDesc(UUID taskId, String callId);
    default Optional<TaskApprovalEntity> findByTaskIdAndCallId(UUID taskId, String callId) { return findFirstByTaskIdAndCallIdOrderByAttemptDescCreatedAtDesc(taskId, callId); }
    @Lock(LockModeType.PESSIMISTIC_WRITE)
    @Query("select a from TaskApprovalEntity a where a.id = :id")
    Optional<TaskApprovalEntity> lockById(@Param("id") UUID id);
}
