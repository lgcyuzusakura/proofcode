package dev.proofcode.control.task;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import dev.proofcode.control.outbox.OutboxRepository;
import dev.proofcode.control.project.ProjectController;
import dev.proofcode.control.project.ProjectEntity;
import dev.proofcode.control.project.ProjectRepository;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.time.Instant;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.support.TransactionTemplate;
import org.springframework.web.server.ResponseStatusException;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.*;

@SpringBootTest(properties = "spring.task.scheduling.enabled=false")
class TaskWorkflowIntegrationTest {
    @MockBean JmsTemplate jmsTemplate;
    @MockBean StringRedisTemplate redisTemplate;
    @MockBean TaskSocketHandler sockets;
    @Autowired ProjectRepository projects;
    @Autowired ProjectController projectApi;
    @Autowired TaskRepository tasks;
    @Autowired TaskEventRepository events;
    @Autowired TaskArtifactRepository artifacts;
    @Autowired TaskApprovalRepository approvals;
    @Autowired OutboxRepository outbox;
    @Autowired TaskService service;
    @Autowired InternalEventController ingestion;
    @Autowired RunnerLeaseController leases;
    @Autowired TaskController taskApi;
    @Autowired ObjectMapper json;
    @Autowired PlatformTransactionManager transactions;

    @Test
    void persistsApprovalResumeAndArtifactsWithGlobalEventOrdering() throws Exception {
        UUID projectId=UUID.randomUUID();
        projects.save(new ProjectEntity(projectId,"workflow","https://example.com/repo.git","main",Instant.now()));
        UUID taskId=service.create(projectId,"Fix the test","test-model").getId();
        assertTrue(json.readTree(outbox.findAll().get(0).getPayload()).isObject());

        UUID runner=UUID.randomUUID();
        assertEquals(202,leases.claim(taskId,new RunnerLeaseController.LeaseRequest(runner,1)).getStatusCode().value());
        assertEquals(202,ingestion.ingest(taskId,event(taskId,"run-a",1,"task.started",json.createObjectNode())).getStatusCode().value());
        assertEquals(204,ingestion.ingest(taskId,event(taskId,"run-a",1,"task.started",json.createObjectNode())).getStatusCode().value());
        assertEquals(1,events.findTop500ByTaskIdOrderBySequenceAsc(taskId).size());

        ObjectNode request=json.createObjectNode().put("callId","call-1").put("tool","exec_command").put("risk","high");
        request.putObject("arguments").put("cmd","go test ./...");
        assertEquals(202,ingestion.ingest(taskId,event(taskId,"run-a",2,"tool.approval_required",request)).getStatusCode().value());
        assertEquals(TaskStatus.WAITING_APPROVAL,tasks.findById(taskId).orElseThrow().getStatus());
        TaskApprovalEntity approval=approvals.findByTaskIdAndCallId(taskId,"call-1").orElseThrow();
        assertTrue(json.readTree(approval.getArguments()).isObject());
        assertEquals(409,ingestion.ingest(taskId,event(taskId,"run-a",3,"task.started",json.createObjectNode())).getStatusCode().value());
        assertEquals(TaskStatus.WAITING_APPROVAL,tasks.findById(taskId).orElseThrow().getStatus());

        long outboxBefore=outbox.count();
        assertEquals(ApprovalStatus.APPROVED,service.decideApproval(taskId,approval.getId(),true).getStatus());
        assertEquals(outboxBefore+1,outbox.count());
        assertEquals(ApprovalStatus.APPROVED,service.decideApproval(taskId,approval.getId(),true).getStatus());
        assertEquals(outboxBefore+1,outbox.count());
        ResponseStatusException conflict=assertThrows(ResponseStatusException.class,()->service.decideApproval(taskId,approval.getId(),false));
        assertEquals(HttpStatus.CONFLICT,conflict.getStatusCode());
        assertEquals(TaskStatus.QUEUED,tasks.findById(taskId).orElseThrow().getStatus());

        assertEquals(202,leases.claim(taskId,new RunnerLeaseController.LeaseRequest(runner,1)).getStatusCode().value());
        assertEquals(202,ingestion.ingest(taskId,event(taskId,"run-b",1,"verification.completed",json.createObjectNode())).getStatusCode().value());
        assertEquals(TaskStatus.VERIFYING,tasks.findById(taskId).orElseThrow().getStatus());
        assertEquals(204,leases.renew(taskId,new RunnerLeaseController.LeaseRequest(runner,1)).getStatusCode().value());

        ObjectNode checkpoint=json.createObjectNode().put("patch","diff --git a/main.go b/main.go\n+new line\n").put("commit","abc123").put("branch","task-branch");
        assertEquals(202,ingestion.ingest(taskId,event(taskId,"run-b",2,"checkpoint.created",checkpoint)).getStatusCode().value());
        TaskArtifactEntity artifact=artifacts.findByTaskIdAndKind(taskId,"checkpoint").orElseThrow();
        assertEquals(checkpoint.get("patch").asText(),artifact.getPatch());
        assertTrue(json.readTree(artifact.getMetadata()).isObject());
        assertFalse(json.readTree(artifact.getMetadata()).has("patch"));
        var storedEvents=events.findTop500ByTaskIdOrderBySequenceAsc(taskId);
        assertEquals(4,storedEvents.size());
        assertEquals(4,storedEvents.get(3).getSequence());
        var publicEvent=json.valueToTree(storedEvents.get(0));
        assertEquals("v1",publicEvent.path("version").asText());
        assertEquals(runner.toString(),publicEvent.path("runnerId").asText());
        assertEquals("run-a",publicEvent.path("runId").asText());
        assertFalse(publicEvent.has("eventKey"));
        assertFalse(publicEvent.has("id"));
        assertFalse(json.readTree(storedEvents.get(3).getPayload()).has("patch"));

        assertEquals(202,ingestion.ingest(taskId,event(taskId,"run-b",3,"task.completed",json.createObjectNode().put("result","Done"))).getStatusCode().value());
        assertEquals(TaskStatus.SUCCEEDED,tasks.findById(taskId).orElseThrow().getStatus());
        verify(sockets,times(5)).broadcast(eq(taskId),anyString());
    }

