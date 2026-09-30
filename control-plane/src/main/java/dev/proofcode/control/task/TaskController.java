package dev.proofcode.control.task;

import jakarta.validation.Valid;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Size;
import java.util.List;
import java.util.UUID;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.http.ResponseEntity;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.server.ResponseStatusException;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

@RestController
@RequestMapping("/api/tasks")
public class TaskController {
    private static final Logger log=LoggerFactory.getLogger(TaskController.class);
    private final TaskService service;private final TaskRepository tasks;private final TaskEventRepository events;private final TaskArtifactRepository artifacts;private final TaskApprovalRepository approvals;private final StringRedisTemplate redis;
    public TaskController(TaskService service,TaskRepository tasks,TaskEventRepository events,TaskArtifactRepository artifacts,TaskApprovalRepository approvals,StringRedisTemplate redis){this.service=service;this.tasks=tasks;this.events=events;this.artifacts=artifacts;this.approvals=approvals;this.redis=redis;}
    @PostMapping ResponseEntity<TaskEntity> create(@RequestHeader(value="Idempotency-Key",required=false) String idempotencyKey,@Valid @RequestBody CreateTask request){
        String key=normalizeKey(idempotencyKey);
        try{
            var result=service.createWithOutcome(request.projectId(),request.prompt(),request.model(),key);
            return ResponseEntity.status(result.created()?201:200).body(result.task());
        }catch(DataIntegrityViolationException conflict){
            if(key==null)throw conflict;
            TaskEntity existing=tasks.findByProjectIdAndIdempotencyKey(request.projectId(),key).orElseThrow(()->conflict);
            if(!existing.getPrompt().equals(request.prompt())||!existing.getModel().equals(request.model())){
                throw new ResponseStatusException(HttpStatus.CONFLICT,"idempotency key already belongs to another request");
            }
            return ResponseEntity.ok(existing);
        }
    }
    @GetMapping("/{id}") TaskEntity get(@PathVariable UUID id){return tasks.findById(id).orElseThrow();}
    @GetMapping List<TaskEntity> list(@RequestParam UUID projectId){return tasks.findByProjectIdOrderByCreatedAtDesc(projectId);}
    @GetMapping("/{id}/events") List<TaskEventEntity> eventList(@PathVariable UUID id,@RequestParam(defaultValue="0") long afterSequence){return afterSequence<=0?events.findTop500ByTaskIdOrderBySequenceAsc(id):events.findTop500ByTaskIdAndSequenceGreaterThanOrderBySequenceAsc(id,afterSequence);}
    @GetMapping("/{id}/artifacts") List<ArtifactSummary> artifactList(@PathVariable UUID id,@RequestParam(defaultValue="false") boolean includeHistory){TaskEntity task=tasks.findById(id).orElseThrow();var values=includeHistory?artifacts.findByTaskIdOrderByAttemptDescCreatedAtDesc(id):artifacts.findByTaskIdAndAttemptOrderByCreatedAtDesc(id,task.getAttempt());return values.stream().map(value->new ArtifactSummary(value.getId(),value.getTaskId(),value.getAttempt(),value.getKind(),value.getCommitHash(),value.getBranch(),value.getCreatedAt())).toList();}
    @GetMapping("/{id}/artifacts/{kind}") ResponseEntity<TaskArtifactEntity> artifact(@PathVariable UUID id,@PathVariable String kind){TaskEntity task=tasks.findById(id).orElseThrow();return artifacts.findByTaskIdAndAttemptAndKind(id,task.getAttempt(),kind).map(ResponseEntity::ok).orElseGet(()->ResponseEntity.notFound().build());}
    @GetMapping("/{id}/approvals") List<TaskApprovalEntity> approvalList(@PathVariable UUID id,@RequestParam(defaultValue="false") boolean includeHistory){TaskEntity task=tasks.findById(id).orElseThrow();return includeHistory?approvals.findByTaskIdOrderByAttemptDescCreatedAtDesc(id):approvals.findByTaskIdAndAttemptOrderByCreatedAtDesc(id,task.getAttempt());}
    @PostMapping("/{id}/approvals/{approvalId}") TaskApprovalEntity decideApproval(@PathVariable UUID id,@PathVariable UUID approvalId,@Valid @RequestBody ApprovalDecision request){return service.decideApproval(id,approvalId,request.approved());}
    @PostMapping("/{id}/cancel") TaskEntity cancel(@PathVariable UUID id){
        TaskEntity task=service.cancel(id);
        if(task.getStatus()==TaskStatus.CANCELLED){
            try{redis.opsForValue().set("cancel:task:"+id,"1",java.time.Duration.ofHours(1));}
            catch(RuntimeException error){log.warn("Redis cancellation hint failed for task {}; runner will poll task status",id,error);}
        }
        return task;
    }
    @PostMapping("/{id}/retry") TaskEntity retry(@PathVariable UUID id){return service.retry(id);}
    private String normalizeKey(String value){if(value==null)return null;String key=value.trim();if(key.length()>200)throw new IllegalArgumentException("Idempotency-Key exceeds 200 characters");return key.isEmpty()?null:key;}
    public record CreateTask(@NotNull UUID projectId,@NotBlank @Size(max=131072) String prompt,@NotBlank @Size(max=200) String model){}
    public record ApprovalDecision(@NotNull Boolean approved){ }
    public record ArtifactSummary(UUID id,UUID taskId,int attempt,String kind,String commitHash,String branch,java.time.Instant createdAt){}
}
