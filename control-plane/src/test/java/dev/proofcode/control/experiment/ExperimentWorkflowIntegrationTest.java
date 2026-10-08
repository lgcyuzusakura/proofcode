package dev.proofcode.control.experiment;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import dev.proofcode.control.outbox.OutboxRepository;
import dev.proofcode.control.project.ProjectEntity;
import dev.proofcode.control.project.ProjectRepository;
import dev.proofcode.control.session.WorkspaceEntity;
import dev.proofcode.control.session.WorkspaceService;
import dev.proofcode.control.task.*;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.time.Instant;
import java.util.*;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.http.MediaType;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.web.server.ResponseStatusException;

import static org.junit.jupiter.api.Assertions.*;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;

@SpringBootTest(properties="spring.task.scheduling.enabled=false")
@AutoConfigureMockMvc
class ExperimentWorkflowIntegrationTest {
    private static final String COMMIT="0123456789abcdef0123456789abcdef01234567";
    @MockBean JmsTemplate jmsTemplate;
    @MockBean StringRedisTemplate redisTemplate;
    @MockBean TaskSocketHandler sockets;
    @Autowired MockMvc mvc;
    @Autowired ObjectMapper json;
    @Autowired ProjectRepository projects;
    @Autowired TaskRepository tasks;
    @Autowired OutboxRepository outbox;
    @Autowired ExperimentService experiments;
    @Autowired WorkspaceService scopes;
    @Autowired TaskService taskService;
    @Autowired RunnerLeaseController leases;
    @Autowired InternalEventController ingestion;

    @Test
    void createsRealFixedRevisionTasksForEveryGroupAndReplaysIdempotently() throws Exception {
        UUID project=project("fixed-inputs");
        ObjectNode request=request(project,2);
        long outboxBefore=outbox.count();
        JsonNode first=postExperiment(request,"replay-key",201);
        UUID experimentId=UUID.fromString(first.path("experiment").path("id").asText());
        assertEquals(12,first.path("runCount").asInt());
        assertEquals(6,first.path("groups").size());
        List<TaskEntity> created=tasks.findByExperimentIdOrderByCreatedAtAsc(experimentId);
        assertEquals(12,created.size());
        assertEquals(12,created.stream().map(TaskEntity::getId).distinct().count());
        assertEquals(12,created.stream().map(TaskEntity::getExperimentRunId).distinct().count());
        Set<UUID> taskIds=new HashSet<>();
        for(TaskEntity task:created){
            taskIds.add(task.getId());
            assertEquals(TaskStatus.QUEUED,task.getStatus());
            assertEquals(COMMIT,task.getSourceRevision());
            assertEquals(15,task.getMaxSteps());
            assertEquals(0.25,task.getTemperature());
            assertEquals("go test ./...",task.getTestCommand());
            assertEquals("Fix arithmetic",task.getPrompt());
            assertEquals("fixed-model",task.getModel());
            assertNotNull(task.getWorkspaceId());assertNotNull(task.getConversationId());
        }
        Map<ExperimentProfile.Group,Boolean> safety=Map.of(ExperimentProfile.Group.A,false,ExperimentProfile.Group.B,true,ExperimentProfile.Group.C,false,ExperimentProfile.Group.D,false,ExperimentProfile.Group.E,false,ExperimentProfile.Group.F,true);
        int queueMessages=0;
        for(var message:outbox.findAll()){
            if(!taskIds.contains(message.getAggregateId()))continue;
            queueMessages++;
            JsonNode payload=json.readTree(message.getPayload());
            assertEquals(COMMIT,payload.path("sourceRevision").asText());
            assertEquals("https://example.com/repo.git",payload.path("repositoryUrl").asText());
            assertEquals("",payload.path("branch").asText());
            assertEquals("go test ./...",payload.path("testCommand").asText());
            assertEquals(15,payload.path("maxSteps").asInt());
            assertEquals(0.25,payload.path("temperature").asDouble());
            JsonNode profile=payload.path("experimentProfile");
            var group=ExperimentProfile.Group.valueOf(payload.path("experimentGroup").asText());
            assertEquals(ExperimentProfile.VERSION,profile.path("version").asText());
            assertTrue(profile.path("mandatorySafety").asBoolean());
            assertEquals(safety.get(group),profile.path("deterministicSafety").asBoolean());
            assertEquals(group!=ExperimentProfile.Group.A,profile.path("toolsEnabled").asBoolean());
            assertEquals(group==ExperimentProfile.Group.C||group==ExperimentProfile.Group.F,profile.path("jevRequired").asBoolean());
            assertEquals(group==ExperimentProfile.Group.D||group==ExperimentProfile.Group.E||group==ExperimentProfile.Group.F,profile.path("ragEnabled").asBoolean());
            assertEquals(group==ExperimentProfile.Group.E||group==ExperimentProfile.Group.F,profile.path("contextCompressionEnabled").asBoolean());
            assertEquals(group==ExperimentProfile.Group.F,profile.path("feedbackRetrieval").asBoolean());
        }
        assertEquals(12,queueMessages);assertEquals(outboxBefore+12,outbox.count());
        JsonNode replay=postExperiment(request,"replay-key",200);
        assertEquals(experimentId.toString(),replay.path("experiment").path("id").asText());
        assertEquals(outboxBefore+12,outbox.count());
        request.put("testCommand","go test ./other");
        postExperiment(request,"replay-key",409);
        assertEquals(outboxBefore+12,outbox.count());
    }

