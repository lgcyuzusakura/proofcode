package dev.proofcode.control.data;

import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.node.ObjectNode;
import dev.proofcode.control.project.*;
import dev.proofcode.control.task.*;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.time.Instant;
import java.util.*;
import org.junit.jupiter.api.*;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.support.TransactionTemplate;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.web.server.ResponseStatusException;
import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.*;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;

@SpringBootTest(properties={"spring.task.scheduling.enabled=false","RUNNER_ALLOW_WRITE=true","RUNNER_ALLOW_EXEC=true"})
@AutoConfigureMockMvc
class DataOperationIntegrationTest {
    @MockBean JmsTemplate jms;
    @MockBean StringRedisTemplate redisTemplate;
    @MockBean TaskSocketHandler sockets;
    @MockBean PostgresDataAdapter postgres;
    @MockBean RedisDataAdapter redis;
    @Autowired DataOperationService service;
    @Autowired DataOperationRepository operations;
    @Autowired DataResourceRepository resources;
    @Autowired ProjectRepository projects;
    @Autowired TaskRepository tasks;
    @Autowired TaskService taskService;
    @Autowired TaskApprovalRepository taskApprovals;
    @Autowired InternalEventController events;
    @Autowired ObjectMapper json;
    @Autowired PlatformTransactionManager transactions;
    @Autowired MockMvc http;
    private UUID project,task;
    private DataResource resource;
    private String schemaVersion;
    @BeforeEach void setup()throws Exception{
        reset(postgres,redis);
        project=UUID.randomUUID();projects.save(new ProjectEntity(project,"data","https://example.com/data.git","main",Instant.now()));
        task=UUID.randomUUID();tasks.save(new TaskEntity(task,project,"Inspect data","test",Instant.now()));
        resource=service.register(project,json.readTree("{\"provider\":\"postgres\",\"environment\":\"dev\",\"secretRef\":\"TEST\"}"));
        JsonNode schema=json.readTree("{\"tables\":[{\"schema\":\"public\",\"name\":\"orders\",\"columns\":[{\"name\":\"id\"}]}]}");
        when(postgres.schema(any())).thenReturn(schema);
        when(postgres.compile(any(),any(),any())).thenAnswer(i->new DataAdapter.Compiled("mutation","UPDATE orders SET status = ?","",List.of(),i.getArgument(2)));
        when(postgres.execute(any(),any(),nullable(String.class),any())).thenReturn(new DataAdapter.Outcome("COMMITTED",Map.of("affectedRows",1),null));
        schemaVersion=(String)service.inspectSchema(task,1,resource.id).get("schemaVersion");
    }
    private ObjectNode request(){ObjectNode n=json.createObjectNode().put("resourceId",resource.id.toString()).put("attempt",1).put("schemaVersion",schemaVersion).put("idempotencyKey",UUID.randomUUID().toString());n.putObject("ir").put("kind","mutation").put("table","orders");return n;}
    private DataOperation plan(){return service.plan(task,request());}
    private void approve(DataOperation p){service.approveFromTool(task,1,"data_execute",json.createObjectNode().put("planId",p.id.toString()).put("digest",p.digest),true);}
    private void mutate(java.util.function.Consumer<DataOperation> f,UUID id){new TransactionTemplate(transactions).executeWithoutResult(s->{f.accept(operations.lockById(id).orElseThrow());});}
    @Test void pendingAndDeniedNeverExecuteEvenWithAutomaticRunnerFlags(){DataOperation p=plan();assertThrows(ResponseStatusException.class,()->service.executeApproved(task,1,p.id,p.digest));service.approveFromTool(task,1,"data_execute",json.createObjectNode().put("planId",p.id.toString()).put("digest",p.digest),false);assertThrows(ResponseStatusException.class,()->service.executeApproved(task,1,p.id,p.digest));verify(postgres,never()).execute(any(),any(),nullable(String.class),any());}
    @Test void executesOnceAfterApprovalAndReturnsOnlySummaryOnReplay(){DataOperation p=plan();approve(p);assertEquals("COMMITTED",service.executeApproved(task,1,p.id,p.digest).status());assertEquals("COMMITTED",service.executeApproved(task,1,p.id,p.digest).status());verify(postgres,times(1)).execute(any(),any(),nullable(String.class),any());assertTrue(service.audit(project).stream().anyMatch(a->a.action.equals("EXECUTION_STARTED")));}
    @Test void capabilityCannotBeGuessedAndExpires(){DataOperation p=plan();var approved=service.approve(project,p.id,p.digest,true);assertThrows(ResponseStatusException.class,()->service.executeWithCapability(project,1,p.id,p.digest,"guessed"));assertEquals("APPROVED",operations.findById(p.id).orElseThrow().status);mutate(o->o.capabilityExpiresAt=Instant.now().minusSeconds(1),p.id);assertThrows(ResponseStatusException.class,()->service.executeWithCapability(project,1,p.id,p.digest,approved.capability()));verify(postgres,never()).execute(any(),any(),nullable(String.class),any());}
    @Test void rejectsOtherProjectAndTask(){UUID other=UUID.randomUUID();projects.save(new ProjectEntity(other,"other","https://example.com/other.git","main",Instant.now()));UUID otherTask=UUID.randomUUID();tasks.save(new TaskEntity(otherTask,other,"other","test",Instant.now()));assertThrows(ResponseStatusException.class,()->service.plan(otherTask,request()));DataOperation p=plan();approve(p);assertThrows(ResponseStatusException.class,()->service.executeApproved(otherTask,1,p.id,p.digest));assertThrows(ResponseStatusException.class,()->service.approve(other,p.id,p.digest,true));verify(postgres,never()).execute(any(),any(),nullable(String.class),any());}
    @Test void rejectsDigestTamperOldAttemptAndTerminalTask(){DataOperation p=plan();approve(p);mutate(o->o.canonicalIr="{\"kind\":\"mutation\",\"table\":\"other\"}",p.id);assertThrows(ResponseStatusException.class,()->service.executeApproved(task,1,p.id,p.digest));DataOperation clean=plan();approve(clean);assertThrows(ResponseStatusException.class,()->service.executeApproved(task,2,clean.id,clean.digest));new TransactionTemplate(transactions).executeWithoutResult(s->tasks.lockById(task).orElseThrow().transition(TaskStatus.CANCELLED));assertThrows(ResponseStatusException.class,()->service.executeApproved(task,1,clean.id,clean.digest));verify(postgres,never()).execute(any(),any(),nullable(String.class),any());}
    @Test void schemaDriftIsRecordedBeforeAnyMutation()throws Exception{DataOperation p=plan();approve(p);when(postgres.schema(any())).thenReturn(json.readTree("{\"tables\":[]}"));assertEquals("FAILED_PRECONDITION",service.executeApproved(task,1,p.id,p.digest).status());verify(postgres,never()).execute(any(),any(),nullable(String.class),any());assertEquals("FAILED_PRECONDITION",operations.findById(p.id).orElseThrow().status);}
    @Test void unknownResultIsDurableAndCannotReplay(){DataOperation p=plan();approve(p);when(postgres.execute(any(),any(),nullable(String.class),any())).thenThrow(new IllegalStateException("connection lost"));assertEquals("COMMIT_UNKNOWN",service.executeApproved(task,1,p.id,p.digest).status());assertThrows(ResponseStatusException.class,()->service.executeApproved(task,1,p.id,p.digest));verify(postgres,times(1)).execute(any(),any(),nullable(String.class),any());assertTrue(service.audit(project).stream().anyMatch(a->a.status.equals("COMMIT_UNKNOWN")));}
    @Test void sensitiveResultIsReturnedButNeverStoredInAuditOrPublicLedger(){DataOperation p=plan();approve(p);when(postgres.execute(any(),any(),nullable(String.class),any())).thenReturn(new DataAdapter.Outcome("SUCCEEDED",Map.of("rows",List.of(Map.of("secret","private-user-data")),"returnedRows",1),null));var result=service.executeApproved(task,1,p.id,p.digest);assertTrue(result.result().containsKey("rows"));assertFalse(operations.findById(p.id).orElseThrow().resultSummary.contains("private-user-data"));assertFalse(service.audit(project).stream().anyMatch(a->a.details.contains("private-user-data")));JsonNode publicResource=json.valueToTree(resource);assertFalse(publicResource.has("secretRef"));JsonNode publicPlan=json.valueToTree(operations.findById(p.id).orElseThrow());assertFalse(publicPlan.has("canonicalIr"));assertFalse(publicPlan.has("capabilityHash"));}
    @Test void planningIsIdempotentButCannotChangeApprovedIr(){ObjectNode input=request();DataOperation first=service.plan(task,input);assertEquals(first.id,service.plan(task,input).id);((ObjectNode)input.get("ir")).put("table","other");assertThrows(ResponseStatusException.class,()->service.plan(task,input));}
    @Test void compensationUsesSeparateApprovalAndAdapter()throws Exception{DataResource cache=service.register(project,json.readTree("{\"provider\":\"redis\",\"environment\":\"dev\",\"secretRef\":\"CACHE\"}"));when(redis.schema(any())).thenReturn(json.createObjectNode().put("namespace",project.toString()));when(redis.compile(any(),any(),any())).thenAnswer(i->new DataAdapter.Compiled("cache","SET namespaced key","",List.of(),i.getArgument(2)));when(redis.prepare(any(),any())).thenReturn("encrypted-before");when(redis.execute(any(),any(),anyString())).thenReturn(new DataAdapter.Outcome("COMMITTED",Map.of("affectedKeys",1),"encrypted-recovery"));ObjectNode input=request();input.put("resourceId",cache.id.toString());input.remove("schemaVersion");DataOperation original=service.plan(task,input);approve(original);assertEquals("COMMITTED",service.executeApproved(task,1,original.id,original.digest).status());DataOperation compensation=service.compensationPlan(project,original.id,"restore");assertEquals(compensation.id,service.compensationPlan(project,original.id,"restore").id);assertThrows(ResponseStatusException.class,()->service.executeApproved(task,1,compensation.id,compensation.digest));approve(compensation);when(redis.compensate(any(),eq("encrypted-recovery"))).thenReturn(new DataAdapter.Outcome("COMPENSATED",Map.of("restored",true),null));assertEquals("COMPENSATED",service.executeApproved(task,1,compensation.id,compensation.digest).status());verify(redis,times(1)).compensate(any(),eq("encrypted-recovery"));}
    @Test void runnerCannotApproveAndUserCannotCallInternalEndpoint()throws Exception{DataOperation p=plan();String body="{\"digest\":\""+p.digest+"\",\"approved\":true}";http.perform(post("/api/projects/"+project+"/data/plans/"+p.id+"/approval").header("Authorization","Bearer runner-token").contentType("application/json").content(body)).andExpect(status().isForbidden());http.perform(get("/internal/tasks/"+task+"/data/resources?attempt=1").header("Authorization","Bearer test-token")).andExpect(status().isForbidden());http.perform(post("/internal/tasks/"+task+"/data/plans/"+p.id+"/execute-approved").header("Authorization","Bearer runner-token").contentType("application/json").content("{\"attempt\":1,\"digest\":\""+p.digest+"\"}")).andExpect(status().isConflict());verify(postgres,never()).execute(any(),any(),nullable(String.class),any());}
    @Test void normalTaskApprovalAtomicallyApprovesPlanAndResumesTask(){DataOperation p=plan();UUID runner=UUID.randomUUID();new TransactionTemplate(transactions).executeWithoutResult(s->{TaskEntity t=tasks.lockById(task).orElseThrow();t.transition(TaskStatus.QUEUED);assertTrue(t.claim(runner,Instant.now()));});ObjectNode payload=json.createObjectNode().put("callId","data-call").put("tool","data_execute").put("risk","write");payload.set("arguments",json.createObjectNode().put("planId",p.id.toString()).put("digest",p.digest));events.ingest(task,new InternalEventController.RunnerEvent("v1",task,runner,"data-run",1,1,"tool.approval_required",Instant.now(),payload));UUID approval=taskApprovals.findByTaskIdAndAttemptAndCallId(task,1,"data-call").orElseThrow().getId();assertEquals(TaskStatus.WAITING_APPROVAL,tasks.findById(task).orElseThrow().getStatus());taskService.decideApproval(task,approval,true);assertEquals("APPROVED",operations.findById(p.id).orElseThrow().status);assertEquals(TaskStatus.QUEUED,tasks.findById(task).orElseThrow().getStatus());assertEquals("COMMITTED",service.executeApproved(task,1,p.id,p.digest).status());verify(postgres,times(1)).execute(any(),any(),nullable(String.class),any());}
    @Test void planInspectionAndExplainRequireTheExplicitCurrentAttempt()throws Exception{
        DataOperation old=plan();
        new TransactionTemplate(transactions).executeWithoutResult(s->{TaskEntity t=tasks.lockById(task).orElseThrow();t.transition(TaskStatus.CANCELLED);t.retry();});
        ObjectNode input=request().put("attempt",2);
        DataOperation fresh=service.plan(task,input);
        String base="/internal/tasks/"+task+"/data/plans/";
        String auth="Bearer runner-token";
        http.perform(get(base+fresh.id).header("Authorization",auth)).andExpect(status().isBadRequest());
        http.perform(get(base+fresh.id+"?attempt=1").header("Authorization",auth)).andExpect(status().isConflict());
        http.perform(get(base+old.id+"?attempt=2").header("Authorization",auth)).andExpect(status().isForbidden());
        http.perform(get(base+fresh.id+"?attempt=2").header("Authorization",auth)).andExpect(status().isOk()).andExpect(jsonPath("$.attempt").value(2));
        ObjectNode body=json.createObjectNode().put("digest",fresh.digest);
        http.perform(post(base+fresh.id+"/explain").header("Authorization",auth).contentType("application/json").content(body.toString())).andExpect(status().isBadRequest());
        body.put("attempt",1);
        http.perform(post(base+fresh.id+"/explain").header("Authorization",auth).contentType("application/json").content(body.toString())).andExpect(status().isConflict());
        body.put("attempt",2).put("digest",old.digest);
        http.perform(post(base+fresh.id+"/explain").header("Authorization",auth).contentType("application/json").content(body.toString())).andExpect(status().isConflict());
        verify(postgres,never()).explain(any(),any());
        body.put("digest",fresh.digest);
        when(postgres.explain(any(),any())).thenReturn(Map.of("plan",List.of("fixture plan")));
        http.perform(post(base+fresh.id+"/explain").header("Authorization",auth).contentType("application/json").content(body.toString())).andExpect(status().isOk()).andExpect(jsonPath("$.plan[0]").value("fixture plan"));
        verify(postgres,times(1)).explain(any(),any());
        verify(postgres,never()).execute(any(),any(),nullable(String.class),any());
    }
}
