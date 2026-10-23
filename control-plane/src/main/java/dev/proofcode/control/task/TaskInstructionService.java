package dev.proofcode.control.task;
import java.time.Instant;
import java.util.*;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;
import org.springframework.http.HttpStatus;
@Service
public class TaskInstructionService {
 private final TaskRepository tasks;private final TaskInstructionRepository instructions;
 public TaskInstructionService(TaskRepository tasks,TaskInstructionRepository instructions){this.tasks=tasks;this.instructions=instructions;}
 public record Input(String content,String clientKey,int attempt){}
 @Transactional public TaskInstruction append(UUID id,Input input){
  var task=tasks.lockById(id).orElseThrow();
  if(input==null||input.content()==null||input.content().isBlank()||input.content().length()>16000||input.clientKey()==null||!input.clientKey().matches("[A-Za-z0-9_-]{1,80}"))throw new IllegalArgumentException("instruction requires text (up to 16000 characters) and a client key");
  if(input.attempt()!=task.getAttempt())throw conflict("instruction belongs to another task attempt");
  var existing=instructions.findByTaskIdAndAttemptAndClientKey(id,task.getAttempt(),input.clientKey());
  if(existing.isPresent()){if(!existing.get().content.equals(input.content()))throw conflict("instruction key already used for different content");return existing.get();}
  if(task.getExecutionMode().equals("CHAT")||!Set.of(TaskStatus.QUEUED,TaskStatus.RUNNING,TaskStatus.VERIFYING,TaskStatus.WAITING_APPROVAL).contains(task.getStatus()))throw conflict("task is not accepting instructions");
  var values=list(id);if(values.size()>=100||values.stream().mapToInt(v->v.content.length()).sum()+input.content().length()>128000)throw conflict("task instruction budget reached");
  long sequence=values.isEmpty()?1:values.get(values.size()-1).sequence+1;
  return instructions.saveAndFlush(new TaskInstruction(id,task.getAttempt(),sequence,input.content(),input.clientKey()));
 }
 public List<TaskInstruction> list(UUID id){var task=tasks.findById(id).orElseThrow();return instructions.findByTaskIdAndAttemptOrderBySequenceAsc(id,task.getAttempt());}
 public List<TaskInstruction> pending(UUID id,UUID runner,int attempt,long after){lease(id,runner,attempt);if(after<0)throw new IllegalArgumentException("invalid instruction cursor");return instructions.findByTaskIdAndAttemptOrderBySequenceAsc(id,attempt).stream().filter(v->v.sequence>after).toList();}
 @Transactional public void delivered(UUID id,UUID runner,int attempt,long through){tasks.lockById(id).orElseThrow();lease(id,runner,attempt);var values=instructions.findByTaskIdAndAttemptOrderBySequenceAsc(id,attempt);if(through<0||(!values.isEmpty()&&through>values.get(values.size()-1).sequence)||(values.isEmpty()&&through!=0))throw new IllegalArgumentException("invalid instruction acknowledgement");for(var v:values)if(v.sequence<=through&&v.deliveredAt==null)v.deliveredAt=Instant.now();}
 private void lease(UUID id,UUID runner,int attempt){var task=tasks.findById(id).orElseThrow();if(task.getAttempt()!=attempt||!Objects.equals(task.getRunnerId(),runner)||task.getLeaseUntil()==null||!task.getLeaseUntil().isAfter(Instant.now())||!Set.of(TaskStatus.RUNNING,TaskStatus.VERIFYING).contains(task.getStatus()))throw conflict("active task lease required");}
 private static ResponseStatusException conflict(String message){return new ResponseStatusException(HttpStatus.CONFLICT,message);}
}
