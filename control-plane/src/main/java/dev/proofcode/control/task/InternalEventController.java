package dev.proofcode.control.task;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.time.Instant;
import java.util.UUID;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.http.ResponseEntity;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/internal/tasks")
public class InternalEventController {
    private final TaskRepository tasks;private final TaskEventRepository events;private final ObjectMapper json;private final TaskSocketHandler sockets;
    public InternalEventController(TaskRepository tasks,TaskEventRepository events,ObjectMapper json,TaskSocketHandler sockets){this.tasks=tasks;this.events=events;this.json=json;this.sockets=sockets;}
    @PostMapping("/{taskId}/events")
    @Transactional
    public ResponseEntity<Void> ingest(@PathVariable UUID taskId,@RequestBody RunnerEvent value){if(!taskId.equals(value.taskId()))return ResponseEntity.badRequest().build();if(events.existsByTaskIdAndSequence(taskId,value.sequence()))return ResponseEntity.noContent().build();TaskEntity task=tasks.findById(taskId).orElseThrow();String payload=value.payload().toString();try{events.saveAndFlush(new TaskEventEntity(taskId,value.sequence(),value.type(),payload,value.timestamp()==null?Instant.now():value.timestamp()));applyStatus(task,value);sockets.broadcast(taskId,json.writeValueAsString(value));}catch(DataIntegrityViolationException duplicate){return ResponseEntity.noContent().build();}catch(Exception error){throw new IllegalStateException(error);}return ResponseEntity.accepted().build();}
    @GetMapping("/{taskId}")
    public TaskEntity status(@PathVariable UUID taskId){return tasks.findById(taskId).orElseThrow();}
    private void applyStatus(TaskEntity task,RunnerEvent value){if(task.getStatus()==TaskStatus.SUCCEEDED||task.getStatus()==TaskStatus.FAILED||task.getStatus()==TaskStatus.CANCELLED)return;switch(value.type()){case "task.started"->task.transition(TaskStatus.RUNNING);case "task.completed"->task.complete(value.payload().path("result").asText("Completed"));case "task.failed"->task.fail(value.payload().path("error").asText("Runner failed"));case "task.cancelled"->task.transition(TaskStatus.CANCELLED);case "tool.approval_required"->task.transition(TaskStatus.WAITING_APPROVAL);case "verification.completed"->task.transition(TaskStatus.VERIFYING);default->{}}}
    public record RunnerEvent(String version,UUID taskId,long sequence,String type,Instant timestamp,JsonNode payload){}
}
