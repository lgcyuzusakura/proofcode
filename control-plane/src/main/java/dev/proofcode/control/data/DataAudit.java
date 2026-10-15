package dev.proofcode.control.data;
import jakarta.persistence.*;
import java.util.UUID;
import java.time.Instant;
@Entity @Table(name="data_operation_audit")
public class DataAudit {
    @Id UUID id;
    @Column(name="operation_id",nullable=false) UUID operationId;
    @Column(name="project_id",nullable=false) UUID projectId;
    @Column(name="task_id",nullable=false) UUID taskId;
    @Column(nullable=false) int attempt;
    @Column(nullable=false,length=32) String action;
    @Column(nullable=false,length=32) String status;
    @Column(nullable=false,columnDefinition="text") String details;
    @Column(name="created_at",nullable=false) Instant createdAt;
    protected DataAudit(){}
    DataAudit(DataOperation op,String action,String details){id=UUID.randomUUID();operationId=op.id;projectId=op.projectId;taskId=op.taskId;attempt=op.attempt;this.action=action;status=op.status;this.details=details;createdAt=Instant.now();}
    public UUID getOperationId(){return operationId;} public UUID getProjectId(){return projectId;} public UUID getTaskId(){return taskId;}
    public String getAction(){return action;} public String getStatus(){return status;} public String getDetails(){return details;} public Instant getCreatedAt(){return createdAt;}
}
