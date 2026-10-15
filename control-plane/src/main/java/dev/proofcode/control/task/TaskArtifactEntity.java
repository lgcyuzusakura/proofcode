package dev.proofcode.control.task;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.time.Instant;
import java.util.UUID;
import com.fasterxml.jackson.annotation.JsonRawValue;
import org.hibernate.annotations.JdbcTypeCode;
import org.hibernate.type.SqlTypes;

@Entity
@Table(name = "task_artifacts")
public class TaskArtifactEntity {
    @Id private UUID id;
    @Column(name = "task_id", nullable = false) private UUID taskId;
    @Column(nullable = false) private int attempt;
    @Column(nullable = false) private String kind;
    @Column(name = "commit_hash") private String commitHash;
    @Column private String branch;
    @Column(columnDefinition = "text") private String patch;
    @JdbcTypeCode(SqlTypes.JSON) @Column(nullable = false, columnDefinition = "jsonb") private String metadata;
    @Column(name = "created_at", nullable = false) private Instant createdAt;

    protected TaskArtifactEntity() {}

    public TaskArtifactEntity(UUID id, UUID taskId, String kind, String commitHash, String branch, String patch, String metadata, Instant createdAt) {
        this(id, taskId, 1, kind, commitHash, branch, patch, metadata, createdAt);
    }

    public TaskArtifactEntity(UUID id, UUID taskId, int attempt, String kind, String commitHash, String branch, String patch, String metadata, Instant createdAt) {
        this.id = id;
        this.taskId = taskId;
        this.attempt = attempt;
        this.kind = kind;
        this.commitHash = commitHash;
        this.branch = branch;
        this.patch = patch;
        this.metadata = metadata == null ? "{}" : metadata;
        this.createdAt = createdAt;
    }

    public UUID getId() { return id; }
    public UUID getTaskId() { return taskId; }
    public int getAttempt() { return attempt; }
    public String getKind() { return kind; }
    public String getCommitHash() { return commitHash; }
    public String getBranch() { return branch; }
    public String getPatch() { return patch; }
    @JsonRawValue public String getMetadata() { return metadata; }
    public Instant getCreatedAt() { return createdAt; }
}
