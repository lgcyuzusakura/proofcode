package dev.proofcode.control.source;
import dev.proofcode.control.task.TaskRepository;
import dev.proofcode.control.session.WorkspaceService;
import java.util.*;
import org.springframework.web.bind.annotation.*;
import org.springframework.http.*;
import org.springframework.web.server.ResponseStatusException;

@RestController public class SourceSnapshotController {
    private final SourceSnapshotService sources;private final TaskRepository tasks;private final WorkspaceService scopes;
    public SourceSnapshotController(SourceSnapshotService sources,TaskRepository tasks,WorkspaceService scopes){this.sources=sources;this.tasks=tasks;this.scopes=scopes;}
    public record Upload(UUID workspaceId,String manifestHash,List<SourceSnapshotService.FileEntry> files){}
    @PostMapping("/api/projects/{projectId}/sources") public ResponseEntity<SourceSnapshot> upload(@PathVariable UUID projectId,@RequestBody Upload value){return ResponseEntity.status(201).body(sources.create(projectId,value.workspaceId(),new SourceSnapshotService.Archive(value.manifestHash(),value.files())));}
    @GetMapping("/api/projects/{projectId}/sources/{sourceId}") public SourceSnapshot metadata(@PathVariable UUID projectId,@PathVariable UUID sourceId){scopes.requireProject(projectId);return sources.require(projectId,null,sourceId);}
    @GetMapping(value="/internal/tasks/{taskId}/source",produces=MediaType.APPLICATION_JSON_VALUE) public String content(@PathVariable UUID taskId,@RequestParam int attempt){
        var task=tasks.findById(taskId).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));
        if(task.getAttempt()!=attempt||task.getSourceSnapshotId()==null)throw new ResponseStatusException(HttpStatus.CONFLICT,"task source or attempt mismatch");
        return sources.require(task.getProjectId(),task.getWorkspaceId(),task.getSourceSnapshotId()).getContent();
    }
    @org.springframework.transaction.annotation.Transactional
    @PostMapping("/internal/tasks/{taskId}/source-result") public SourceSnapshot output(@PathVariable UUID taskId,@RequestParam int attempt,@RequestParam UUID runnerId,@RequestBody SourceSnapshotService.Archive archive){
        var task=tasks.lockById(taskId).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));
        if(!"CODE".equals(task.getExecutionMode())||task.getAttempt()!=attempt||!runnerId.equals(task.getRunnerId())||!java.util.Set.of(dev.proofcode.control.task.TaskStatus.RUNNING,dev.proofcode.control.task.TaskStatus.VERIFYING).contains(task.getStatus())||task.getLeaseUntil()==null||!task.getLeaseUntil().isAfter(java.time.Instant.now()))throw new ResponseStatusException(HttpStatus.CONFLICT,"task source result is no longer writable");
        SourceSnapshot value=sources.create(task.getProjectId(),task.getWorkspaceId(),archive);task.bindResultSource(value.getId());return value;
    }
}
