package dev.proofcode.control.task;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.time.Instant;
import java.nio.charset.StandardCharsets;
import java.util.UUID;
import org.springframework.http.ResponseEntity;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.transaction.support.TransactionSynchronization;
import org.springframework.transaction.support.TransactionSynchronizationManager;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/internal/tasks")
public class InternalEventController {
    private final TaskRepository tasks;private final TaskEventRepository events;private final TaskArtifactRepository artifacts;private final TaskApprovalRepository approvals;private final ObjectMapper json;private final TaskSocketHandler sockets;
    public InternalEventController(TaskRepository tasks,TaskEventRepository events,TaskArtifactRepository artifacts,TaskApprovalRepository approvals,ObjectMapper json,TaskSocketHandler sockets){this.tasks=tasks;this.events=events;this.artifacts=artifacts;this.approvals=approvals;this.json=json;this.sockets=sockets;}
    @PostMapping("/{taskId}/events")
    @Transactional
    public ResponseEntity<?> ingest(@PathVariable UUID taskId,@RequestBody RunnerEvent value){
        if(value==null||!taskId.equals(value.taskId())||!"v1".equals(value.version())||value.runnerId()==null
            ||value.attempt()<1||value.sequence()<1||value.type()==null||!validType(value.type())
            ||value.payload()==null||!value.payload().isObject()||value.runId()==null
            ||value.runId().isBlank()||value.runId().length()>200)return ResponseEntity.badRequest().build();
        TaskEntity task=tasks.lockById(taskId).orElseThrow();
        if(task.getAttempt()!=value.attempt())return ResponseEntity.status(409).build();
        if(!value.runnerId().equals(task.getRunnerId()))return ResponseEntity.status(409).build();
        String eventKey=value.attempt()+":"+value.runId()+":"+value.sequence();
        if(events.existsByTaskIdAndEventKey(taskId,eventKey))return ResponseEntity.noContent().build();
        if(!task.acceptsEvent(value.type(),Instant.now()))return ResponseEntity.status(409).build();
        validateEventPayload(value);
        Instant timestamp=value.timestamp()==null?Instant.now():value.timestamp();
        long sequence=events.maxSequence(taskId)+1;
        saveApproval(task,value);
        saveArtifact(task,value);
        JsonNode eventPayload=eventPayload(value);
        events.saveAndFlush(new TaskEventEntity(taskId,value.runnerId(),value.runId(),value.attempt(),sequence,eventKey,value.type(),eventPayload.toString(),timestamp));
        applyStatus(task,value);
        try {
            String message=json.writeValueAsString(new RunnerEvent(value.version(),taskId,value.runnerId(),value.runId(),value.attempt(),sequence,value.type(),timestamp,eventPayload));
            TransactionSynchronizationManager.registerSynchronization(new TransactionSynchronization() {
                @Override public void afterCommit() { sockets.broadcast(taskId,message); }
            });
        } catch (com.fasterxml.jackson.core.JsonProcessingException error) {
            throw new IllegalStateException("serialize task event",error);
        }
        return ResponseEntity.accepted().body(new IngestResponse(sequence));
    }
    @GetMapping("/{taskId}")
    public TaskEntity status(@PathVariable UUID taskId){return tasks.findById(taskId).orElseThrow();}
    @GetMapping("/{taskId}/artifacts")
    public java.util.List<TaskArtifactEntity> artifacts(@PathVariable UUID taskId,@RequestParam(defaultValue="false") boolean includeHistory){TaskEntity task=tasks.findById(taskId).orElseThrow();return includeHistory?artifacts.findByTaskIdOrderByAttemptDescCreatedAtDesc(taskId):artifacts.findByTaskIdAndAttemptOrderByCreatedAtDesc(taskId,task.getAttempt());}
    @GetMapping("/{taskId}/artifacts/{kind}")
    public ResponseEntity<TaskArtifactEntity> artifact(@PathVariable UUID taskId,@PathVariable String kind){TaskEntity task=tasks.findById(taskId).orElseThrow();return artifacts.findByTaskIdAndAttemptAndKind(taskId,task.getAttempt(),kind).map(ResponseEntity::ok).orElseGet(()->ResponseEntity.notFound().build());}
    private void saveArtifact(TaskEntity task,RunnerEvent value) {
        UUID taskId=task.getId();
        String kind=switch(value.type()) { case "checkpoint.created" -> "checkpoint"; case "recovery.created" -> "recovery"; default -> null; };
        if (kind==null || artifacts.findByTaskIdAndAttemptAndKind(taskId,task.getAttempt(),kind).isPresent()) return;
        JsonNode payload=value.payload();
        String patch=text(payload,"patch");
        if(patch==null||patch.isEmpty()||patch.getBytes(StandardCharsets.UTF_8).length>8<<20)throw new IllegalArgumentException(kind+" patch is missing or too large");
        var metadata=payload.deepCopy();
        if(metadata instanceof com.fasterxml.jackson.databind.node.ObjectNode object) object.remove("patch");
        artifacts.save(new TaskArtifactEntity(UUID.randomUUID(),taskId,task.getAttempt(),kind,text(payload,"commit"),text(payload,"branch"),patch,metadata.toString(),value.timestamp()==null?Instant.now():value.timestamp()));
    }
    private void saveApproval(TaskEntity task,RunnerEvent value) {
        UUID taskId=task.getId();
        if (!"tool.approval_required".equals(value.type())) return;
        JsonNode payload=value.payload();
        String callId=text(payload,"callId");
        String tool=text(payload,"tool");
        if(callId==null||tool==null||approvals.findByTaskIdAndAttemptAndCallId(taskId,task.getAttempt(),callId).isPresent()) return;
        JsonNode arguments=payload.get("arguments");
        approvals.save(new TaskApprovalEntity(UUID.nameUUIDFromBytes((taskId+":"+task.getAttempt()+":"+callId).getBytes(StandardCharsets.UTF_8)),taskId,task.getAttempt(),callId,tool,text(payload,"risk"),arguments==null?"{}":arguments.toString(),value.timestamp()==null?Instant.now():value.timestamp()));
    }
    private JsonNode eventPayload(RunnerEvent value) {
        if (!"checkpoint.created".equals(value.type()) && !"recovery.created".equals(value.type())) return value.payload();
        var payload=value.payload().deepCopy();
        if (payload instanceof com.fasterxml.jackson.databind.node.ObjectNode object) object.remove("patch");
        return payload;
    }
    private String text(JsonNode value,String field){JsonNode node=value.get(field);return node==null||node.isNull()?null:node.asText();}
    private boolean validType(String type){return switch(type){
        case "task.started","task.completed","task.failed","task.cancelled","agent.message.delta","agent.message.completed",
             "agent.delegated","tool.requested","tool.approval_required","tool.started","tool.completed","tool.failed",
             "decision.tool_routed","decision.tool_route_fallback","context.selected","checkpoint.created",
             "recovery.created","verification.completed","usage.updated" -> true;
        default -> false;
    };}
    private void validateEventPayload(RunnerEvent value){
        JsonNode payload=value.payload();
        if("tool.approval_required".equals(value.type())){
            String callId=text(payload,"callId");
            String tool=text(payload,"tool");
            String risk=text(payload,"risk");
            if(callId==null||callId.isBlank()||callId.length()>200||tool==null||tool.isBlank()||tool.length()>120
                ||risk==null||risk.isBlank()||risk.length()>40||!payload.path("arguments").isObject()){
                throw new IllegalArgumentException("approval event requires callId, tool, risk and object arguments");
            }
        }
        if("checkpoint.created".equals(value.type())||"recovery.created".equals(value.type())){
            JsonNode patch=payload.path("patch");
            if(!patch.isTextual()||patch.textValue().isEmpty()||patch.textValue().getBytes(StandardCharsets.UTF_8).length>8<<20){
                throw new IllegalArgumentException("artifact patch is missing or exceeds 8 MiB");
            }
            var metadata=payload.deepCopy();
            if(metadata instanceof com.fasterxml.jackson.databind.node.ObjectNode object)object.remove("patch");
            if(metadata.toString().getBytes(StandardCharsets.UTF_8).length>1<<20){
                throw new IllegalArgumentException("artifact metadata exceeds 1 MiB");
            }
        }else{
            int maxBytes=("tool.requested".equals(value.type())||"tool.approval_required".equals(value.type()))
                ?18<<20:1<<20;
            if(payload.toString().getBytes(StandardCharsets.UTF_8).length>maxBytes){
                throw new IllegalArgumentException("event payload exceeds the limit for "+value.type());
            }
        }
    }
    private void applyStatus(TaskEntity task,RunnerEvent value){if(task.getStatus()==TaskStatus.SUCCEEDED||task.getStatus()==TaskStatus.FAILED||task.getStatus()==TaskStatus.CANCELLED)return;switch(value.type()){case "task.started"->task.transition(TaskStatus.RUNNING);case "task.completed"->task.complete(value.payload().path("result").asText("Completed"));case "task.failed"->task.fail(value.payload().path("error").asText("Runner failed"));case "task.cancelled"->task.transition(TaskStatus.CANCELLED);case "tool.approval_required"->task.transition(TaskStatus.WAITING_APPROVAL);case "verification.completed"->task.transition(TaskStatus.VERIFYING);default->{}}}
    public record RunnerEvent(String version,UUID taskId,UUID runnerId,String runId,int attempt,long sequence,String type,Instant timestamp,JsonNode payload){}
    public record IngestResponse(long sequence){}
}
