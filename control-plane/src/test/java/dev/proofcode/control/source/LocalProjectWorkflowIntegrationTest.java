package dev.proofcode.control.source;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.outbox.OutboxRepository;
import dev.proofcode.control.project.ProjectController;
import dev.proofcode.control.session.ConversationMessageService;
import dev.proofcode.control.session.WorkspaceEntity;
import dev.proofcode.control.session.WorkspaceService;
import dev.proofcode.control.task.TaskRepository;
import dev.proofcode.control.task.TaskService;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.Base64;
import java.util.HexFormat;
import java.util.List;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.web.server.ResponseStatusException;

import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest(properties = "spring.task.scheduling.enabled=false")
class LocalProjectWorkflowIntegrationTest {
    @MockBean JmsTemplate jms;
    @MockBean StringRedisTemplate redis;
    @MockBean TaskSocketHandler sockets;
    @Autowired ProjectController projects;
    @Autowired WorkspaceService scopes;
    @Autowired SourceSnapshotService sources;
    @Autowired SourceSnapshotController sourceController;
    @Autowired ConversationMessageService messages;
    @Autowired TaskService tasks;
    @Autowired TaskRepository taskRepository;
    @Autowired OutboxRepository outbox;
    @Autowired ObjectMapper json;

    @Test
    void scratchRegistrationAndChatRetryKeepOneProjectAndOneTurn() throws Exception {
        String bootstrap = UUID.randomUUID().toString();
        var request = new ProjectController.CreateProject("无文件夹对话", null, "main", "SCRATCH", null, bootstrap);
        var first = projects.create(request);
        var repeated = projects.create(request);
        assertEquals(201, first.getStatusCode().value());
        assertEquals(200, repeated.getStatusCode().value());
        assertEquals(first.getBody().getId(), repeated.getBody().getId());
        assertNull(first.getBody().getRepositoryUrl());
        var scope = scopes.resolve(first.getBody().getId(), null, null);
        assertEquals(scope, scopes.resolve(first.getBody().getId(), null, null));
        assertEquals(WorkspaceEntity.Kind.SCRATCH,
            scopes.requireWorkspace(scope.projectId(), scope.workspaceId()).getKind());

        long dispatched = outbox.count();
        var created = tasks.createWithOutcome(scope.projectId(), "你好，帮我规划工程", "fixture", bootstrap,
            scope.workspaceId(), scope.conversationId(), null, null, null, null, "CHAT", null);
        var retried = tasks.createWithOutcome(scope.projectId(), "你好，帮我规划工程", "fixture", bootstrap,
            scope.workspaceId(), scope.conversationId(), null, null, null, null, "CHAT", null);
        assertTrue(created.created());
        assertFalse(retried.created());
        assertEquals(created.task().getId(), retried.task().getId());
        assertEquals(dispatched + 1, outbox.count());
        assertEquals("CHAT", taskRepository.findById(created.task().getId()).orElseThrow().getExecutionMode());
        var turn = messages.list(scope.projectId(), scope.workspaceId(), scope.conversationId(), 0);
        assertEquals(2, turn.size());
        assertEquals("你好，帮我规划工程", turn.get(0).getContent());
        assertEquals("PENDING", turn.get(1).getStatus());
        assertEquals(List.of(), messages.history(created.task()));
        var serialized = json.valueToTree(first.getBody());
        assertFalse(serialized.has("path"));
        assertFalse(serialized.has("absolutePath"));

        assertThrows(ResponseStatusException.class, () -> projects.create(
            new ProjectController.CreateProject("另一个名字", null, "main", "SCRATCH", null, bootstrap)));
        assertThrows(IllegalArgumentException.class, () -> tasks.createWithOutcome(scope.projectId(), "执行", "fixture", null,
            scope.workspaceId(), scope.conversationId(), null, null, "go test ./...", null, "CHAT", null));
    }