    @Test
    void comparesActualTerminalEvidenceAndKeepsMissingAndConfigurationFailuresVisible() throws Exception {
        UUID project=project("real-evidence");
        JsonNode created=postExperiment(request(project,2),null,201);
        UUID experiment=UUID.fromString(created.path("experiment").path("id").asText());
        var createdTasks=tasks.findByExperimentIdOrderByCreatedAtAsc(experiment);
        int a=0;
        for(TaskEntity task:createdTasks){
            switch(task.getExperimentGroup()){
                case A -> terminal(task,"task.completed",evidence(true,COMMIT).put("durationMs",++a==1?10:100).put("testsPassed",a==2).put("taskCompleted",true));
                case B -> terminal(task,"task.completed",null);
                case C -> terminal(task,"task.failed",evidence(false,COMMIT).put("failureReason","Jev unavailable").put("taskCompleted",false));
                case D -> terminal(task,"task.completed",evidence(true,"f".repeat(40)).put("durationMs",1).put("testsPassed",true));
                case E -> terminal(task,"task.completed",evidence(true,COMMIT).put("tokensBeforeCompression",900).put("tokensAfterCompression",200).put("compressionTokenBasis","estimated").put("usageReported",false));
                case F -> { /* No terminal event: queued is unknown, never inferred as a pass. */ }
            }
        }
        var comparison=experiments.compare(project,experiment);
        assertEquals(12,comparison.runCount());
        var groups=new EnumMap<ExperimentProfile.Group,ExperimentService.GroupComparison>(ExperimentProfile.Group.class);
        comparison.groups().forEach(group->groups.put(group.group(),group));
        var groupA=groups.get(ExperimentProfile.Group.A);
        assertEquals(2,groupA.plannedRuns());assertEquals(2,groupA.terminalRuns());
        assertEquals(55.0,groupA.metrics().get("durationMs").mean());
        assertEquals(100.0,groupA.metrics().get("durationMs").p95());
        assertEquals(2,groupA.metrics().get("durationMs").sampleCount());
        assertEquals(1,groupA.booleanMetrics().get("testsPassed").trueCount());
        assertEquals(1,groupA.booleanMetrics().get("testsPassed").falseCount());
        assertEquals(.5,groupA.booleanMetrics().get("testsPassed").rate());
        var groupB=groups.get(ExperimentProfile.Group.B);
        assertEquals(2,groupB.terminalRuns());assertEquals(2,groupB.unknownResults());
        assertNull(groupB.metrics().get("durationMs").mean());
        assertNull(groupB.booleanMetrics().get("testsPassed").rate());
        var groupC=groups.get(ExperimentProfile.Group.C);
        assertEquals(2,groupC.failedRuns());assertEquals(2,groupC.configurationFailedRuns());
        assertEquals(0,groupC.validRuns());assertEquals(0.0,groupC.taskStatusSuccessRate());
        assertEquals("Jev unavailable",groupC.results().get(0).reason());
        assertNull(groups.get(ExperimentProfile.Group.D).metrics().get("durationMs").mean());
        assertEquals(2,groups.get(ExperimentProfile.Group.D).metrics().get("durationMs").missingCount());
        assertEquals(0,groups.get(ExperimentProfile.Group.D).booleanMetrics().get("testsPassed").trueCount());
        assertEquals(2,groups.get(ExperimentProfile.Group.D).booleanMetrics().get("testsPassed").unknownCount());
        assertNull(groups.get(ExperimentProfile.Group.D).booleanMetrics().get("testsPassed").rate());
        var groupE=groups.get(ExperimentProfile.Group.E);
        assertEquals(200.0,groupE.metrics().get("tokensAfterCompression").mean());
        assertEquals(2,groupE.booleanMetrics().get("testsPassed").unknownCount());
        assertEquals("estimated",groupE.results().get(0).observedResult().get("compressionTokenBasis"));
        assertEquals(false,groupE.results().get(0).observedResult().get("usageReported"));
        var groupF=groups.get(ExperimentProfile.Group.F);
        assertEquals(2,groupF.plannedRuns());assertEquals(0,groupF.terminalRuns());
        assertNull(groupF.taskStatusSuccessRate());assertNull(groupF.metrics().get("durationMs").p95());
        String csv=mvc.perform(get("/api/experiments/"+experiment+"/compare").param("projectId",project.toString()).param("format","csv").header("Authorization","Bearer test-token"))
            .andExpect(status().isOk()).andExpect(content().contentTypeCompatibleWith("text/csv")).andReturn().getResponse().getContentAsString();
        assertTrue(csv.contains("testsPassed"));assertTrue(csv.contains("Jev unavailable"));
        assertTrue(csv.contains("compressionTokenBasis"));assertTrue(csv.contains("estimated"));
        assertEquals(13,csv.lines().count());
    }

