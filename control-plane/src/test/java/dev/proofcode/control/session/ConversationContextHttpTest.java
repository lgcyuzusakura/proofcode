package dev.proofcode.control.session;

import dev.proofcode.control.account.*;
import dev.proofcode.control.project.ProjectController;
import dev.proofcode.control.task.*;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.util.*;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.http.*;
import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest(webEnvironment=SpringBootTest.WebEnvironment.RANDOM_PORT,properties="spring.task.scheduling.enabled=false")
class ConversationContextHttpTest {
    @MockBean JmsTemplate jms; @MockBean StringRedisTemplate redis; @MockBean TaskSocketHandler sockets;
    @Autowired TestRestTemplate http; @LocalServerPort int port;
    @Autowired ProjectController projects; @Autowired WorkspaceService scopes; @Autowired TaskService tasks;
    @Autowired ConversationMessageService messages;
    private String url(String path){return "http://localhost:"+port+path;}
    private HttpEntity<Object> body(String token,Object body){var headers=new HttpHeaders();headers.setBearerAuth(token);headers.setContentType(MediaType.APPLICATION_JSON);return new HttpEntity<>(body,headers);}
    @Test void previewEditAndRunnerSnapshotUseTheSameProjectionAndOwnershipBoundary(){
        var project=projects.create(new ProjectController.CreateProject("HTTP context","https://example.com/context.git","main","REMOTE_REPOSITORY",null,UUID.randomUUID().toString())).getBody();
        var scope=scopes.resolve(project.getId(),null,null);
        var previous=tasks.createWithOutcome(project.getId(),"unrelated private branch","fixture",null,scope.workspaceId(),scope.conversationId(),null,null,null,null,"CHAT",null).task();
        messages.finish(previous,"private branch answer","SUCCEEDED");
        var transcript=messages.list(scope.projectId(),scope.workspaceId(),scope.conversationId(),0);
        String path="/api/projects/"+scope.projectId()+"/workspaces/"+scope.workspaceId()+"/conversations/"+scope.conversationId()+"/context";
        var preview=http.exchange(url(path),HttpMethod.GET,body("test-token",null),ConversationContextService.Preview.class);
        assertEquals(HttpStatus.OK,preview.getStatusCode());assertEquals(2,preview.getBody().messages().size());
        var request=new ConversationContextController.Update(preview.getBody().version(),new ConversationContextService.View("FOCUS",List.of(new ConversationContextService.Item(transcript.get(0).getId(),"EXCLUDE",null)),List.of()));
        var edited=http.exchange(url(path),HttpMethod.PATCH,body("test-token",request),ConversationContextService.Preview.class);
        assertEquals(HttpStatus.OK,edited.getStatusCode());assertTrue(edited.getBody().messages().isEmpty());
        var task=tasks.createWithOutcome(project.getId(),"current request","fixture",null,scope.workspaceId(),scope.conversationId(),null,null,null,null,"CHAT",null).task();
        var runner=http.exchange(url("/internal/tasks/"+task.getId()+"/conversation?attempt=1"),HttpMethod.GET,body("runner-token",null),String.class);
        assertEquals(HttpStatus.OK,runner.getStatusCode());assertEquals("[]",runner.getBody());assertEquals(task.getContextSelectionHash(),runner.getHeaders().getFirst("X-Context-Selection-Hash"));
        assertEquals(HttpStatus.CONFLICT,http.exchange(url("/internal/tasks/"+task.getId()+"/conversation?attempt=2"),HttpMethod.GET,body("runner-token",null),String.class).getStatusCode());
        var account=http.postForEntity(url("/api/auth/register"),new AccountController.Credentials("ctx-"+UUID.randomUUID().toString().substring(0,8),"password-long-enough"),AccountService.Login.class).getBody();
        // First-account migration may claim unowned projects; use a second account for isolation.
        account=http.postForEntity(url("/api/auth/register"),new AccountController.Credentials("ctx-"+UUID.randomUUID().toString().substring(0,8),"password-long-enough"),AccountService.Login.class).getBody();
        assertEquals(HttpStatus.NOT_FOUND,http.exchange(url(path),HttpMethod.GET,body(account.token(),null),String.class).getStatusCode());
        assertEquals(HttpStatus.NOT_FOUND,http.exchange(url(path),HttpMethod.PATCH,body(account.token(),request),String.class).getStatusCode());
    }

