package dev.proofcode.control.task;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.outbox.OutboxEntity;
import dev.proofcode.control.outbox.OutboxRepository;
import dev.proofcode.control.project.ProjectEntity;
import dev.proofcode.control.project.ProjectRepository;
import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.UUID;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.http.HttpStatus;
import org.springframework.web.server.ResponseStatusException;

@Service
public class TaskService {
    private final TaskRepository tasks; private final ProjectRepository projects; private final OutboxRepository outbox; private final TaskApprovalRepository approvals; private final ObjectMapper json; private final String queue;
    public TaskService(TaskRepository tasks,ProjectRepository projects,OutboxRepository outbox,TaskApprovalRepository approvals,ObjectMapper json,@Value("${proofcode.queue}")String queue){this.tasks=tasks;this.projects=projects;this.outbox=outbox;this.approvals=approvals;this.json=json;this.queue=queue;}
    @Transactional
    public TaskEntity create(UUID projectId,String prompt,String model,String idempotencyKey){return createWithOutcome(projectId,prompt,model,idempotencyKey).task();}
    @Transactional
    public CreateResult createWithOutcome(UUID projectId,String prompt,String model,String idempotencyKey){
        if(idempotencyKey!=null){
            var existing=tasks.findByProjectIdAndIdempotencyKey(projectId,idempotencyKey);
            if(existing.isPresent()){
                TaskEntity task=existing.get();
                if(!task.getPrompt().equals(prompt)||!task.getModel().equals(model)){
                    throw new ResponseStatusException(HttpStatus.CONFLICT,"idempotency key already belongs to another request");
                }
                return new CreateResult(task,false);
            }
        }
        ProjectEntity project=projects.findById(projectId).orElseThrow();
        Instant now=Instant.now();
        TaskEntity task=new TaskEntity(UUID.randomUUID(),projectId,prompt,model,idempotencyKey,now);
        task.transition(TaskStatus.QUEUED);
        tasks.save(task);
        enqueue(project,task,now,null,null);
        return new CreateResult(task,true);
    }
    public TaskEntity create(UUID projectId,String prompt,String model){return create(projectId,prompt,model,null);}
    @Transactional
    public TaskEntity retry(UUID taskId){TaskEntity task=tasks.lockById(taskId).orElseThrow();if(task.getStatus()!=TaskStatus.FAILED&&task.getStatus()!=TaskStatus.CANCELLED)throw new ResponseStatusException(HttpStatus.CONFLICT,"only failed or cancelled tasks can be retried");task.retry();ProjectEntity project=projects.findById(task.getProjectId()).orElseThrow();enqueue(project,task,Instant.now(),null,null);return task;}
    @Transactional
    public TaskEntity cancel(UUID taskId){
        TaskEntity task=tasks.lockById(taskId).orElseThrow();
        if(task.getStatus()==TaskStatus.SUCCEEDED||task.getStatus()==TaskStatus.FAILED||task.getStatus()==TaskStatus.CANCELLED)return task;
        task.transition(TaskStatus.CANCELLED);
        approvals.findByTaskIdAndAttemptAndStatus(taskId,task.getAttempt(),ApprovalStatus.PENDING)
            .forEach(TaskApprovalEntity::cancel);
        return task;
    }
    @Transactional
    public TaskApprovalEntity decideApproval(UUID taskId,UUID approvalId,boolean approved){
        TaskEntity task=tasks.lockById(taskId).orElseThrow();
        TaskApprovalEntity approval=approvals.lockById(approvalId).orElseThrow();
        if(!taskId.equals(approval.getTaskId())||approval.getAttempt()!=task.getAttempt())throw new ResponseStatusException(HttpStatus.CONFLICT,"approval belongs to an older task attempt");
        if(approval.getStatus()!=ApprovalStatus.PENDING){
            if(approval.getStatus()!=(approved?ApprovalStatus.APPROVED:ApprovalStatus.DENIED))throw new ResponseStatusException(HttpStatus.CONFLICT,"approval has already been decided");
            return approval;
        }
        if(task.getStatus()!=TaskStatus.WAITING_APPROVAL)throw new ResponseStatusException(HttpStatus.CONFLICT,"task is no longer waiting for approval");
        approval.decide(approved);task.resume();ProjectEntity project=projects.findById(task.getProjectId()).orElseThrow();enqueue(project,task,Instant.now(),approval.getCallId(),approved?"approved":"denied");
        return approval;
    }
    private void enqueue(ProjectEntity project,TaskEntity task,Instant now,String approvalCallId,String decision){
        try{
            Map<String,Object> payload=new LinkedHashMap<>();
            payload.put("version","v1");payload.put("taskId",task.getId());payload.put("projectId",task.getProjectId());payload.put("attempt",task.getAttempt());payload.put("repositoryUrl",project.getRepositoryUrl());payload.put("branch",project.getDefaultBranch());payload.put("prompt",task.getPrompt());payload.put("model",task.getModel());
            if(approvalCallId!=null){payload.put("resume",true);payload.put("approvalId",approvalCallId);payload.put("approvalDecision",decision);}
            outbox.save(new OutboxEntity(UUID.randomUUID(),task.getId(),queue,json.writeValueAsString(payload),now));
        }catch(JsonProcessingException e){throw new IllegalStateException(e);}
    }
    public record CreateResult(TaskEntity task,boolean created){}
}