    @Test
    void storesRecoveryPatchOutsideEventStream() throws Exception {
        UUID projectId=UUID.randomUUID();
        projects.save(new ProjectEntity(projectId,"recovery","https://example.com/repo.git","main",Instant.now()));
        UUID taskId=service.create(projectId,"Recover task","test-model").getId();
        assertEquals(202,leases.claim(taskId,new RunnerLeaseController.LeaseRequest(UUID.randomUUID(),1)).getStatusCode().value());
        assertEquals(TaskStatus.CANCELLED,service.cancel(taskId).getStatus());
        ObjectNode payload=json.createObjectNode().put("patch","diff --git a/file b/file\n").put("reason","uncertain command outcome");
        assertEquals(202,ingestion.ingest(taskId,event(taskId,"run-c",1,"recovery.created",payload)).getStatusCode().value());
        assertEquals(payload.get("patch").asText(),artifacts.findByTaskIdAndKind(taskId,"recovery").orElseThrow().getPatch());
        assertFalse(json.readTree(events.findTop500ByTaskIdOrderBySequenceAsc(taskId).get(0).getPayload()).has("patch"));
    }

    @Test
    void broadcastsOnlyAfterCommitAndNeverAfterRollback() {
        UUID projectId=UUID.randomUUID();
        projects.save(new ProjectEntity(projectId,"events","https://example.com/repo.git","main",Instant.now()));
        UUID taskId=service.create(projectId,"Track events","test-model").getId();
        assertEquals(202,leases.claim(taskId,new RunnerLeaseController.LeaseRequest(UUID.randomUUID(),1)).getStatusCode().value());
        TransactionTemplate transaction=new TransactionTemplate(transactions);

        transaction.execute(status->{
            ingestion.ingest(taskId,event(taskId,"run-rollback",1,"task.started",json.createObjectNode()));
            verify(sockets,never()).broadcast(eq(taskId),anyString());
            status.setRollbackOnly();
            return null;
        });
        assertEquals(0,events.findTop500ByTaskIdOrderBySequenceAsc(taskId).size());
        verify(sockets,never()).broadcast(eq(taskId),anyString());

        transaction.execute(status->{
            ingestion.ingest(taskId,event(taskId,"run-commit",1,"task.started",json.createObjectNode()));
            verify(sockets,never()).broadcast(eq(taskId),anyString());
            return null;
        });
        assertEquals(1,events.findTop500ByTaskIdOrderBySequenceAsc(taskId).size());
        verify(sockets,times(1)).broadcast(eq(taskId),anyString());
    }