    @Test void conversationRenameArchiveRestoreAndArchivedGuardsWorkOverHttp(){
        var project=projects.create(new ProjectController.CreateProject("Archive HTTP","https://example.com/archive.git","main","REMOTE_REPOSITORY",null,UUID.randomUUID().toString())).getBody();
        var scope=scopes.resolve(project.getId(),null,null);
        String defaultPath="/api/projects/"+project.getId()+"/workspaces/"+scope.workspaceId()+"/conversations/"+scope.conversationId();
        assertEquals(HttpStatus.CONFLICT,http.exchange(url(defaultPath),HttpMethod.PATCH,body("test-token",Map.of("archived",true)),String.class).getStatusCode());
        var created=scopes.createConversation(project.getId(),scope.workspaceId(),"Old title");
        String conversationPath="/api/projects/"+project.getId()+"/workspaces/"+scope.workspaceId()+"/conversations/"+created.getId();
        var renamed=http.exchange(url(conversationPath),HttpMethod.PATCH,body("test-token",Map.of("title","Renamed title")),ConversationEntity.class);
        assertEquals(HttpStatus.OK,renamed.getStatusCode());assertEquals("Renamed title",renamed.getBody().getTitle());

        var active=tasks.createWithOutcome(project.getId(),"still running","fixture",null,scope.workspaceId(),created.getId(),null,null,null,null,"CHAT",null).task();
        var blocked=http.exchange(url(conversationPath),HttpMethod.PATCH,body("test-token",Map.of("archived",true)),String.class);
        assertEquals(HttpStatus.CONFLICT,blocked.getStatusCode());
        tasks.cancel(active.getId());

        var archived=http.exchange(url(conversationPath),HttpMethod.PATCH,body("test-token",Map.of("archived",true)),ConversationEntity.class);
        assertEquals(HttpStatus.OK,archived.getStatusCode());assertNotNull(archived.getBody().getArchivedAt());
        assertEquals(HttpStatus.OK,http.exchange(url(conversationPath),HttpMethod.GET,body("test-token",null),ConversationEntity.class).getStatusCode());
        assertEquals(HttpStatus.OK,http.exchange(url("/api/tasks?projectId="+project.getId()+"&conversationId="+created.getId()),HttpMethod.GET,body("test-token",null),String.class).getStatusCode());
        assertEquals(HttpStatus.OK,http.exchange(url("/api/projects/"+project.getId()+"/agent-platform/selection/"+created.getId()),HttpMethod.GET,body("test-token",null),String.class).getStatusCode());
        var contextPath=conversationPath+"/context";
        var contextEdit=new ConversationContextController.Update(0,new ConversationContextService.View("FOCUS",List.of(),List.of()));
        assertEquals(HttpStatus.CONFLICT,http.exchange(url(contextPath),HttpMethod.PATCH,body("test-token",contextEdit),String.class).getStatusCode());
        assertEquals(HttpStatus.OK,http.exchange(url(contextPath),HttpMethod.GET,body("test-token",null),String.class).getStatusCode());
        assertEquals(HttpStatus.OK,http.exchange(url(conversationPath+"/messages"),HttpMethod.GET,body("test-token",null),String.class).getStatusCode());
        assertEquals(HttpStatus.CONFLICT,http.exchange(url("/api/tasks/"+active.getId()+"/retry"),HttpMethod.POST,body("test-token",null),String.class).getStatusCode());
        assertEquals(HttpStatus.CONFLICT,http.exchange(url("/api/projects/"+project.getId()+"/agent-platform/selection/"+created.getId()),HttpMethod.PATCH,body("test-token",Collections.singletonMap("profileId",null)),String.class).getStatusCode());
        assertEquals(HttpStatus.BAD_REQUEST,http.exchange(url(conversationPath),HttpMethod.PATCH,body("test-token",Map.of("title"," ","archived",false)),String.class).getStatusCode());
        assertTrue(http.exchange(url(conversationPath),HttpMethod.GET,body("test-token",null),ConversationEntity.class).getBody().isArchived());
        var anotherWorkspace=scopes.createWorkspace(project.getId(),"Other scope",WorkspaceEntity.Kind.REMOTE_REPOSITORY);
        String wrongScope="/api/projects/"+project.getId()+"/workspaces/"+anotherWorkspace.getId()+"/conversations/"+created.getId();
        assertEquals(HttpStatus.NOT_FOUND,http.exchange(url(wrongScope),HttpMethod.PATCH,body("test-token",Map.of("archived",false)),String.class).getStatusCode());
        assertEquals(HttpStatus.CONFLICT,http.exchange(url("/api/tasks"),HttpMethod.POST,body("test-token",new TaskController.CreateTask(project.getId(),"must not start","fixture",scope.workspaceId(),created.getId(),null,null,null,null,"CHAT",null)),String.class).getStatusCode());
        var rejected=assertThrows(org.springframework.web.server.ResponseStatusException.class,()->tasks.createWithOutcome(project.getId(),"must not start","fixture",null,scope.workspaceId(),created.getId(),null,null,null,null,"CHAT",null));
        assertEquals(HttpStatus.CONFLICT,rejected.getStatusCode());
        var restored=http.exchange(url(conversationPath),HttpMethod.PATCH,body("test-token",Map.of("archived",false)),ConversationEntity.class);
        assertEquals(HttpStatus.OK,restored.getStatusCode());assertNull(restored.getBody().getArchivedAt());
        var createdAfterRestore=http.exchange(url("/api/tasks"),HttpMethod.POST,body("test-token",new TaskController.CreateTask(project.getId(),"can start again","fixture",scope.workspaceId(),created.getId(),null,null,null,null,"CHAT",null)),TaskEntity.class);
        assertTrue(createdAfterRestore.getStatusCode().is2xxSuccessful());
    }

