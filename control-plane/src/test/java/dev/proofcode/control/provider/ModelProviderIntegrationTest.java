package dev.proofcode.control.provider;

import com.sun.net.httpserver.HttpServer;
import dev.proofcode.control.account.*;
import dev.proofcode.control.websocket.TaskSocketHandler;
import dev.proofcode.control.task.TaskEntity;
import dev.proofcode.control.task.TaskStatus;
import java.time.Instant;
import java.util.UUID;
import org.springframework.security.authentication.UsernamePasswordAuthenticationToken;
import org.springframework.security.core.context.SecurityContextHolder;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import org.junit.jupiter.api.*;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.http.*;
import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest(webEnvironment=SpringBootTest.WebEnvironment.RANDOM_PORT,properties={"spring.task.scheduling.enabled=false","DATA_SNAPSHOT_KEY=MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=","proofcode.providers.allow-private-hosts=true"})
class ModelProviderIntegrationTest {
    @MockBean JmsTemplate jms; @MockBean StringRedisTemplate redis; @MockBean TaskSocketHandler sockets;
    @Autowired TestRestTemplate http; @LocalServerPort int port; HttpServer modelServer;
    @Autowired ProviderService providers;
    String accountToken;
    UUID accountId;
    @BeforeEach void startServer() throws Exception {
        modelServer=HttpServer.create(new InetSocketAddress("127.0.0.1",0),0);
        modelServer.createContext("/v1/models",exchange->{assertEquals("Bearer top-secret-key",exchange.getRequestHeaders().getFirst("Authorization"));byte[] body="{\"data\":[{\"id\":\"model-small\"},{\"id\":\"model-large\"}]}".getBytes(StandardCharsets.UTF_8);exchange.getResponseHeaders().add("Content-Type","application/json");exchange.sendResponseHeaders(200,body.length);exchange.getResponseBody().write(body);exchange.close();});
        modelServer.start();
        var account=http.postForEntity(url("/api/auth/register"),new AccountController.Credentials("provider-test-01","password-long-enough"),AccountService.Login.class);
        accountToken=account.getBody().token();
        accountId=account.getBody().user().id();
    }
    @AfterEach void stopServer(){modelServer.stop(0);}
    private String url(String path){return "http://localhost:"+port+path;}
    private HttpEntity<String> body(String json){HttpHeaders h=new HttpHeaders();h.setBearerAuth(accountToken);h.setContentType(MediaType.APPLICATION_JSON);return new HttpEntity<>(json,h);}
    @Test void modelCatalogIsFetchedAndSecretsNeverAppearInProviderDto() throws Exception {
        String input="{\"name\":\"Local OpenAI compatible\",\"baseUrl\":\"http://127.0.0.1:"+modelServer.getAddress().getPort()+"/v1\",\"apiKey\":\"top-secret-key\",\"wireApi\":\"chat\",\"aliases\":{\"model-small\":\"Small\"},\"modelSettings\":{\"model-small\":{\"contextTokens\":1000000,\"maxInputTokens\":900000,\"maxOutputTokens\":32000,\"compression\":\"aggressive\",\"currency\":\"USD\",\"prices\":[{\"upToInputTokens\":10000,\"input\":2,\"output\":8,\"cacheRead\":0.5},{\"upToInputTokens\":24000,\"input\":4,\"output\":12},{\"upToInputTokens\":0,\"input\":6,\"output\":16}]}}}";
        var saved=http.exchange(url("/api/model-providers"),HttpMethod.POST,body(input),String.class);
        assertEquals(HttpStatus.CREATED,saved.getStatusCode());assertFalse(saved.getBody().contains("top-secret-key"));assertTrue(saved.getBody().contains("\"hasKey\":true"));
        var created=com.fasterxml.jackson.databind.json.JsonMapper.builder().build().readTree(saved.getBody()).path("id").asText();
        var refreshed=http.exchange(url("/api/model-providers/"+created+"/models/refresh"),HttpMethod.POST,body("{}"),String.class);
        assertEquals(HttpStatus.OK,refreshed.getStatusCode());assertFalse(refreshed.getBody().contains("top-secret-key"));assertTrue(refreshed.getBody().contains("model-small"));assertTrue(refreshed.getBody().contains("\"name\":\"Small\""));
        var mapper=com.fasterxml.jackson.databind.json.JsonMapper.builder().findAndAddModules().build();
        assertEquals(1000000,mapper.readTree(refreshed.getBody()).path("modelSettings").path("model-small").path("contextTokens").asInt());
        SecurityContextHolder.getContext().setAuthentication(new UsernamePasswordAuthenticationToken(accountId,null,java.util.List.of()));
        try {
            var task=new TaskEntity(UUID.randomUUID(),UUID.randomUUID(),"fixture","model-small",Instant.now());
            providers.bind(task,UUID.fromString(created));
            task.transition(TaskStatus.QUEUED);var runner=UUID.randomUUID();assertTrue(task.claim(runner,Instant.now()));
            assertEquals(1000000,providers.resolve(task,runner,1).settings().contextTokens());
            var update=mapper.readTree(input);((com.fasterxml.jackson.databind.node.ObjectNode)update.path("modelSettings").path("model-small")).put("contextTokens",1100000);
            var patched=http.exchange(url("/api/model-providers/"+created),HttpMethod.PATCH,body(update.toString()),String.class);
            assertEquals(HttpStatus.OK,patched.getStatusCode());
            assertEquals(1000000,providers.resolve(task,runner,1).settings().contextTokens(),"running task retains its original configuration");
            assertFalse(mapper.writeValueAsString(task).contains("top-secret-key"));
        } finally {SecurityContextHolder.clearContext();}
    }
}
