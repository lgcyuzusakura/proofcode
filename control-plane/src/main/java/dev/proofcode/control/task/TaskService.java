package dev.proofcode.control.task;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.outbox.OutboxEntity;
import dev.proofcode.control.outbox.OutboxRepository;
import dev.proofcode.control.project.ProjectEntity;
import dev.proofcode.control.project.ProjectRepository;
import dev.proofcode.control.experiment.ExperimentProfile;
import dev.proofcode.control.experiment.ExperimentRepository;
import dev.proofcode.control.session.WorkspaceService;
import dev.proofcode.control.data.DataOperationService;
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
    private final TaskRepository tasks; private final ProjectRepository projects; private final OutboxRepository outbox; private final TaskApprovalRepository approvals; private final ObjectMapper json; private final String queue; private final WorkspaceService scopes; private final ExperimentRepository experiments; private final DataOperationService dataOperations;
    public TaskService(TaskRepository tasks,ProjectRepository projects,OutboxRepository outbox,TaskApprovalRepository approvals,ObjectMapper json,@Value("${proofcode.queue}")String queue,WorkspaceService scopes,ExperimentRepository experiments,DataOperationService dataOperations){this.tasks=tasks;this.projects=projects;this.outbox=outbox;this.approvals=approvals;this.json=json;this.queue=queue;this.scopes=scopes;this.experiments=experiments;this.dataOperations=dataOperations;}
    @Transactional
    public TaskEntity create(UUID projectId,String prompt,String model,String idempotencyKey){return createWithOutcome(projectId,prompt,model,idempotencyKey).task();}
    @Transactional
    public CreateResult createWithOutcome(UUID projectId,String prompt,String model,String idempotencyKey){
        return createWithOutcome(projectId,prompt,model,idempotencyKey,null,null,null,null,null,null);
    }
    @Transactional
    public CreateResult createWithOutcome(UUID projectId,String prompt,String model,String idempotencyKey,UUID workspaceId,UUID conversationId,String sourceRevision,Integer maxSteps,String testCommand,Double temperature){
        validateExecution(sourceRevision,maxSteps,testCommand,temperature);
        var scope=scopes.resolve(projectId,workspaceId,conversationId);
        if(idempotencyKey!=null){
            var existing=tasks.findByProjectIdAndIdempotencyKey(projectId,idempotencyKey);
            if(existing.isPresent()){
                TaskEntity task=existing.get();
                if(!matches(task,prompt,model,scope.workspaceId(),scope.conversationId(),sourceRevision,maxSteps,testCommand,temperature)){
                    throw new ResponseStatusException(HttpStatus.CONFLICT,"idempotency key already belongs to another request");
                }
                return new CreateResult(task,false);
            }
        }
        ProjectEntity project=projects.findById(projectId).orElseThrow();
        Instant now=Instant.now();
        TaskEntity task=new TaskEntity(UUID.randomUUID(),projectId,prompt,model,idempotencyKey,now);
        task.bindScope(scope.workspaceId(),scope.conversationId());
        task.configureExecution(sourceRevision,maxSteps,testCommand,temperature);
        task.transition(TaskStatus.QUEUED);
        tasks.save(task);
        enqueue(project,task,now,null,null);
        return new CreateResult(task,true);
    }
    @Transactional
    public TaskEntity createExperimentTask(UUID projectId,UUID workspaceId,UUID conversationId,String prompt,String model,String sourceRevision,int maxSteps,String testCommand,double temperature,UUID experimentId,UUID experimentRunId,ExperimentProfile.Group group){
        validateExecution(sourceRevision,maxSteps,testCommand,temperature);
        var scope=scopes.resolve(projectId,workspaceId,conversationId);
        ProjectEntity project=projects.findById(projectId).orElseThrow();
        Instant now=Instant.now();
        TaskEntity task=new TaskEntity(UUID.randomUUID(),projectId,prompt,model,"experiment:"+experimentRunId,now);
        task.bindScope(scope.workspaceId(),scope.conversationId());
        task.configureExecution(sourceRevision,maxSteps,testCommand,temperature);
        task.bindExperiment(experimentId,experimentRunId,group);
        task.transition(TaskStatus.QUEUED);tasks.save(task);enqueue(project,task,now,null,null);return task;
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
        dataOperations.approveFromTool(taskId,task.getAttempt(),approval.getTool(),approval.getArguments(),approved);
        approval.decide(approved);task.resume();ProjectEntity project=projects.findById(task.getProjectId()).orElseThrow();enqueue(project,task,Instant.now(),approval.getCallId(),approved?"approved":"denied");
        return approval;
    }
    private void enqueue(ProjectEntity project,TaskEntity task,Instant now,String approvalCallId,String decision){
        try{
            Map<String,Object> payload=new LinkedHashMap<>();
            payload.put("version","v1");payload.put("taskId",task.getId());payload.put("projectId",task.getProjectId());payload.put("attempt",task.getAttempt());payload.put("repositoryUrl",project.getRepositoryUrl());payload.put("branch",project.getDefaultBranch());payload.put("prompt",task.getPrompt());payload.put("model",task.getModel());
            payload.put("workspaceId",task.getWorkspaceId());payload.put("conversationId",task.getConversationId());
            if(task.getSourceRevision()!=null)payload.put("sourceRevision",task.getSourceRevision());
            if(task.getMaxSteps()!=null)payload.put("maxSteps",task.getMaxSteps());
            if(task.getTestCommand()!=null)payload.put("testCommand",task.getTestCommand());
            if(task.getTemperature()!=null)payload.put("temperature",task.getTemperature());
            if(task.getExperimentGroup()!=null){
                var experiment=experiments.findById(task.getExperimentId()).orElseThrow(() -> new IllegalStateException("experiment missing"));
                if(!experiment.getProjectId().equals(task.getProjectId())||!experiment.getSourceRevision().equals(task.getSourceRevision()))throw new IllegalStateException("experiment task source mismatch");
                payload.put("repositoryUrl",experiment.getRepositoryUrl());payload.put("branch","");
                payload.put("experimentId",task.getExperimentId());payload.put("experimentRunId",task.getExperimentRunId());payload.put("experimentGroup",task.getExperimentGroup());
                payload.put("profileVersion",task.getProfileVersion());payload.put("experimentProfile",ExperimentProfile.forGroup(task.getExperimentGroup()));
            }
            if(approvalCallId!=null){payload.put("resume",true);payload.put("approvalId",approvalCallId);payload.put("approvalDecision",decision);}
            outbox.save(new OutboxEntity(UUID.randomUUID(),task.getId(),queue,json.writeValueAsString(payload),now));
        }catch(JsonProcessingException e){throw new IllegalStateException(e);}
    }
    public static void validateExecution(String sourceRevision,Integer maxSteps,String testCommand,Double temperature){
        if(sourceRevision!=null&&!sourceRevision.matches("(?i)([0-9a-f]{40}|[0-9a-f]{64})"))throw new IllegalArgumentException("sourceRevision must be a full 40 or 64 character commit hash");
        if(maxSteps!=null&&(maxSteps<1||maxSteps>100))throw new IllegalArgumentException("maxSteps must be between 1 and 100");
        if(testCommand!=null&&(testCommand.isBlank()||testCommand.length()>4096||testCommand.indexOf('\0')>=0||testCommand.contains("\n")||testCommand.contains("\r")))throw new IllegalArgumentException("testCommand must be a single nonempty command up to 4096 characters");
        if(temperature!=null&&(!Double.isFinite(temperature)||temperature<0||temperature>2))throw new IllegalArgumentException("temperature must be between 0 and 2");
    }
    public static boolean matches(TaskEntity task,String prompt,String model,UUID workspaceId,UUID conversationId,String sourceRevision,Integer maxSteps,String testCommand,Double temperature){
        return task.getExperimentId()==null&&task.getPrompt().equals(prompt)&&task.getModel().equals(model)
            &&java.util.Objects.equals(task.getWorkspaceId(),workspaceId)&&java.util.Objects.equals(task.getConversationId(),conversationId)
            &&java.util.Objects.equals(task.getSourceRevision(),sourceRevision)&&java.util.Objects.equals(task.getMaxSteps(),maxSteps)
            &&java.util.Objects.equals(task.getTestCommand(),testCommand)&&java.util.Objects.equals(task.getTemperature(),temperature);
    }
    public record CreateResult(TaskEntity task,boolean created){}
}