    @Test void archivedConversationIsHiddenFromOtherAccounts(){
        var owner=http.postForEntity(url("/api/auth/register"),new AccountController.Credentials("archive-owner-"+UUID.randomUUID(),"password-long-enough"),AccountService.Login.class).getBody();
        var outsider=http.postForEntity(url("/api/auth/register"),new AccountController.Credentials("archive-other-"+UUID.randomUUID(),"password-long-enough"),AccountService.Login.class).getBody();
        var project=http.exchange(url("/api/projects"),HttpMethod.POST,body(owner.token(),new ProjectController.CreateProject("Owned archive","https://example.com/owned.git","main","REMOTE_REPOSITORY",null,UUID.randomUUID().toString())),dev.proofcode.control.project.ProjectEntity.class).getBody();
        String projectPath="/api/projects/"+project.getId();
        var scope=http.exchange(url(projectPath+"/default-scope"),HttpMethod.POST,body(owner.token(),Map.of()),WorkspaceService.Scope.class).getBody();
        var created=http.exchange(url(projectPath+"/workspaces/"+scope.workspaceId()+"/conversations"),HttpMethod.POST,body(owner.token(),Map.of("title","Private archived history")),ConversationEntity.class).getBody();
        String path=projectPath+"/workspaces/"+scope.workspaceId()+"/conversations/"+created.getId();
        assertEquals(HttpStatus.OK,http.exchange(url(path),HttpMethod.PATCH,body(owner.token(),Map.of("archived",true)),ConversationEntity.class).getStatusCode());
        for(String suffix:List.of("","/messages","/context"))assertEquals(HttpStatus.NOT_FOUND,http.exchange(url(path+suffix),HttpMethod.GET,body(outsider.token(),null),String.class).getStatusCode());
        assertEquals(HttpStatus.NOT_FOUND,http.exchange(url(path),HttpMethod.PATCH,body(outsider.token(),Map.of("title","Changed","archived",false)),String.class).getStatusCode());
        assertEquals(HttpStatus.NOT_FOUND,http.exchange(url(projectPath+"/agent-platform/selection/"+created.getId()),HttpMethod.GET,body(outsider.token(),null),String.class).getStatusCode());
        assertEquals(HttpStatus.NOT_FOUND,http.exchange(url("/api/tasks?projectId="+project.getId()+"&conversationId="+created.getId()),HttpMethod.GET,body(outsider.token(),null),String.class).getStatusCode());
        var unchanged=http.exchange(url(path),HttpMethod.GET,body(owner.token(),null),ConversationEntity.class).getBody();
        assertTrue(unchanged.isArchived());assertEquals("Private archived history",unchanged.getTitle());
    }
}