    @Test
    void sourceUploadsAndTaskBindingsRejectAnotherWorkspaceAndTampering() throws Exception {
        UUID project = localProject();
        var scope = scopes.resolve(project, null, null);
        var archive = archive("src/订单.go", "package current // dirty, uncommitted source\n");
        var uploaded = sources.create(project, scope.workspaceId(), archive);
        assertEquals(uploaded.getId(), sources.create(project, scope.workspaceId(), archive).getId());
        assertEquals(1, uploaded.getFileCount());
        assertFalse(json.valueToTree(uploaded).has("content"));
        assertFalse(json.valueToTree(uploaded).has("files"));
        assertEquals(archive.manifestHash(), json.readTree(uploaded.getContent()).path("manifestHash").asText());

        assertThrows(IllegalArgumentException.class, () -> tasks.createWithOutcome(project, "改代码", "fixture", null,
            scope.workspaceId(), scope.conversationId(), null, null, null, null, "CODE", null));
        var created = tasks.createWithOutcome(project, "改代码", "fixture", UUID.randomUUID().toString(),
            scope.workspaceId(), scope.conversationId(), null, null, null, null, "CODE", uploaded.getId());
        assertEquals(uploaded.getId(), created.task().getSourceSnapshotId());
        var queued = outbox.findAll().stream().filter(value -> value.getAggregateId().equals(created.task().getId())).findFirst().orElseThrow();
        var payload = json.readTree(queued.getPayload());
        assertEquals("LOCAL_FOLDER", payload.path("sourceKind").asText());
        assertEquals(archive.manifestHash(), payload.path("sourceManifestHash").asText());
        assertFalse(payload.has("path"));

        var otherWorkspace = scopes.createWorkspace(project, "独立工作树", WorkspaceEntity.Kind.LOCAL_FOLDER);
        var otherConversation = scopes.createConversation(project, otherWorkspace.getId(), "独立对话");
        ResponseStatusException mismatch = assertThrows(ResponseStatusException.class, () -> tasks.createWithOutcome(
            project, "越界", "fixture", null, otherWorkspace.getId(), otherConversation.getId(),
            null, null, null, null, "CODE", uploaded.getId()));
        assertEquals(HttpStatus.NOT_FOUND, mismatch.getStatusCode());
        UUID otherProject = localProject();
        assertThrows(ResponseStatusException.class, () -> sources.require(otherProject, null, uploaded.getId()));
        assertThrows(ResponseStatusException.class, () -> messages.list(otherProject, scope.workspaceId(), scope.conversationId(), 0));
        var changed = archive.files().get(0);
        assertThrows(IllegalArgumentException.class, () -> SourceSnapshotService.validate(new SourceSnapshotService.Archive(
            archive.manifestHash(), List.of(new SourceSnapshotService.FileEntry(changed.path(), changed.sha256(),
                Base64.getEncoder().encodeToString("tampered".getBytes(StandardCharsets.UTF_8)), false)))));
        for (String path : List.of("../outside", ".git/config", ".env", "src/CON", "C:/absolute", "auth.json", "src/secret.key")) {
            assertThrows(IllegalArgumentException.class, () -> SourceSnapshotService.validate(archive(path, "private")), path);
        }
    }

    @Test
    void sourceResultsRequireCurrentRunnerAndRetryClearsStaleOutput() throws Exception {
        UUID project=localProject();
        var scope=scopes.resolve(project,null,null);
        var input=archive("app.py","print('input')\n");
        var uploaded=sources.create(project,scope.workspaceId(),input);
        var task=tasks.createWithOutcome(project,"modify","fixture",null,scope.workspaceId(),scope.conversationId(),null,null,null,null,"CODE",uploaded.getId()).task();
        UUID owner=UUID.randomUUID();
        assertTrue(task.claim(owner,java.time.Instant.now()));
        taskRepository.saveAndFlush(task);
        var output=archive("app.py","print('output')\n");
        assertEquals(HttpStatus.CONFLICT,assertThrows(ResponseStatusException.class,
            ()->sourceController.output(task.getId(),1,UUID.randomUUID(),output)).getStatusCode());
        assertNull(taskRepository.findById(task.getId()).orElseThrow().getResultSourceSnapshotId());
        var saved=sourceController.output(task.getId(),1,owner,output);
        assertEquals(saved.getId(),taskRepository.findById(task.getId()).orElseThrow().getResultSourceSnapshotId());
        tasks.cancel(task.getId());
        var retried=tasks.retry(task.getId());
        assertNull(retried.getResultSourceSnapshotId());
        assertEquals(2,retried.getAttempt());
        assertEquals(HttpStatus.CONFLICT,assertThrows(ResponseStatusException.class,
            ()->sourceController.output(task.getId(),1,owner,output)).getStatusCode());

        var chat=tasks.createWithOutcome(project,"question","fixture",null,scope.workspaceId(),scope.conversationId(),null,null,null,null,"CHAT",null).task();
        assertTrue(chat.claim(owner,java.time.Instant.now()));taskRepository.saveAndFlush(chat);
        assertEquals(HttpStatus.CONFLICT,assertThrows(ResponseStatusException.class,
            ()->sourceController.output(chat.getId(),1,owner,output)).getStatusCode());
    }

