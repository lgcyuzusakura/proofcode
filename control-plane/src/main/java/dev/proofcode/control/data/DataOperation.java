package dev.proofcode.control.data;
import jakarta.persistence.*;
import java.util.UUID;
import java.time.Instant;
import com.fasterxml.jackson.annotation.JsonIgnore;
@Entity @Table(name="data_operation_plans")
public class DataOperation {
    @Id UUID id;
    @Column(name="task_id") UUID taskId;
    @Column(name="data_session_id") UUID dataSessionId;
    @Column(name="project_id",nullable=false) UUID projectId;
    @Column(nullable=false) int attempt;
    @Column(name="connection_id",nullable=false) UUID connectionId;
    @Column(name="resource_version",nullable=false) long resourceVersion;
    @Column(name="policy_version",nullable=false,length=40) String policyVersion;
    @Column(nullable=false,length=32) String kind;
    @JsonIgnore @Column(name="canonical_ir",nullable=false,columnDefinition="text") String canonicalIr;
    @Column(nullable=false,length=64) String digest;
    @Column(name="schema_version",nullable=false,length=64) String schemaVersion;
    @Column(nullable=false,columnDefinition="text") String preview;
    @Column(nullable=false,length=32) String status="PENDING";
    @JsonIgnore @Column(name="capability_hash",length=64) String capabilityHash;
    @JsonIgnore @Column(name="capability_expires_at") Instant capabilityExpiresAt;
    @JsonIgnore @Column(name="capability_used_at") Instant capabilityUsedAt;
    @Column(name="idempotency_key",nullable=false,length=200) String idempotencyKey;
    @Column(name="result_summary",columnDefinition="text") String resultSummary;
    @JsonIgnore @Column(name="private_snapshot",columnDefinition="text") String privateSnapshot;
    @Column(name="created_at",nullable=false) Instant createdAt;
    @Column(name="updated_at",nullable=false) Instant updatedAt;
    @Version long version;
    protected DataOperation(){}
    DataOperation(UUID task,UUID project,int attempt,DataResource resource,String ir,String digest,String schemaVersion,String preview,String key,String kind){
        id=UUID.randomUUID();taskId=task;projectId=project;this.attempt=attempt;connectionId=resource.id;resourceVersion=resource.resourceVersion;policyVersion=resource.policyVersion;canonicalIr=ir;this.digest=digest;this.schemaVersion=schemaVersion;this.preview=preview;idempotencyKey=key;this.kind=kind;createdAt=updatedAt=Instant.now();
    }
    public UUID getId(){return id;} public UUID getTaskId(){return taskId;} public UUID getProjectId(){return projectId;}
    public int getAttempt(){return attempt;} public UUID getConnectionId(){return connectionId;}
    public UUID getDataSessionId(){return dataSessionId;} public long getResourceVersion(){return resourceVersion;} public String getPolicyVersion(){return policyVersion;}
    public String getDigest(){return digest;} public String getStatus(){return status;} public String getPreview(){return preview;}
    public String getSchemaVersion(){return schemaVersion;} public String getKind(){return kind;}
    /** Caller-authored intent is reviewable; fetched database rows and private recovery snapshots never appear here. */
    public com.fasterxml.jackson.databind.JsonNode getIr(){try{return new com.fasterxml.jackson.databind.ObjectMapper().readTree(canonicalIr);}catch(Exception e){throw new IllegalStateException("stored operation IR is invalid");}}
    public String getResultSummary(){return resultSummary;} public Instant getCreatedAt(){return createdAt;}
}
