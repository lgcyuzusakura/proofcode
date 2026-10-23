package dev.proofcode.control.task;

import dev.proofcode.control.session.WorkspaceService;
import java.time.Instant;
import java.util.*;
import org.springframework.data.domain.PageRequest;
import org.springframework.http.*;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.server.ResponseStatusException;

@RestController
public class TaskHistoryController {
 private final TaskRepository tasks;private final TaskEventRepository events;private final TaskArtifactRepository artifacts;private final WorkspaceService scopes;
 private static final Map<String,String> TITLES=Map.ofEntries(Map.entry("task.created","创建任务"),Map.entry("task.queued","任务进入队列"),Map.entry("task.started","开始执行"),Map.entry("task.completed","任务完成"),Map.entry("task.failed","执行失败"),Map.entry("task.cancelled","任务已停止"),Map.entry("tool.approval_required","等待工具批准"),Map.entry("checkpoint.created","保存检查点"),Map.entry("recovery.created","生成恢复补丁"),Map.entry("verification.completed","完成验证"));
 public TaskHistoryController(TaskRepository tasks,TaskEventRepository events,TaskArtifactRepository artifacts,WorkspaceService scopes){this.tasks=tasks;this.events=events;this.artifacts=artifacts;this.scopes=scopes;}
 public record Entry(long sequence,int attempt,String type,String title,Instant timestamp){}
 public record Page(UUID taskId,int currentAttempt,String status,List<Entry> entries,List<TaskController.ArtifactSummary> artifacts,Long nextBeforeSequence){}
 private TaskEntity require(UUID id){var task=tasks.findById(id).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));scopes.requireProject(task.getProjectId());return task;}
 @GetMapping("/api/tasks/{id}/history")public ResponseEntity<Page> history(@PathVariable UUID id,@RequestParam(required=false)Long beforeSequence,@RequestParam(defaultValue="40")int limit){
  var task=require(id);if(limit<1||limit>100||(beforeSequence!=null&&beforeSequence<1))throw new IllegalArgumentException("无效的历史分页");
  var rows=events.history(id,List.copyOf(TITLES.keySet()),beforeSequence==null?Long.MAX_VALUE:beforeSequence,PageRequest.of(0,limit+1));
  var values=rows.stream().limit(limit).map(event->new Entry(event.getSequence(),event.getAttempt(),event.getType(),TITLES.get(event.getType()),event.getTimestamp())).toList();
  Long next=rows.size()>limit?values.get(values.size()-1).sequence():null;
  var attempts=new HashSet<Integer>();values.forEach(value->attempts.add(value.attempt()));if(beforeSequence==null)attempts.add(task.getAttempt());
  var saved=attempts.isEmpty()?List.<TaskController.ArtifactSummary>of():artifacts.historyMetadata(id,List.copyOf(attempts)).stream().map(value->new TaskController.ArtifactSummary(value.getId(),value.getTaskId(),value.getAttempt(),value.getKind(),value.getCommitHash(),value.getBranch(),value.getCreatedAt())).toList();
  return ResponseEntity.ok().cacheControl(CacheControl.noStore()).body(new Page(id,task.getAttempt(),task.getStatus().name(),values,saved,next));
 }
 @GetMapping("/api/tasks/{id}/artifacts/by-id/{artifactId}")public ResponseEntity<TaskArtifactEntity> artifact(@PathVariable UUID id,@PathVariable UUID artifactId){
  require(id);var value=artifacts.findById(artifactId).filter(item->item.getTaskId().equals(id)).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));
  return ResponseEntity.ok().cacheControl(CacheControl.noStore()).body(value);
 }
}