    @Test
    void conversationHistoryRestoresCompletedTurnsAndExcludesPendingReplies() {
        UUID project = localProject();
        var scope = scopes.resolve(project, null, null);
        var first = tasks.createWithOutcome(project, "请保留 Java 17", "fixture", null,
            scope.workspaceId(), scope.conversationId(), null, null, null, null, "CHAT", null).task();
        messages.finish(first, "好的，使用 Java 17", "SUCCEEDED");
        var second = tasks.createWithOutcome(project, "下一步", "fixture", null,
            scope.workspaceId(), scope.conversationId(), null, null, null, null, "CHAT", null).task();
        var history = messages.history(second);
        assertEquals(2, history.size());
        assertEquals("请保留 Java 17", history.get(0).getContent());
        assertEquals("好的，使用 Java 17", history.get(1).getContent());
        assertTrue(history.get(0).getSequence() < history.get(1).getSequence());
        tasks.cancel(second.getId());
        var restored = messages.list(project, scope.workspaceId(), scope.conversationId(), 2);
        assertEquals(2, restored.size());
        assertEquals("CANCELLED", restored.get(1).getStatus());
    }

    private UUID localProject() {
        String bootstrap = UUID.randomUUID().toString();
        return projects.create(new ProjectController.CreateProject("本机工程", null, "main", "LOCAL_FOLDER",
            "desktop:" + bootstrap, bootstrap)).getBody().getId();
    }

    @Test
    void scratchCodeTasksSerializeAcrossConversationsAndRetryButAllowChatAndOtherWorkspaces() {
        UUID project=projects.create(new ProjectController.CreateProject("Scratch",null,"main","SCRATCH",null,UUID.randomUUID().toString())).getBody().getId();
        var scope=scopes.resolve(project,null,null);
        var otherChat=scopes.createConversation(project,scope.workspaceId(),"Other chat");
        String key=UUID.randomUUID().toString();
        var first=tasks.createWithOutcome(project,"first","fixture",key,scope.workspaceId(),scope.conversationId(),null,null,null,null,"CODE",null);
        assertFalse(tasks.createWithOutcome(project,"first","fixture",key,scope.workspaceId(),scope.conversationId(),null,null,null,null,"CODE",null).created());
        assertEquals(HttpStatus.CONFLICT,assertThrows(ResponseStatusException.class,()->tasks.createWithOutcome(project,"second","fixture",null,scope.workspaceId(),otherChat.getId(),null,null,null,null,"CODE",null)).getStatusCode());
        tasks.createWithOutcome(project,"chat","fixture",null,scope.workspaceId(),otherChat.getId(),null,null,null,null,"CHAT",null);
        var otherWorkspace=scopes.createWorkspace(project,"Other workspace",WorkspaceEntity.Kind.SCRATCH);
        tasks.createWithOutcome(project,"independent","fixture",null,otherWorkspace.getId(),null,null,null,null,null,"CODE",null);
        tasks.cancel(first.task().getId());
        var second=tasks.createWithOutcome(project,"second","fixture",null,scope.workspaceId(),otherChat.getId(),null,null,null,null,"CODE",null).task();
        assertThrows(ResponseStatusException.class,()->tasks.retry(first.task().getId()));
        tasks.cancel(second.getId());
        assertEquals(2,tasks.retry(first.task().getId()).getAttempt());
    }

    @Test
    void concurrentScratchCodeCreationAcceptsOnlyOneTask() throws Exception {
        UUID project=projects.create(new ProjectController.CreateProject("Concurrent scratch",null,"main","SCRATCH",null,UUID.randomUUID().toString())).getBody().getId();
        var scope=scopes.resolve(project,null,null);
        var workers=java.util.concurrent.Executors.newFixedThreadPool(2);
        var start=new java.util.concurrent.CountDownLatch(1);
        java.util.concurrent.Callable<Boolean> create=()->{start.await();try{tasks.createWithOutcome(project,"concurrent","fixture",UUID.randomUUID().toString(),scope.workspaceId(),scope.conversationId(),null,null,null,null,"CODE",null);return true;}catch(ResponseStatusException error){assertEquals(HttpStatus.CONFLICT,error.getStatusCode());return false;}};
        try{var first=workers.submit(create);var second=workers.submit(create);start.countDown();assertNotEquals(first.get(15,java.util.concurrent.TimeUnit.SECONDS),second.get(15,java.util.concurrent.TimeUnit.SECONDS));}
        finally{start.countDown();workers.shutdownNow();}
    }

    private static SourceSnapshotService.Archive archive(String path, String content) {
        byte[] data = content.getBytes(StandardCharsets.UTF_8);
        String hash = digest(data);
        var file = new SourceSnapshotService.FileEntry(path, hash, Base64.getEncoder().encodeToString(data), false);
        return new SourceSnapshotService.Archive(digest((path + '\0' + hash + '\0' + "0\n").getBytes(StandardCharsets.UTF_8)), List.of(file));
    }

    private static String digest(byte[] data) {
        try { return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(data)); }
        catch (java.security.NoSuchAlgorithmException error) { throw new IllegalStateException(error); }
    }
}
