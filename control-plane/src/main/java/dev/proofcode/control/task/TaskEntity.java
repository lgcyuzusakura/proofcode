package dev.proofcode.control.task;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;
import dev.proofcode.control.experiment.ExperimentProfile;

@Entity
@Table(name="tasks")
public class TaskEntity {
    @Id private UUID id;
    @Column(name="project_id",nullable=false) private UUID projectId;
    @Column(nullable=false,columnDefinition="text") private String prompt;
    @Column(nullable=false) private String model;
    @Enumerated(EnumType.STRING) @Column(nullable=false) private TaskStatus status;
    @Column(columnDefinition="text") private String result;
    @Column(columnDefinition="text") private String error;
    @Column(name="created_at",nullable=false) private Instant createdAt;
    @Column(name="updated_at",nullable=false) private Instant updatedAt;
    @Version private long version;
    @Column(name="runner_id") private UUID runnerId;
    @Column(name="lease_until") private Instant leaseUntil;
    @Column(name="idempotency_key") private String idempotencyKey;
    @Column(nullable=false) private int attempt;
    @Column(name="workspace_id") private UUID workspaceId;
    @Column(name="conversation_id") private UUID conversationId;
    @Column(name="source_revision",length=64) private String sourceRevision;
    @Column(name="experiment_id") private UUID experimentId;
    @Column(name="experiment_run_id") private UUID experimentRunId;
    @Enumerated(EnumType.STRING) @org.hibernate.annotations.JdbcTypeCode(org.hibernate.type.SqlTypes.VARCHAR) @Column(name="experiment_group",length=1) private ExperimentProfile.Group experimentGroup;
    @Column(name="profile_version",length=80) private String profileVersion;
    @Column(name="max_steps") private Integer maxSteps;
    @Column(name="test_command",columnDefinition="text") private String testCommand;
    @Column private Double temperature;
    @Column(name="execution_mode",nullable=false,length=8) private String executionMode="CODE";
    @Column(name="source_snapshot_id") private UUID sourceSnapshotId;
    @Column(name="result_source_snapshot_id") private UUID resultSourceSnapshotId;
    protected TaskEntity(){}
    public TaskEntity(UUID id,UUID projectId,String prompt,String model,String idempotencyKey,Instant now){this.id=id;this.projectId=projectId;this.prompt=prompt;this.model=model;this.idempotencyKey=idempotencyKey;this.status=TaskStatus.CREATED;this.attempt=1;this.createdAt=now;this.updatedAt=now;}
    public TaskEntity(UUID id,UUID projectId,String prompt,String model,Instant now){this(id,projectId,prompt,model,null,now);}
    public void bindScope(UUID workspaceId,UUID conversationId){this.workspaceId=workspaceId;this.conversationId=conversationId;}
    public void bindSource(String mode,UUID snapshotId){this.executionMode=mode;this.sourceSnapshotId=snapshotId;}
    public String getExecutionMode(){return executionMode;} public UUID getSourceSnapshotId(){return sourceSnapshotId;}
    public UUID getResultSourceSnapshotId(){return resultSourceSnapshotId;} public void bindResultSource(UUID id){this.resultSourceSnapshotId=id;}
    public void configureExecution(String sourceRevision,Integer maxSteps,String testCommand,Double temperature){this.sourceRevision=sourceRevision;this.maxSteps=maxSteps;this.testCommand=testCommand;this.temperature=temperature;}
    public void bindExperiment(UUID experimentId,UUID experimentRunId,ExperimentProfile.Group group){this.experimentId=experimentId;this.experimentRunId=experimentRunId;this.experimentGroup=group;this.profileVersion=ExperimentProfile.VERSION;}
    public void transition(TaskStatus next){if(status==next)return;if(!canTransition(next))throw new IllegalStateException("invalid task transition: "+status+" -> "+next);this.status=next;this.updatedAt=Instant.now();}
    private boolean canTransition(TaskStatus next){return switch(status){case CREATED -> next==TaskStatus.QUEUED||next==TaskStatus.CANCELLED;case QUEUED -> next==TaskStatus.RUNNING||next==TaskStatus.CANCELLED;case RUNNING -> next==TaskStatus.WAITING_APPROVAL||next==TaskStatus.VERIFYING||next==TaskStatus.SUCCEEDED||next==TaskStatus.FAILED||next==TaskStatus.CANCELLED;case WAITING_APPROVAL -> next==TaskStatus.RUNNING||next==TaskStatus.QUEUED||next==TaskStatus.FAILED||next==TaskStatus.CANCELLED;case VERIFYING -> next==TaskStatus.RUNNING||next==TaskStatus.SUCCEEDED||next==TaskStatus.FAILED||next==TaskStatus.CANCELLED;case SUCCEEDED -> false;case FAILED,CANCELLED -> next==TaskStatus.QUEUED;};}
    public void complete(String result){if(status==TaskStatus.SUCCEEDED)return;this.result=result;this.error=null;transition(TaskStatus.SUCCEEDED);} public void fail(String error){if(status==TaskStatus.FAILED)return;this.error=error;transition(TaskStatus.FAILED);}
    public void retry(){if(status!=TaskStatus.FAILED&&status!=TaskStatus.CANCELLED)throw new IllegalStateException("only failed or cancelled tasks can be retried");this.result=null;this.error=null;this.resultSourceSnapshotId=null;this.runnerId=null;this.leaseUntil=null;this.attempt=Math.addExact(this.attempt,1);transition(TaskStatus.QUEUED);}
    public void resume(){if(status!=TaskStatus.WAITING_APPROVAL)throw new IllegalStateException("only tasks waiting for approval can be resumed");this.error=null;this.runnerId=null;this.leaseUntil=null;transition(TaskStatus.QUEUED);}
    public boolean claim(UUID runner,Instant now){if(status!=TaskStatus.QUEUED && !((status==TaskStatus.RUNNING||status==TaskStatus.VERIFYING) && (leaseUntil==null || leaseUntil.isBefore(now))))return false;runnerId=runner;leaseUntil=now.plusSeconds(45);transition(TaskStatus.RUNNING);return true;}
    public boolean renew(UUID runner,Instant now){if((status!=TaskStatus.RUNNING && status!=TaskStatus.VERIFYING) || !runner.equals(runnerId))return false;leaseUntil=now.plusSeconds(45);return true;}
    public boolean acceptsEvent(String type,Instant now){
        if((status==TaskStatus.FAILED||status==TaskStatus.CANCELLED)&&"recovery.created".equals(type))return true;
        return (status==TaskStatus.RUNNING||status==TaskStatus.VERIFYING)
            && leaseUntil!=null&&leaseUntil.isAfter(now);
    }
    public UUID getId(){return id;} public UUID getProjectId(){return projectId;} public String getPrompt(){return prompt;} public String getModel(){return model;} public TaskStatus getStatus(){return status;} public String getResult(){return result;} public String getError(){return error;} public Instant getCreatedAt(){return createdAt;} public Instant getUpdatedAt(){return updatedAt;} public long getVersion(){return version;}
    public UUID getRunnerId(){return runnerId;} public Instant getLeaseUntil(){return leaseUntil;} public String getIdempotencyKey(){return idempotencyKey;} public int getAttempt(){return attempt;}
    public UUID getWorkspaceId(){return workspaceId;} public UUID getConversationId(){return conversationId;} public String getSourceRevision(){return sourceRevision;}
    public UUID getExperimentId(){return experimentId;} public UUID getExperimentRunId(){return experimentRunId;} public ExperimentProfile.Group getExperimentGroup(){return experimentGroup;} public String getProfileVersion(){return profileVersion;}
    public Integer getMaxSteps(){return maxSteps;} public String getTestCommand(){return testCommand;} public Double getTemperature(){return temperature;}
}
