package dev.proofcode.control.task;

import java.time.Instant;
import java.util.UUID;
import org.springframework.http.ResponseEntity;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/internal/tasks")
public class RunnerLeaseController {
    private final TaskRepository tasks;
    public RunnerLeaseController(TaskRepository tasks){this.tasks=tasks;}

    @PostMapping("/{taskId}/claim")
    @Transactional
    public ResponseEntity<Void> claim(@PathVariable UUID taskId,@RequestBody LeaseRequest request){
        if(request.runnerId()==null||request.attempt()<1)return ResponseEntity.badRequest().build();
        TaskEntity task=tasks.lockById(taskId).orElseThrow();
        if(task.getAttempt()!=request.attempt())return ResponseEntity.status(409).build();
        return task.claim(request.runnerId(),Instant.now())?ResponseEntity.accepted().build():ResponseEntity.status(409).build();
    }

    @PostMapping("/{taskId}/renew")
    @Transactional
    public ResponseEntity<Void> renew(@PathVariable UUID taskId,@RequestBody LeaseRequest request){
        if(request.runnerId()==null||request.attempt()<1)return ResponseEntity.badRequest().build();
        TaskEntity task=tasks.lockById(taskId).orElseThrow();
        if(task.getAttempt()!=request.attempt())return ResponseEntity.status(409).build();
        return task.renew(request.runnerId(),Instant.now())?ResponseEntity.noContent().build():ResponseEntity.status(409).build();
    }

    public record LeaseRequest(UUID runnerId,int attempt){}
}
