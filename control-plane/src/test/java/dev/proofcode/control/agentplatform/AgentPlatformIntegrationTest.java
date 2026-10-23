package dev.proofcode.control.agentplatform;

import java.time.Instant;
import java.util.*;
import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.project.*;
import dev.proofcode.control.task.*;
import dev.proofcode.control.session.WorkspaceService;
import dev.proofcode.control.websocket.TaskSocketHandler;
import org.junit.jupiter.api.*;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.security.authentication.UsernamePasswordAuthenticationToken;
import org.springframework.security.core.context.SecurityContextHolder;
import org.springframework.web.server.ResponseStatusException;
import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest(properties="spring.task.scheduling.enabled=false")
class AgentPlatformIntegrationTest {
 @MockBean JmsTemplate jmsTemplate;
 @MockBean StringRedisTemplate redisTemplate;
 @MockBean TaskSocketHandler sockets;
 @Autowired AgentPlatformService service;
 @Autowired AgentPlatformController api;
 @Autowired AgentResourceRepository resources;
 @Autowired ProjectRepository projects;
 @Autowired WorkspaceService scopes;
 @Autowired TaskService tasks;
 @Autowired ObjectMapper json;
 @AfterEach void clearAuth(){SecurityContextHolder.clearContext();}
 UUID project(){UUID id=UUID.randomUUID();projects.save(new ProjectEntity(id,"agent-test","https://example.com/repo.git","main",Instant.now()));return id;}
 AgentPlatformService.Input agent(String name,long version,String instructions){return new AgentPlatformService.Input(name,version,new AgentPlatformService.Profile(instructions,List.of("read_file","search_code"),List.of("docs"),true),null);}
 AgentPlatformService.Input memory(String name,long version,String content,String keywords,boolean pinned,boolean enabled){return new AgentPlatformService.Input(name,version,null,new AgentPlatformService.Memory(content,keywords,pinned,enabled));}

 @Test void snapshotsAgentAndOnlyRelevantEnabledMemoryAndPreservesOnRetry() throws Exception {
  UUID project=project();var scope=scopes.resolve(project,null,null);
  var profile=service.save(project,"AGENT",null,agent("审查员",0,"先读测试，再解释问题"));
  var preference=service.save(project,"MEMORY",null,memory("输出偏好",0,"回答使用中文","",true,true));
  service.save(project,"MEMORY",null,memory("数据库规范",0,"数据库迁移必须有测试","数据库 迁移",false,true));
  service.save(project,"MEMORY",null,memory("影片制作",0,"分镜固定使用电影比例","分镜",false,true));
  service.save(project,"MEMORY",null,memory("已过期",0,"旧的数据库口令不应出现","数据库",true,false));
  service.select(project,scope.conversationId(),new AgentPlatformService.Selection(profile.id()));
  var task=tasks.create(project,"检查数据库迁移","fixture-model");
  var snapshot=json.readValue(task.getAgentSnapshot(),AgentPlatformService.Snapshot.class);
  assertEquals(profile.id(),snapshot.profileId());assertTrue(snapshot.profile().readOnly());
  assertEquals(Set.of("输出偏好","数据库规范"),new HashSet<>(snapshot.memories().stream().map(AgentPlatformService.MemoryEvidence::name).toList()));
  String original=task.getAgentSnapshot();String hash=task.getAgentSnapshotHash();
  service.save(project,"AGENT",profile.id(),agent("审查员",profile.version(),"修改后的角色"));
  service.delete(project,"MEMORY",preference.id(),preference.version());
  task=tasks.retry(failTask(task));
  assertEquals(original,task.getAgentSnapshot());assertEquals(hash,task.getAgentSnapshotHash());
  var next=tasks.create(project,"检查数据库迁移","fixture-model");
  assertTrue(next.getAgentSnapshot().contains("修改后的角色"));assertFalse(next.getAgentSnapshot().contains("回答使用中文"));
 }
 @Autowired TaskRepository taskRepository;
 UUID failTask(TaskEntity task){task.claim(UUID.randomUUID(),Instant.now());task.fail("fixture");taskRepository.saveAndFlush(task);return task.getId();}

 @Test void rejectsCrossProjectAndStaleEditsAndInvalidTools(){
  UUID a=project(),b=project();var profile=service.save(a,"AGENT",null,agent("审查",0,"inspect"));
  var scope=scopes.resolve(b,null,null);
  assertThrows(ResponseStatusException.class,()->service.select(b,scope.conversationId(),new AgentPlatformService.Selection(profile.id())));
  assertThrows(ResponseStatusException.class,()->service.save(a,"AGENT",profile.id(),agent("审查",100,"stale")));
  assertThrows(IllegalArgumentException.class,()->service.save(a,"AGENT",null,new AgentPlatformService.Input("bad",0,new AgentPlatformService.Profile("",List.of("../shell"),List.of(),false),null)));
  UUID owner=UUID.randomUUID();var value=projects.findById(a).orElseThrow();value.setOwnerId(owner);projects.save(value);
  SecurityContextHolder.getContext().setAuthentication(new UsernamePasswordAuthenticationToken(UUID.randomUUID(),null,List.of()));
  assertThrows(ResponseStatusException.class,()->service.list(a,"AGENT"));
 }

 @Test void boundsRetrievalAndChecksRunnerLease(){
  UUID project=project();
  for(int i=0;i<10;i++)service.save(project,"MEMORY",null,memory("偏好"+i,0,"x".repeat(3000),"",true,true));
  assertEquals(4,service.retrieve(project,"").size());
  var task=tasks.create(project,"hello","fixture-model");
  assertThrows(ResponseStatusException.class,()->api.snapshot(task.getId(),UUID.randomUUID(),1));
  UUID runner=UUID.randomUUID();task.claim(runner,Instant.now());taskRepository.saveAndFlush(task);
  assertEquals(task.getAgentSnapshot(),api.snapshot(task.getId(),runner,1).getBody());
  assertThrows(ResponseStatusException.class,()->api.snapshot(task.getId(),runner,2));
 }
}
