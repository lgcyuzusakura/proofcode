package dev.proofcode.control.account;

import dev.proofcode.control.websocket.TaskSocketHandler;
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
class AccountAuthenticationIntegrationTest {
    @MockBean JmsTemplate jms; @MockBean StringRedisTemplate redis; @MockBean TaskSocketHandler sockets;
    @Autowired TestRestTemplate http; @LocalServerPort int port;
    private String url(String path){return "http://localhost:"+port+path;}
    @Test void registrationIssuesOpaqueSessionAndLogoutRevokesIt(){
        var created=http.postForEntity(url("/api/auth/register"),new AccountController.Credentials("test-user-22","password-long-enough"),AccountService.Login.class);
        assertEquals(HttpStatus.OK,created.getStatusCode());assertNotNull(created.getBody());String token=created.getBody().token();assertTrue(token.matches("[A-Za-z0-9_-]{43}"));
        HttpHeaders h=new HttpHeaders();h.setBearerAuth(token);
        assertEquals(HttpStatus.OK,http.exchange(url("/api/auth/me"),HttpMethod.GET,new HttpEntity<>(h),AccountService.User.class).getStatusCode());
        assertEquals(HttpStatus.NO_CONTENT,http.exchange(url("/api/auth/logout"),HttpMethod.POST,new HttpEntity<>(h),Void.class).getStatusCode());
        assertEquals(HttpStatus.FORBIDDEN,http.exchange(url("/api/auth/me"),HttpMethod.GET,new HttpEntity<>(h),String.class).getStatusCode());
    }
    @Test void registrationRejectsWeakPasswords(){
        var response=http.postForEntity(url("/api/auth/register"),new AccountController.Credentials("test-user-23","short"),String.class);
        assertEquals(HttpStatus.BAD_REQUEST,response.getStatusCode());
    }
    @Test void projectsAreScopedToTheLoggedInAccount(){
        var first=http.postForEntity(url("/api/auth/register"),new AccountController.Credentials("owner-test-31","password-long-enough"),AccountService.Login.class).getBody().token();
        HttpHeaders owner=new HttpHeaders();owner.setBearerAuth(first);owner.setContentType(MediaType.APPLICATION_JSON);
        var project=http.exchange(url("/api/projects"),HttpMethod.POST,new HttpEntity<>("{\"name\":\"Private\",\"repositoryUrl\":null,\"defaultBranch\":\"main\",\"sourceKind\":\"SCRATCH\"}",owner),String.class);
        assertEquals(HttpStatus.CREATED,project.getStatusCode());
        String id;
        try{id=com.fasterxml.jackson.databind.json.JsonMapper.builder().build().readTree(project.getBody()).path("id").asText();}catch(Exception e){throw new AssertionError(e);}
        var second=http.postForEntity(url("/api/auth/register"),new AccountController.Credentials("other-test-32","password-long-enough"),AccountService.Login.class).getBody().token();
        HttpHeaders other=new HttpHeaders();other.setBearerAuth(second);
        assertEquals("[]",http.exchange(url("/api/projects"),HttpMethod.GET,new HttpEntity<>(other),String.class).getBody());
        assertEquals(HttpStatus.NOT_FOUND,http.exchange(url("/api/projects/"+id+"/workspaces"),HttpMethod.GET,new HttpEntity<>(other),String.class).getStatusCode());
    }
}
