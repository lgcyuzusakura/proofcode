package dev.proofcode.control.agentplatform;

import java.util.*;
import dev.proofcode.control.task.*;
import org.springframework.web.bind.annotation.*;
import org.springframework.http.*;
import org.springframework.web.server.ResponseStatusException;

@RestController
public class AgentPlatformController {
 private final AgentPlatformService service;private final TaskRepository tasks;
 public AgentPlatformController(AgentPlatformService service,TaskRepository tasks){this.service=service;this.tasks=tasks;}
 UUID taskProject(UUID taskId){return tasks.findById(taskId).orElseThrow().getProjectId();}
 @GetMapping("/api/projects/{project}/agent-platform/{kind:AGENT|MEMORY}") public List<AgentPlatformService.View> list(@PathVariable UUID project,@PathVariable String kind){return service.list(project,kind);}
 @PostMapping("/api/projects/{project}/agent-platform/{kind:AGENT|MEMORY}") public AgentPlatformService.View create(@PathVariable UUID project,@PathVariable String kind,@RequestBody AgentPlatformService.Input input){return service.save(project,kind,null,input);}
 @PatchMapping("/api/projects/{project}/agent-platform/{kind:AGENT|MEMORY}/{id}") public AgentPlatformService.View update(@PathVariable UUID project,@PathVariable String kind,@PathVariable UUID id,@RequestBody AgentPlatformService.Input input){return service.save(project,kind,id,input);}
 @PostMapping("/api/projects/{project}/agent-platform/{kind:AGENT|MEMORY}/{id}/delete") public ResponseEntity<Void> delete(@PathVariable UUID project,@PathVariable String kind,@PathVariable UUID id,@RequestParam long version){service.delete(project,kind,id,version);return ResponseEntity.noContent().build();}
 @GetMapping("/api/projects/{project}/agent-platform/selection/{conversation}") public AgentPlatformService.Selection selection(@PathVariable UUID project,@PathVariable UUID conversation){return service.selection(project,conversation);}
 @PatchMapping("/api/projects/{project}/agent-platform/selection/{conversation}") public AgentPlatformService.Selection select(@PathVariable UUID project,@PathVariable UUID conversation,@RequestBody AgentPlatformService.Selection value){return service.select(project,conversation,value);}
 @GetMapping("/api/projects/{project}/agent-platform/preview") public List<AgentPlatformService.MemoryEvidence> preview(@PathVariable UUID project,@RequestParam String query){if(query.length()>131072)throw new IllegalArgumentException("query too long");return service.retrieve(project,query);}
 @GetMapping("/internal/tasks/{taskId}/agent-snapshot") public ResponseEntity<String> snapshot(@PathVariable UUID taskId,@RequestParam UUID runnerId,@RequestParam int attempt){
  var task=tasks.findById(taskId).orElseThrow();if(task.getAttempt()!=attempt||!Objects.equals(task.getRunnerId(),runnerId)||task.getLeaseUntil()==null||!task.getLeaseUntil().isAfter(java.time.Instant.now())||!(task.getStatus()==TaskStatus.RUNNING||task.getStatus()==TaskStatus.VERIFYING))throw new ResponseStatusException(HttpStatus.CONFLICT,"active task lease required");
  return ResponseEntity.ok().cacheControl(CacheControl.noStore()).contentType(MediaType.APPLICATION_JSON).body(task.getAgentSnapshot()==null?"null":task.getAgentSnapshot());
 }
}