    @Test
    void newerAttemptCannotReuseOlderTerminalEvidenceAndCrossProjectScopeIsRejected() throws Exception {
        UUID project=project("retry");
        UUID other=project("other");
        var workspace=scopes.createWorkspace(other,"Private",WorkspaceEntity.Kind.LOCAL_FOLDER);
        var conversation=scopes.createConversation(other,workspace.getId(),"Only other");
        ObjectNode foreign=request(project,1).put("workspaceId",workspace.getId().toString()).put("conversationId",conversation.getId().toString());
        long before=outbox.count();postExperiment(foreign,null,404);assertEquals(before,outbox.count());
        JsonNode created=postExperiment(request(project,1),null,201);
        UUID experiment=UUID.fromString(created.path("experiment").path("id").asText());
        var task=tasks.findByExperimentIdOrderByCreatedAtAsc(experiment).stream().filter(t->t.getExperimentGroup()==ExperimentProfile.Group.C).findFirst().orElseThrow();
        terminal(task,"task.failed",evidence(false,COMMIT).put("failureReason","Jev missing"));
        ProjectEntity original=projects.findById(project).orElseThrow();
        projects.save(new ProjectEntity(project,original.getName(),"https://example.com/replaced.git","removed-branch",original.getCreatedAt()));
        taskService.retry(task.getId());
        var result=experiments.compare(project,experiment).groups().stream().filter(g->g.group()==ExperimentProfile.Group.C).findFirst().orElseThrow();
        assertEquals(0,result.terminalRuns());assertEquals("QUEUED",result.results().get(0).status());assertNull(result.results().get(0).profileApplied());
        assertEquals(2,result.results().get(0).attempt());
        List<JsonNode> retried=new ArrayList<>();
        for(var message:outbox.findAll()){
            if(message.getAggregateId().equals(task.getId()))retried.add(json.readTree(message.getPayload()));
        }
        assertEquals(2,retried.size());
        for(JsonNode payload:retried){
            assertEquals("https://example.com/repo.git",payload.path("repositoryUrl").asText());
            assertEquals("",payload.path("branch").asText());
            assertEquals(COMMIT,payload.path("sourceRevision").asText());
        }
        mvc.perform(get("/api/experiments/"+experiment).param("projectId",other.toString()).header("Authorization","Bearer test-token")).andExpect(status().isNotFound());
        mvc.perform(get("/api/projects/"+project+"/workspaces/"+workspace.getId()+"/conversations/"+conversation.getId()).header("Authorization","Bearer test-token")).andExpect(status().isNotFound());
    }

