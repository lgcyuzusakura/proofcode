package dev.proofcode.control.task;
import java.util.*;
import org.springframework.http.*;
import org.springframework.web.bind.annotation.*;
@RestController
public class TaskInstructionController{
 private final TaskInstructionService service;
 public TaskInstructionController(TaskInstructionService service){this.service=service;}
 @PostMapping("/api/tasks/{taskId}/instructions") public TaskInstruction append(@PathVariable UUID taskId,@RequestBody TaskInstructionService.Input input){return service.append(taskId,input);}
 @GetMapping("/api/tasks/{taskId}/instructions") public List<TaskInstruction> list(@PathVariable UUID taskId){return service.list(taskId);}
 @GetMapping("/internal/tasks/{taskId}/instructions") public ResponseEntity<List<TaskInstruction>> pending(@PathVariable UUID taskId,@RequestParam UUID runnerId,@RequestParam int attempt,@RequestParam(defaultValue="0")long after){return ResponseEntity.ok().cacheControl(CacheControl.noStore()).body(service.pending(taskId,runnerId,attempt,after));}
 public record Ack(long through){}
 @PostMapping("/internal/tasks/{taskId}/instructions/ack") public ResponseEntity<Void> acknowledge(@PathVariable UUID taskId,@RequestParam UUID runnerId,@RequestParam int attempt,@RequestBody Ack ack){service.delivered(taskId,runnerId,attempt,ack.through());return ResponseEntity.noContent().build();}
}
