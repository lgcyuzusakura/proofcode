package dev.proofcode.control.task;

import java.time.Instant;
import java.util.UUID;
import dev.proofcode.control.project.*;
import dev.proofcode.control.websocket.TaskSocketHandler;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.web.server.ResponseStatusException;
import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest(properties="spring.task.scheduling.enabled=false")
class TaskInstructionIntegrationTest {
 @MockBean JmsTemplate jms; @MockBean StringRedisTemplate redis; @MockBean TaskSocketHandler sockets;
 @Autowired TaskService tasks; @Autowired TaskRepository repository; @Autowired ProjectRepository projects; @Autowired TaskInstructionService instructions;
 TaskEntity task(String mode){var id=UUID.randomUUID();projects.save(new ProjectEntity(id,"planning-test","https://example.com/repo.git","main",Instant.now()));return tasks.createWithOutcome(id,"inspect","fixture",null,null,null,null,null,null,null,mode,null).task();}
 @Test void planAndInstructionsAreDurableOrderedIdempotentAndLeaseBound(){
  var task=task("PLAN");assertEquals("PLAN",task.getExecutionMode());
  var input=new TaskInstructionService.Input("只调查测试","message-one",task.getAttempt());
  var first=instructions.append(task.getId(),input);assertEquals(1,first.sequence);assertEquals(first.id,instructions.append(task.getId(),input).id);
  assertThrows(ResponseStatusException.class,()->instructions.append(task.getId(),new TaskInstructionService.Input("different","message-one",1)));
  var second=instructions.append(task.getId(),new TaskInstructionService.Input("补充接口分析","message-two",1));assertEquals(2,second.sequence);
  UUID runner=UUID.randomUUID();assertThrows(ResponseStatusException.class,()->instructions.pending(task.getId(),runner,1,0));
  task.claim(runner,Instant.now());repository.saveAndFlush(task);
  assertEquals(2,instructions.pending(task.getId(),runner,1,0).size());
  assertThrows(ResponseStatusException.class,()->instructions.pending(task.getId(),UUID.randomUUID(),1,0));
  assertThrows(ResponseStatusException.class,()->instructions.pending(task.getId(),runner,2,0));
  assertThrows(IllegalArgumentException.class,()->instructions.delivered(task.getId(),runner,1,3));
  instructions.delivered(task.getId(),runner,1,1);assertNotNull(instructions.list(task.getId()).get(0).deliveredAt);assertNull(instructions.list(task.getId()).get(1).deliveredAt);
  var terminal=repository.findById(task.getId()).orElseThrow();terminal.complete("plan ready");repository.saveAndFlush(terminal);
  assertThrows(ResponseStatusException.class,()->instructions.append(task.getId(),new TaskInstructionService.Input("late","message-three",1)));
  assertEquals(first.id,instructions.append(task.getId(),input).id);
 }
 @Test void rejectsChatStaleAttemptAndEmptyInstructions(){
  var task=task("CHAT");assertThrows(ResponseStatusException.class,()->instructions.append(task.getId(),new TaskInstructionService.Input("hello","chat",1)));
  var code=task("CODE");assertThrows(ResponseStatusException.class,()->instructions.append(code.getId(),new TaskInstructionService.Input("hello","wrong-attempt",2)));
  assertThrows(IllegalArgumentException.class,()->instructions.append(code.getId(),new TaskInstructionService.Input(" ","empty",1)));
  assertEquals("PLAN",TaskService.normalizeMode("plan"));
 }
}
