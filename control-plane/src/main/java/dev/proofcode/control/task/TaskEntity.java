package dev.proofcode.control.task;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

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
    protected TaskEntity(){}
    public TaskEntity(UUID id,UUID projectId,String prompt,String model,String idempotencyKey,Instant now){this.id=id;this.projectId=projectId;this.prompt=prompt;this.model=model;this.idempotencyKey=idempotencyKey;this.status=TaskStatus.CREATED;this.attempt=1;this.createdAt=now;this.updatedAt=now;}
    public TaskEntity(UUID id,UUID projectId,String prompt,String model,Instant now){this(id,projectId,prompt,model,null,now);}
    public void transition(TaskStatus next){if(status==next)return;if(!canTransition(next))throw new IllegalStateException("invalid task transition: "+status+" -> "+next);this.status=next;this.updatedAt=Instant.now();}
    private boolean canTransition(TaskStatus next){return switch(status){case CREATED -> next==TaskStatus.QUEUED||next==TaskStatus.CANCELLED;case QUEUED -> next==TaskStatus.RUNNING||next==TaskStatus.CANCELLED;case RUNNING -> next==TaskStatus.WAITING_APPROVAL||next==TaskStatus.VERIFYING||next==TaskStatus.SUCCEEDED||next==TaskStatus.FAILED||next==TaskStatus.CANCELLED;case WAITING_APPROVAL -> next==TaskStatus.RUNNING||next==TaskStatus.QUEUED||next==TaskStatus.FAILED||next==TaskStatus.CANCELLED;case VERIFYING -> next==TaskStatus.RUNNING||next==TaskStatus.SUCCEEDED||next==TaskStatus.FAILED||next==TaskStatus.CANCELLED;case SUCCEEDED -> false;case FAILED,CANCELLED -> next==TaskStatus.QUEUED;};}
    public void complete(String result){if(status==TaskStatus.SUCCEEDED)return;this.result=result;this.error=null;transition(TaskStatus.SUCCEEDED);} public void fail(String error){if(status==TaskStatus.FAILED)return;this.error=error;transition(TaskStatus.FAILED);}
    public void retry(){if(status!=TaskStatus.FAILED&&status!=TaskStatus.CANCELLED)throw new IllegalStateException("only failed or cancelled tasks can be retried");this.result=null;this.error=null;this.runnerId=null;this.leaseUntil=null;this.attempt=Math.addExact(this.attempt,1);transition(TaskStatus.QUEUED);}
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
}
