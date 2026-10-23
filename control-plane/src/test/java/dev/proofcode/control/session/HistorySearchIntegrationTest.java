package dev.proofcode.control.session;

import com.fasterxml.jackson.databind.*;
import dev.proofcode.control.account.*;
import dev.proofcode.control.project.*;
import dev.proofcode.control.task.*;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.time.Instant;
import java.util.*;
import org.junit.jupiter.api.*;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.http.*;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.security.authentication.UsernamePasswordAuthenticationToken;
import org.springframework.security.core.context.SecurityContextHolder;
import org.springframework.web.server.ResponseStatusException;
import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest(webEnvironment=SpringBootTest.WebEnvironment.RANDOM_PORT,properties="spring.task.scheduling.enabled=false")
class HistorySearchIntegrationTest {
 @MockBean JmsTemplate jms;@MockBean StringRedisTemplate redis;@MockBean TaskSocketHandler sockets;
 @Autowired HistorySearchService search;@Autowired WorkspaceService scopes;@Autowired ConversationMessageService messages;
 @Autowired TaskService tasks;@Autowired TaskRepository taskRepository;@Autowired TaskEventRepository events;@Autowired TaskArtifactRepository artifacts;
 @Autowired AccountService accounts;@Autowired ProjectRepository projects;@Autowired TestRestTemplate http;@Autowired ObjectMapper json;
 @Autowired org.springframework.jdbc.core.JdbcTemplate jdbc;
 UUID owner,project;String token;WorkspaceService.Scope scope;
 @BeforeEach void setup(){var login=accounts.register("history-"+UUID.randomUUID(),"fixture-password");owner=login.user().id();token=login.token();login(owner);project=project(owner);scope=scopes.resolve(project,null,null);}
 @AfterEach void cleanup(){SecurityContextHolder.clearContext();}
 void login(UUID id){SecurityContextHolder.getContext().setAuthentication(new UsernamePasswordAuthenticationToken(id,null,List.of()));}
 UUID project(UUID user){var id=UUID.randomUUID();var p=new ProjectEntity(id,"Search project","https://example.com/repo.git","main",Instant.now());p.setOwnerId(user);projects.save(p);return id;}
 TaskEntity task(String prompt,String answer){var task=new TaskEntity(UUID.randomUUID(),project,prompt,"fixture",Instant.now());task.bindScope(scope.workspaceId(),scope.conversationId());task.bindSource("CHAT",null);task.transition(TaskStatus.QUEUED);taskRepository.saveAndFlush(task);messages.start(task);messages.finish(task,answer,"COMPLETED");return task;}
 ResponseEntity<JsonNode> get(String path,String auth){var headers=new HttpHeaders();headers.setBearerAuth(auth);return http.exchange(path,HttpMethod.GET,new HttpEntity<>(null,headers),JsonNode.class);}
 @Test void searchesMessageBodyTitleAndTaskResultWithBoundedExcerptsAcrossProjects(){
  var chat=scopes.createConversation(project,scope.workspaceId(),"上下文治理方案");scope=new WorkspaceService.Scope(project,scope.workspaceId(),chat.getId());
  var task=task("investigate keywordneedle","x".repeat(1600)+"上下文污染 needle-answer");
  task.claim(UUID.randomUUID(),Instant.now());task.complete("任务结果 needle-result");taskRepository.saveAndFlush(task);
  var answer=search.search("needle-answer",null,HistorySearchService.Kind.MESSAGE,null,25);assertEquals(1,answer.items().size());assertTrue(answer.items().get(0).excerpt().contains("needle-answer"));assertTrue(answer.items().get(0).excerpt().length()<=280);assertEquals(task.getId(),answer.items().get(0).taskId());assertNotNull(answer.items().get(0).messageId());
  assertEquals(1,search.search("治理",null,HistorySearchService.Kind.CONVERSATION,null,25).items().size());
  assertEquals(1,search.search("needle-result",project,HistorySearchService.Kind.TASK,null,25).items().size());
  UUID second=project(owner);var original=project;project=second;scope=scopes.resolve(second,null,null);task("another keywordneedle","other answer");
  assertEquals(2,search.search("keywordneedle",null,HistorySearchService.Kind.TASK,null,25).items().size());assertEquals(1,search.search("keywordneedle",original,HistorySearchService.Kind.TASK,null,25).items().size());
 }
 @Test void treatsSqlWildcardsAsLiteralAndRejectsInvalidQueries(){
  task("literal 10%_!\\ marker","answer");task("literal 10XX marker","answer");
  assertEquals(1,search.search("10%_!\\",null,HistorySearchService.Kind.TASK,null,25).items().size());
  for(String text:List.of("x".repeat(101),"bad\nquery"))assertThrows(IllegalArgumentException.class,()->search.search(text,null,HistorySearchService.Kind.ALL,null,25));
  assertThrows(IllegalArgumentException.class,()->search.search("",null,HistorySearchService.Kind.ALL,null,51));
  assertThrows(IllegalArgumentException.class,()->search.search("",null,HistorySearchService.Kind.ALL,"bad-cursor",25));
 }
 @Test void keysetPaginationDoesNotDuplicateOrIncludeNewRowsAndBindsSearchCriteria(){
  for(int i=0;i<12;i++)task("history-page-"+i,"history-page-answer-"+i);
  var first=search.search("history-page",null,HistorySearchService.Kind.ALL,null,3);assertNotNull(first.nextCursor());
  var newTask=task("history-page-new","history-page-new-answer");
  var ids=new HashSet<String>();first.items().forEach(hit->assertTrue(ids.add(hit.kind()+":"+hit.id())));String cursor=first.nextCursor();int pages=0;
  while(cursor!=null){assertTrue(pages++<30);var page=search.search("history-page",null,HistorySearchService.Kind.ALL,cursor,3);for(var hit:page.items()){assertTrue(ids.add(hit.kind()+":"+hit.id()));assertNotEquals(newTask.getId(),hit.taskId());}cursor=page.nextCursor();}
  assertEquals(36,ids.size());
  assertThrows(IllegalArgumentException.class,()->search.search("changed",null,HistorySearchService.Kind.ALL,first.nextCursor(),3));
  assertThrows(IllegalArgumentException.class,()->search.search("history-page",project,HistorySearchService.Kind.ALL,first.nextCursor(),3));
 }
 @Test void httpSearchAndHistoryRespectOwnershipAndDoNotExposePrivateSnapshotsOrRawEvents(){
  var task=task("private-search-needle","answer");task.captureAgent("private-role-secret");taskRepository.saveAndFlush(task);
  events.saveAndFlush(new TaskEventEntity(task.getId(),1,1,null,"task.started","{\"private\":\"event-secret\"}",Instant.now()));
  events.saveAndFlush(new TaskEventEntity(task.getId(),1,2,null,"agent.message.delta","{\"delta\":\"noise\"}",Instant.now()));
  var artifact=new TaskArtifactEntity(UUID.randomUUID(),task.getId(),1,"recovery","abc","branch","private-patch-needle","{}",Instant.now());artifacts.saveAndFlush(artifact);
  var response=get("/api/history/search?query=private-search-needle",token);assertEquals(HttpStatus.OK,response.getStatusCode());assertEquals("no-store",response.getHeaders().getCacheControl());assertFalse(response.getBody().toString().contains("private-role-secret"));
  var history=get("/api/tasks/"+task.getId()+"/history",token);assertEquals(1,history.getBody().path("entries").size());assertFalse(history.getBody().toString().contains("event-secret"));assertFalse(history.getBody().toString().contains("private-patch-needle"));assertEquals(artifact.getId().toString(),history.getBody().path("artifacts").get(0).path("id").asText());
  assertTrue(get("/api/tasks/"+task.getId()+"/artifacts/by-id/"+artifact.getId(),token).getBody().path("patch").asText().contains("private-patch-needle"));
  var foreign=accounts.register("history-other-"+UUID.randomUUID(),"fixture-password");assertTrue(get("/api/history/search?query=private-search-needle",foreign.token()).getBody().path("items").isEmpty());
  assertEquals(HttpStatus.NOT_FOUND,get("/api/history/search?projectId="+project,foreign.token()).getStatusCode());assertEquals(HttpStatus.NOT_FOUND,get("/api/tasks/"+task.getId()+"/history",foreign.token()).getStatusCode());assertEquals(HttpStatus.NOT_FOUND,get("/api/tasks/"+task.getId()+"/artifacts/by-id/"+artifact.getId(),foreign.token()).getStatusCode());
 }
 @Test void equalMicrosecondTimestampsKeepEveryKindAndUuidExactlyOnce(){
  for(int i=0;i<3;i++){
   var chat=scopes.createConversation(project,scope.workspaceId(),"tie-break chat "+i);scope=new WorkspaceService.Scope(project,scope.workspaceId(),chat.getId());
   task("tie-break prompt "+i,"tie-break answer "+i);
  }
  var instant=java.sql.Timestamp.from(Instant.parse("2026-01-01T12:34:56.123456Z"));
  for(String table:List.of("conversations","conversation_messages","tasks"))jdbc.update("update "+table+" set created_at=? where project_id=?",instant,project);
  var expected=search.search("tie-break",null,HistorySearchService.Kind.ALL,null,50).items();assertEquals(12,expected.size());
  for(int size:List.of(1,2,3,5)){
   var actual=new ArrayList<HistorySearchService.Hit>();String cursor=null;int pages=0;
   do{assertTrue(pages++<20);var page=search.search("tie-break",null,HistorySearchService.Kind.ALL,cursor,size);actual.addAll(page.items());cursor=page.nextCursor();}while(cursor!=null);
   assertEquals(expected,actual,"stable order for page size "+size);
  }
 }
 @Test void recoveryTimelinePaginatesAcrossAttemptsAndArtifactIdCannotCrossTasks(){
  var task=task("recover fixture","answer");task.claim(UUID.randomUUID(),Instant.now());task.fail("fixture");task.retry();taskRepository.saveAndFlush(task);
  for(int i=1;i<=5;i++)events.saveAndFlush(new TaskEventEntity(task.getId(),i<4?1:2,i,null,i%2==0?"checkpoint.created":"task.started","{}",Instant.now()));
  var old=new TaskArtifactEntity(UUID.randomUUID(),task.getId(),1,"checkpoint","old","old-branch","old patch","{}",Instant.now());artifacts.saveAndFlush(old);
  var page=get("/api/tasks/"+task.getId()+"/history?limit=2",token).getBody();assertEquals(2,page.path("currentAttempt").asInt());assertEquals(5,page.path("entries").get(0).path("sequence").asLong());assertEquals(4,page.path("nextBeforeSequence").asLong());
  page=get("/api/tasks/"+task.getId()+"/history?limit=2&beforeSequence=4",token).getBody();assertEquals(3,page.path("entries").get(0).path("sequence").asLong());assertEquals(old.getId().toString(),page.path("artifacts").get(0).path("id").asText());
  var other=task("other task","answer");assertEquals(HttpStatus.NOT_FOUND,get("/api/tasks/"+other.getId()+"/artifacts/by-id/"+old.getId(),token).getStatusCode());
  assertEquals(HttpStatus.BAD_REQUEST,get("/api/tasks/"+task.getId()+"/history?limit=101",token).getStatusCode());
 }
}