    @Test
    void retryKeepsHistoryButRejectsStaleRunnerAndApproval() {
        UUID projectId=UUID.randomUUID();
        projects.save(new ProjectEntity(projectId,"retry","https://example.com/repo.git","main",Instant.now()));
        UUID taskId=service.create(projectId,"Retry safely","test-model").getId();
        UUID oldRunner=UUID.randomUUID();
        assertEquals(202,leases.claim(taskId,new RunnerLeaseController.LeaseRequest(oldRunner,1)).getStatusCode().value());
        ingestion.ingest(taskId,event(taskId,"same-run",1,"task.started",json.createObjectNode()));
        ObjectNode oldPatch=json.createObjectNode().put("patch","old patch").put("commit","old");
        ingestion.ingest(taskId,event(taskId,"same-run",2,"checkpoint.created",oldPatch));
        ObjectNode approvalPayload=json.createObjectNode().put("callId","same-call").put("tool","exec_command").put("risk","exec");
        approvalPayload.putObject("arguments").put("cmd","go test");
        ingestion.ingest(taskId,event(taskId,"same-run",3,"tool.approval_required",approvalPayload));
        UUID oldApproval=approvals.findByTaskIdAndAttemptAndCallId(taskId,1,"same-call").orElseThrow().getId();
        assertEquals(TaskStatus.CANCELLED,service.cancel(taskId).getStatus());
        assertEquals(ApprovalStatus.CANCELLED,approvals.findById(oldApproval).orElseThrow().getStatus());
        assertEquals(409,ingestion.ingest(taskId,event(taskId,oldRunner,"same-run",1,4,"task.completed",json.createObjectNode())).getStatusCode().value());
        assertEquals(TaskStatus.CANCELLED,tasks.findById(taskId).orElseThrow().getStatus());

        assertEquals(2,service.retry(taskId).getAttempt());
        assertTrue(taskApi.artifactList(taskId,false).isEmpty());
        assertEquals(1,taskApi.artifactList(taskId,true).size());
        assertEquals(1,taskApi.artifactList(taskId,true).get(0).attempt());
        assertEquals(404,taskApi.artifact(taskId,"checkpoint").getStatusCode().value());
        assertTrue(taskApi.approvalList(taskId,false).isEmpty());
        assertEquals(1,taskApi.approvalList(taskId,true).size());
        assertEquals(HttpStatus.CONFLICT,assertThrows(ResponseStatusException.class,()->service.decideApproval(taskId,oldApproval,true)).getStatusCode());

        assertEquals(409,leases.claim(taskId,new RunnerLeaseController.LeaseRequest(oldRunner,1)).getStatusCode().value());
        assertEquals(409,leases.renew(taskId,new RunnerLeaseController.LeaseRequest(oldRunner,1)).getStatusCode().value());
        assertEquals(409,ingestion.ingest(taskId,event(taskId,oldRunner,"same-run",1,1,"task.started",json.createObjectNode())).getStatusCode().value());
        assertEquals(3,events.findTop500ByTaskIdOrderBySequenceAsc(taskId).size());

        UUID newRunner=UUID.randomUUID();
        assertEquals(202,leases.claim(taskId,new RunnerLeaseController.LeaseRequest(newRunner,2)).getStatusCode().value());
        assertEquals(202,ingestion.ingest(taskId,event(taskId,"same-run",2,1,"task.started",json.createObjectNode())).getStatusCode().value());
        ObjectNode newPatch=json.createObjectNode().put("patch","new patch").put("commit","new");
        assertEquals(202,ingestion.ingest(taskId,event(taskId,"same-run",2,2,"checkpoint.created",newPatch)).getStatusCode().value());
        assertEquals("new patch",taskApi.artifact(taskId,"checkpoint").getBody().getPatch());
        assertEquals(2,taskApi.artifactList(taskId,true).size());
        assertEquals(1,taskApi.artifactList(taskId,false).size());
        assertEquals(2,taskApi.artifactList(taskId,false).get(0).attempt());
        assertEquals(202,ingestion.ingest(taskId,event(taskId,"same-run",2,3,"tool.approval_required",approvalPayload)).getStatusCode().value());
        TaskApprovalEntity newApproval=approvals.findByTaskIdAndAttemptAndCallId(taskId,2,"same-call").orElseThrow();
        assertNotEquals(oldApproval,newApproval.getId());
        assertEquals(1,taskApi.approvalList(taskId,false).size());
        assertEquals(6,events.findTop500ByTaskIdOrderBySequenceAsc(taskId).size());
    }

    @Test
    void rejectsEventsFromRunnerWhoseLeaseWasTakenOver() {
        UUID projectId=UUID.randomUUID();
        projects.save(new ProjectEntity(projectId,"lease","https://example.com/repo.git","main",Instant.now()));
        UUID taskId=service.create(projectId,"Check lease","test-model").getId();
        UUID firstRunner=UUID.randomUUID();
        UUID secondRunner=UUID.randomUUID();
        assertEquals(202,leases.claim(taskId,new RunnerLeaseController.LeaseRequest(firstRunner,1)).getStatusCode().value());
        ingestion.ingest(taskId,event(taskId,firstRunner,"old-run",1,1,"task.started",json.createObjectNode()));

        new TransactionTemplate(transactions).execute(status->{
            assertTrue(tasks.lockById(taskId).orElseThrow().claim(secondRunner,Instant.now().plusSeconds(60)));
            return null;
        });

        assertEquals(409,ingestion.ingest(taskId,event(taskId,firstRunner,"old-run",1,2,"task.completed",json.createObjectNode())).getStatusCode().value());
        assertEquals(1,events.findTop500ByTaskIdOrderBySequenceAsc(taskId).size());
        assertEquals(202,ingestion.ingest(taskId,event(taskId,secondRunner,"new-run",1,1,"task.started",json.createObjectNode())).getStatusCode().value());
        assertEquals(2,events.findTop500ByTaskIdOrderBySequenceAsc(taskId).size());
    }

