package dev.proofcode.control.data;

import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.node.ObjectNode;
import dev.proofcode.control.project.*;
import dev.proofcode.control.task.*;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.*;
import org.junit.jupiter.api.*;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.mock.mockito.*;
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

@SpringBootTest(properties={"spring.task.scheduling.enabled=false"})
@AutoConfigureMockMvc
class DataSessionIntegrationTest {
    @MockBean JmsTemplate jms;
    @MockBean StringRedisTemplate redisTemplate;
    @MockBean TaskSocketHandler sockets;
    @MockBean PostgresDataAdapter postgres;
    @MockBean RedisDataAdapter redis;
    @SpyBean DataAuditRepository audits;
    @Autowired DataOperationService service;
    @Autowired DataOperationRepository operations;
    @Autowired DataSessionRepository sessions;
    @Autowired ProjectRepository projects;
    @Autowired TaskRepository tasks;
    @Autowired ObjectMapper json;
    @Autowired PlatformTransactionManager manager;
    @Autowired MockMvc http;
    private UUID project;
    private DataResource resource;
    private DataSession session;
    private String schema;
    @BeforeEach void setup()throws Exception{
        project=UUID.randomUUID();projects.save(new ProjectEntity(project,"data-only","https://example.com/test.git","main",Instant.now()));
        resource=service.register(project,json.readTree("{\"name\":\"Orders\",\"provider\":\"postgres\",\"environment\":\"dev\",\"secretRef\":\"PG\"}"));
        session=service.createSession(project,json.readTree("{\"name\":\"Table editor\"}"));
        when(postgres.schema(any())).thenReturn(json.readTree("{\"tables\":[]}"));
        when(postgres.compile(any(),any(),any())).thenAnswer(i->new DataAdapter.Compiled("mutation","UPDATE bounded row","",List.of(),i.getArgument(2)));
        when(postgres.execute(any(),any(),nullable(String.class),any())).thenReturn(new DataAdapter.Outcome("COMMITTED",Map.of("affectedRows",1),"sealed-recovery"));
        schema=service.inspectSessionSchema(project,session.id,resource.id).get("schemaVersion").toString();
    }
    private ObjectNode input(){ObjectNode n=json.createObjectNode().put("resourceId",resource.id.toString()).put("resourceVersion",resource.resourceVersion).put("schemaVersion",schema).put("idempotencyKey",UUID.randomUUID().toString());n.putObject("ir").put("kind","mutation").put("table","orders");return n;}
    private DataOperation plan(){return service.planInSession(project,session.id,input());}
    private void neverExecuted(){verify(postgres,never()).execute(any(),any(),nullable(String.class),any());verify(redis,never()).execute(any(),any(),nullable(String.class));}
    @Test void standaloneSessionHasNoProgrammingTaskAndPersistsApprovalBeforeExecution(){DataOperation p=plan();assertNull(p.taskId);assertEquals(session.id,p.dataSessionId);assertEquals(0,p.attempt);assertThrows(ResponseStatusException.class,()->service.executeWithCapability(project,0,p.id,p.digest,"forged"));neverExecuted();var approval=service.approve(project,p.id,p.digest,true);assertEquals("COMMITTED",service.executeWithCapability(project,0,p.id,p.digest,approval.capability()).status());assertEquals("COMMITTED",service.executeWithCapability(project,0,p.id,p.digest,approval.capability()).status());verify(postgres,times(1)).execute(any(),any(),nullable(String.class),any());assertTrue(service.audit(project).stream().anyMatch(a->a.action.equals("USER_APPROVED")&&a.status.equals("APPROVED")));assertTrue(service.audit(project).stream().anyMatch(a->a.action.equals("EXECUTION_STARTED")&&a.status.equals("EXECUTING")));}
    @Test void resourceRegistrationAcceptsBoundedSchemaArraysAndRedisEmptyAllowlist()throws Exception{DataResource multi=service.register(project,json.readTree("{\"name\":\"Reporting\",\"provider\":\"postgres\",\"environment\":\"dev\",\"secretRef\":\"PG\",\"allowedSchema\":[\"public\",\"reporting\",\"public\"]}"));assertEquals("[\"public\",\"reporting\"]",multi.allowedSchemas);DataResource cache=service.register(project,json.readTree("{\"name\":\"Cache\",\"provider\":\"redis\",\"environment\":\"dev\",\"secretRef\":\"REDIS\",\"allowedSchema\":[]}"));assertEquals("[]",cache.allowedSchemas);ObjectNode changed=json.createObjectNode().put("expectedVersion",multi.resourceVersion);changed.putArray("allowedSchema").add("public");assertEquals("[\"public\"]",service.updateResource(project,multi.id,changed).allowedSchemas);ObjectNode invalid=json.createObjectNode().put("provider","postgres").put("environment","dev").put("secretRef","PG");invalid.putArray("allowedSchema");assertThrows(ResponseStatusException.class,()->service.register(project,invalid));invalid.withArray("allowedSchema").add("public;drop");assertThrows(ResponseStatusException.class,()->service.register(project,invalid));}
    @Test void resourceConfigurationAndPolicyVersionsInvalidateAnExistingApproval(){DataOperation p=plan();var approval=service.approve(project,p.id,p.digest,true);DataResource updated=service.updateResource(project,resource.id,json.createObjectNode().put("expectedVersion",resource.resourceVersion).put("name","Orders v2"));assertTrue(updated.resourceVersion>resource.resourceVersion);assertThrows(ResponseStatusException.class,()->service.executeWithCapability(project,0,p.id,p.digest,approval.capability()));neverExecuted();assertThrows(ResponseStatusException.class,()->service.updateResource(project,resource.id,json.createObjectNode().put("expectedVersion",resource.resourceVersion).put("active",false)));}
    @Test void closedExpiredAndWrongProjectSessionRejectEveryExecution(){DataOperation p=plan();var approval=service.approve(project,p.id,p.digest,true);UUID other=UUID.randomUUID();projects.save(new ProjectEntity(other,"other","https://example.com/other.git","main",Instant.now()));assertThrows(ResponseStatusException.class,()->service.inspectSessionSchema(other,session.id,resource.id));assertThrows(ResponseStatusException.class,()->service.planInSession(other,session.id,input()));assertThrows(ResponseStatusException.class,()->service.executeWithCapability(other,0,p.id,p.digest,approval.capability()));service.closeSession(project,session.id);assertThrows(ResponseStatusException.class,()->service.executeWithCapability(project,0,p.id,p.digest,approval.capability()));new TransactionTemplate(manager).executeWithoutResult(s->{DataSession expired=sessions.lockById(session.id).orElseThrow();expired.status="ACTIVE";expired.expiresAt=Instant.now().minusSeconds(1);});assertThrows(ResponseStatusException.class,()->service.executeWithCapability(project,0,p.id,p.digest,approval.capability()));neverExecuted();}
    @Test void unavailableStartAuditPreventsAnyExternalCallAndPreservesApproval(){DataOperation p=plan();var approval=service.approve(project,p.id,p.digest,true);doThrow(new IllegalStateException("ledger unavailable")).when(audits).save(argThat(a->a!=null&&a.action.equals("EXECUTION_STARTED")));assertThrows(IllegalStateException.class,()->service.executeWithCapability(project,0,p.id,p.digest,approval.capability()));neverExecuted();assertEquals("APPROVED",operations.findById(p.id).orElseThrow().status);assertNull(operations.findById(p.id).orElseThrow().capabilityUsedAt);}
    @Test void failedFinishAuditLeavesAnUnreplayableExecutingRecord(){DataOperation p=plan();var approval=service.approve(project,p.id,p.digest,true);doThrow(new IllegalStateException("ledger unavailable")).when(audits).save(argThat(a->a!=null&&a.action.equals("EXECUTION_FINISHED")));assertThrows(IllegalStateException.class,()->service.executeWithCapability(project,0,p.id,p.digest,approval.capability()));assertEquals("EXECUTING",operations.findById(p.id).orElseThrow().status);assertThrows(ResponseStatusException.class,()->service.executeWithCapability(project,0,p.id,p.digest,approval.capability()));verify(postgres,times(1)).execute(any(),any(),nullable(String.class),any());}
    @Test void terminalProgrammingTaskRecoveryGetsANewSessionAndRequiresNewApproval()throws Exception{
        UUID task=UUID.randomUUID();tasks.save(new TaskEntity(task,project,"change row","fixture",Instant.now()));ObjectNode request=input().put("attempt",1);DataOperation original=service.plan(task,request);var approval=service.approve(project,original.id,original.digest,true);assertEquals("COMMITTED",service.executeWithCapability(project,1,original.id,original.digest,approval.capability()).status());new TransactionTemplate(manager).executeWithoutResult(s->tasks.lockById(task).orElseThrow().transition(TaskStatus.CANCELLED));
        DataOperation reverse=service.compensationPlan(project,original.id,"undo-after-finish");assertNull(reverse.taskId);assertNotNull(reverse.dataSessionId);assertEquals("RECOVERY",service.getSession(project,reverse.dataSessionId).purpose);assertEquals(reverse.id,service.compensationPlan(project,original.id,"undo-after-finish").id);assertThrows(ResponseStatusException.class,()->service.executeWithCapability(project,0,reverse.id,reverse.digest,"forged"));verify(postgres,never()).compensate(any(),anyString());when(postgres.compensate(any(),eq("sealed-recovery"))).thenReturn(new DataAdapter.Outcome("COMPENSATED",Map.of("restored",true),null));var second=service.approve(project,reverse.id,reverse.digest,true);assertEquals("COMPENSATED",service.executeWithCapability(project,0,reverse.id,reverse.digest,second.capability()).status());verify(postgres,times(1)).compensate(any(),eq("sealed-recovery"));
    }
    @Test void httpSessionPlanContractKeepsSecretsPrivateAndRunnerCannotCreateManagementSessions()throws Exception{
        String base="/api/projects/"+project+"/data";
        http.perform(get(base+"/resources").header("Authorization","Bearer test-token")).andExpect(status().isOk()).andExpect(jsonPath("$[0].name").value("Orders")).andExpect(jsonPath("$[0].resourceVersion").value(0)).andExpect(jsonPath("$[0].secretRef").doesNotExist());
        http.perform(post(base+"/sessions").header("Authorization","Bearer runner-token").contentType("application/json").content("{\"name\":\"forged\"}")).andExpect(status().isForbidden());
        DataOperation p=plan();var approval=service.approve(project,p.id,p.digest,true);
        http.perform(post(base+"/plans/"+p.id+"/execute").header("Authorization","Bearer test-token").contentType("application/json").content(json.createObjectNode().put("digest",p.digest).put("capability",approval.capability()).toString())).andExpect(status().isOk()).andExpect(jsonPath("$.status").value("COMMITTED"));
        JsonNode exposed=json.valueToTree(operations.findById(p.id).orElseThrow());assertFalse(exposed.has("privateSnapshot"));assertFalse(exposed.has("capabilityHash"));assertFalse(exposed.has("canonicalIr"));
    }
    @Test void concurrentRecoveryRetriesProduceOnePlanAndOneIndependentRecoverySession()throws Exception{
        DataOperation original=plan();var approval=service.approve(project,original.id,original.digest,true);assertEquals("COMMITTED",service.executeWithCapability(project,0,original.id,original.digest,approval.capability()).status());service.closeSession(project,session.id);
        int before=service.sessions(project).size();ExecutorService workers=Executors.newFixedThreadPool(2);CountDownLatch ready=new CountDownLatch(2),start=new CountDownLatch(1);
        Callable<UUID> retry=()->{ready.countDown();assertTrue(start.await(10,TimeUnit.SECONDS));return service.compensationPlan(project,original.id,"parallel-undo").id;};
        try{Future<UUID> first=workers.submit(retry),second=workers.submit(retry);assertTrue(ready.await(10,TimeUnit.SECONDS));start.countDown();assertEquals(first.get(15,TimeUnit.SECONDS),second.get(15,TimeUnit.SECONDS));assertEquals(before+1,service.sessions(project).size());}
        finally{start.countDown();workers.shutdownNow();}
        verify(postgres,never()).compensate(any(),anyString());assertEquals("CLOSED",service.getSession(project,session.id).status);
    }
}
