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
}
