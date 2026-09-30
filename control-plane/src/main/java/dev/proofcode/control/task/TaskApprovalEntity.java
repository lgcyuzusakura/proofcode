package dev.proofcode.control.task;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.time.Instant;
import java.util.UUID;
import com.fasterxml.jackson.annotation.JsonRawValue;
import org.hibernate.annotations.JdbcTypeCode;
import org.hibernate.type.SqlTypes;

@Entity
@Table(name = "task_approvals")
public class TaskApprovalEntity {
    @Id private UUID id;
    @Column(name = "task_id", nullable = false) private UUID taskId;
    @Column(nullable = false) private int attempt;
    @Column(name = "call_id", nullable = false) private String callId;
    @Column(nullable = false) private String tool;
    @Column(nullable = false) private String risk;
    @JdbcTypeCode(SqlTypes.JSON) @Column(nullable = false, columnDefinition = "jsonb") private String arguments;
    @Enumerated(EnumType.STRING) @Column(nullable = false) private ApprovalStatus status;
    @Column private String decision;
    @Column(name = "created_at", nullable = false) private Instant createdAt;
    @Column(name = "updated_at", nullable = false) private Instant updatedAt;

    protected TaskApprovalEntity() {}

    public TaskApprovalEntity(UUID id, UUID taskId, String callId, String tool, String risk, String arguments, Instant now) {
        this(id, taskId, 1, callId, tool, risk, arguments, now);
    }

    public TaskApprovalEntity(UUID id, UUID taskId, int attempt, String callId, String tool, String risk, String arguments, Instant now) {
        this.id = id;
        this.taskId = taskId;
        this.attempt = attempt;
        this.callId = callId;
        this.tool = tool;
        this.risk = risk;
        this.arguments = arguments == null ? "{}" : arguments;
        this.status = ApprovalStatus.PENDING;
        this.createdAt = now;
        this.updatedAt = now;
    }

    public void decide(boolean approved) {
        if (status != ApprovalStatus.PENDING) return;
        status = approved ? ApprovalStatus.APPROVED : ApprovalStatus.DENIED;
        decision = approved ? "approved" : "denied";
        updatedAt = Instant.now();
    }

    public void cancel() {
        if (status != ApprovalStatus.PENDING) return;
        status = ApprovalStatus.CANCELLED;
        decision = "cancelled";
        updatedAt = Instant.now();
    }

    public UUID getId() { return id; }
    public UUID getTaskId() { return taskId; }
    public int getAttempt() { return attempt; }
    public String getCallId() { return callId; }
    public String getTool() { return tool; }
    public String getRisk() { return risk; }
    @JsonRawValue public String getArguments() { return arguments; }
    public ApprovalStatus getStatus() { return status; }
    public String getDecision() { return decision; }
    public Instant getCreatedAt() { return createdAt; }
    public Instant getUpdatedAt() { return updatedAt; }
}