    @Test
    void legacyTaskApiGetsStableDefaultConversationAndInvalidControlsQueueNothing() throws Exception {
        UUID project=project("legacy");
        TaskEntity first=taskService.create(project,"One","model");
        TaskEntity second=taskService.create(project,"Two","model");
        assertEquals(first.getWorkspaceId(),second.getWorkspaceId());assertEquals(first.getConversationId(),second.getConversationId());
        assertEquals(1,scopes.listWorkspaces(project).size());
        assertEquals(1,scopes.listConversations(project,first.getWorkspaceId()).size());
        long before=outbox.count();
        ObjectNode invalid=request(project,1).put("baseCommit","main");
        postExperiment(invalid,null,400);
        invalid=request(project,21);
        postExperiment(invalid,null,400);
        invalid=request(project,1).put("temperature",3);
        postExperiment(invalid,null,400);
        assertEquals(before,outbox.count());
    }

    private UUID project(String name){
        UUID id=UUID.randomUUID();projects.save(new ProjectEntity(id,name,"https://example.com/repo.git","main",Instant.now()));return id;
    }
    private ObjectNode request(UUID project,int repetitions){
        return json.createObjectNode().put("projectId",project.toString()).put("name","Controlled comparison").put("prompt","Fix arithmetic").put("model","fixed-model").put("baseCommit",COMMIT).put("maxSteps",15).put("temperature",.25).put("testCommand","go test ./...").put("repetitions",repetitions);
    }
    private JsonNode postExperiment(ObjectNode request,String key,int expected) throws Exception{
        var builder=post("/api/experiments").header("Authorization","Bearer test-token").contentType(MediaType.APPLICATION_JSON).content(request.toString());
        if(key!=null)builder.header("Idempotency-Key",key);
        var response=mvc.perform(builder).andExpect(status().is(expected)).andReturn().getResponse();
        return expected>=200&&expected<300?json.readTree(response.getContentAsString()):json.createObjectNode();
    }
    private ObjectNode evidence(boolean applied,String source){
        return json.createObjectNode().put("profileVersion",ExperimentProfile.VERSION).put("profileApplied",applied).put("sourceRevision",source);
    }
    private void terminal(TaskEntity task,String type,ObjectNode evidence){
        UUID runner=UUID.randomUUID();int attempt=task.getAttempt();
        assertEquals(202,leases.claim(task.getId(),new RunnerLeaseController.LeaseRequest(runner,attempt)).getStatusCode().value());
        ObjectNode payload=json.createObjectNode().put("result","Finished").put("error","Failed");
        if(evidence!=null)payload.set("experimentResult",evidence);
        assertEquals(202,ingestion.ingest(task.getId(),new InternalEventController.RunnerEvent("v1",task.getId(),runner,"run-"+UUID.randomUUID(),attempt,1,type,Instant.now(),payload)).getStatusCode().value());
    }
}