    @Test
    void invalidApprovalEventDoesNotStrandTask() {
        UUID projectId=UUID.randomUUID();
        projects.save(new ProjectEntity(projectId,"approval-validation","https://example.com/repo.git","main",Instant.now()));
        UUID taskId=service.create(projectId,"Check approval","test-model").getId();
        UUID runner=UUID.randomUUID();
        assertEquals(202,leases.claim(taskId,new RunnerLeaseController.LeaseRequest(runner,1)).getStatusCode().value());
        ObjectNode invalid=json.createObjectNode().put("callId","call-1").put("tool","run_command").put("risk","exec");
        assertThrows(IllegalArgumentException.class,()->ingestion.ingest(taskId,event(taskId,runner,"run",1,1,"tool.approval_required",invalid)));
        assertEquals(TaskStatus.RUNNING,tasks.findById(taskId).orElseThrow().getStatus());
        assertTrue(approvals.findByTaskIdAndAttemptOrderByCreatedAtDesc(taskId,1).isEmpty());
        assertTrue(events.findTop500ByTaskIdOrderBySequenceAsc(taskId).isEmpty());
    }

    @Test
    void preservesLargePatchArgumentsForApprovalReview() throws Exception {
        UUID projectId=UUID.randomUUID();
        projects.save(new ProjectEntity(projectId,"large-approval","https://example.com/repo.git","main",Instant.now()));
        UUID taskId=service.create(projectId,"Review patch","test-model").getId();
        UUID runner=UUID.randomUUID();
        assertEquals(202,leases.claim(taskId,new RunnerLeaseController.LeaseRequest(runner,1)).getStatusCode().value());
        ObjectNode request=json.createObjectNode().put("callId","large-call").put("tool","apply_patch").put("risk","write");
        request.putObject("arguments").put("patch","x".repeat((1<<20)+1));
        assertEquals(202,ingestion.ingest(taskId,event(taskId,runner,"large-run",1,1,"tool.approval_required",request)).getStatusCode().value());
        TaskApprovalEntity approval=approvals.findByTaskIdAndAttemptAndCallId(taskId,1,"large-call").orElseThrow();
        assertEquals((1<<20)+1,json.readTree(approval.getArguments()).path("patch").asText().length());
    }

    @Test
    void taskCreateIdempotencyPreservesOriginalRequest() {
        UUID projectId=UUID.randomUUID();
        projects.save(new ProjectEntity(projectId,"idempotency","https://example.com/repo.git","main",Instant.now()));
        var request=new TaskController.CreateTask(projectId,"Fix tests","test-model");
        var created=taskApi.create("same-key",request);
        assertEquals(201,created.getStatusCode().value());
        var repeated=taskApi.create("same-key",request);
        assertEquals(200,repeated.getStatusCode().value());
        assertEquals(created.getBody().getId(),repeated.getBody().getId());
        assertEquals(HttpStatus.CONFLICT,assertThrows(ResponseStatusException.class,
            ()->taskApi.create("same-key",new TaskController.CreateTask(projectId,"Change another file","test-model"))).getStatusCode());
        assertEquals(HttpStatus.CONFLICT,assertThrows(ResponseStatusException.class,
            ()->service.retry(created.getBody().getId())).getStatusCode());
    }

    @Test
    void projectRejectsMalformedAndCredentialBearingRepositoryUrls() {
        assertThrows(IllegalArgumentException.class,()->projectApi.create(new ProjectController.CreateProject("invalid","https:///missing-host","main")));
        assertThrows(IllegalArgumentException.class,()->projectApi.create(new ProjectController.CreateProject("secret","https://token@example.com/repo.git","main")));
    }

    private InternalEventController.RunnerEvent event(UUID taskId,String runId,long sequence,String type,ObjectNode payload) {
        return event(taskId,runId,1,sequence,type,payload);
    }

    private InternalEventController.RunnerEvent event(UUID taskId,String runId,int attempt,long sequence,String type,ObjectNode payload) {
        return event(taskId,tasks.findById(taskId).orElseThrow().getRunnerId(),runId,attempt,sequence,type,payload);
    }

    private InternalEventController.RunnerEvent event(UUID taskId,UUID runnerId,String runId,int attempt,long sequence,String type,ObjectNode payload) {
        return new InternalEventController.RunnerEvent("v1",taskId,runnerId,runId,attempt,sequence,type,Instant.now(),payload);
    }
}
