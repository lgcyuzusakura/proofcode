package dev.proofcode.control.task;

import jakarta.validation.Valid;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import java.util.List;
import java.util.UUID;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/tasks")
public class TaskController {
    private final TaskService service;private final TaskRepository tasks;private final TaskEventRepository events;private final StringRedisTemplate redis;
    public TaskController(TaskService service,TaskRepository tasks,TaskEventRepository events,StringRedisTemplate redis){this.service=service;this.tasks=tasks;this.events=events;this.redis=redis;}
    @PostMapping ResponseEntity<TaskEntity> create(@RequestHeader(value="Idempotency-Key",required=false) String idempotencyKey,@Valid @RequestBody CreateTask request){TaskEntity task=service.create(request.projectId(),request.prompt(),request.model(),normalizeKey(idempotencyKey));return ResponseEntity.status(task.getCreatedAt().equals(task.getUpdatedAt())?201:200).body(task);}
    @GetMapping("/{id}") TaskEntity get(@PathVariable UUID id){return tasks.findById(id).orElseThrow();}
    @GetMapping List<TaskEntity> list(@RequestParam UUID projectId){return tasks.findByProjectIdOrderByCreatedAtDesc(projectId);}
    @GetMapping("/{id}/events") List<TaskEventEntity> eventList(@PathVariable UUID id,@RequestParam(defaultValue="0") long afterSequence){return afterSequence<=0?events.findByTaskIdOrderBySequenceAsc(id):events.findByTaskIdAndSequenceGreaterThanOrderBySequenceAsc(id,afterSequence);}
    @PostMapping("/{id}/cancel") TaskEntity cancel(@PathVariable UUID id){TaskEntity task=tasks.findById(id).orElseThrow();if(task.getStatus()!=TaskStatus.SUCCEEDED&&task.getStatus()!=TaskStatus.FAILED&&task.getStatus()!=TaskStatus.CANCELLED){task.transition(TaskStatus.CANCELLED);redis.opsForValue().set("cancel:task:"+id,"1",java.time.Duration.ofHours(1));return tasks.save(task);}return task;}
    @PostMapping("/{id}/retry") TaskEntity retry(@PathVariable UUID id){return service.retry(id);}
    private String normalizeKey(String value){if(value==null)return null;String key=value.trim();return key.isEmpty()?null:key.length()>200?key.substring(0,200):key;}
    public record CreateTask(@NotNull UUID projectId,@NotBlank String prompt,@NotBlank String model){}
}
