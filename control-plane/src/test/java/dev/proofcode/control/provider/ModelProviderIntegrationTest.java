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
        var account=http.postForEntity(url("/api/auth/register"),new AccountController.Credentials("provider-"+UUID.randomUUID(),"password-long-enough"),AccountService.Login.class);
        accountToken=account.getBody().token();
        accountId=account.getBody().user().id();
    }
    @AfterEach void stopServer(){modelServer.stop(0);}
    private String url(String path){return "http://localhost:"+port+path;}
    private HttpEntity<String> body(String json){HttpHeaders h=new HttpHeaders();h.setBearerAuth(accountToken);h.setContentType(MediaType.APPLICATION_JSON);return new HttpEntity<>(json,h);}
    @Test void toolProbeChecksEachProtocolAndNeverLeaksProviderErrors() throws Exception {
        var mapper=com.fasterxml.jackson.databind.json.JsonMapper.builder().build();
        var current=new java.util.concurrent.atomic.AtomicReference<>("chat");
        var status=new java.util.concurrent.atomic.AtomicInteger(200);
        var malformed=new java.util.concurrent.atomic.AtomicBoolean();
        modelServer.removeContext("/v1/models");
        modelServer.createContext("/v1/models",exchange->{byte[] bytes="{\"data\":[{\"id\":\"model-small\"}]}".getBytes(StandardCharsets.UTF_8);exchange.sendResponseHeaders(200,bytes.length);exchange.getResponseBody().write(bytes);exchange.close();});
        com.sun.net.httpserver.HttpHandler handler=exchange->{
            String protocol=current.get();
            assertEquals("POST",exchange.getRequestMethod());
            if(protocol.equals("anthropic")){assertEquals("top-secret-key",exchange.getRequestHeaders().getFirst("x-api-key"));assertNull(exchange.getRequestHeaders().getFirst("Authorization"));}else assertEquals("Bearer top-secret-key",exchange.getRequestHeaders().getFirst("Authorization"));
            var payload=mapper.readTree(exchange.getRequestBody());assertEquals("model-small",payload.path("model").asText());assertFalse(payload.path("stream").asBoolean());assertEquals(1,payload.path("tools").size());
            if(protocol.equals("responses")){assertFalse(payload.path("store").asBoolean(true));assertEquals("function",payload.path("tool_choice").path("type").asText());}
            if(protocol.equals("anthropic"))assertEquals("proofcode_probe",payload.path("tool_choice").path("name").asText());
            String response=switch(protocol){
                case "anthropic"->"{\"type\":\"message\",\"stop_reason\":\"tool_use\",\"content\":[{\"type\":\"tool_use\",\"id\":\"fixture\",\"name\":\"proofcode_probe\",\"input\":{\"ok\":true}}]}";
                case "responses"->"{\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"call_id\":\"fixture\",\"name\":\"proofcode_probe\",\"arguments\":\"{\\\"ok\\\":true}\"}]}";
                default->"{\"choices\":[{\"finish_reason\":\"tool_calls\",\"message\":{\"tool_calls\":[{\"id\":\"fixture\",\"function\":{\"name\":\"proofcode_probe\",\"arguments\":\"{\\\"ok\\\":true}\"}}]}}]}";
            };
            if(malformed.get())response="{\"text\":\"top-secret-key\"}";
            if(status.get()!=200)response="{\"error\":\"top-secret-key\"}";
            byte[] bytes=response.getBytes(StandardCharsets.UTF_8);exchange.sendResponseHeaders(status.get(),bytes.length);exchange.getResponseBody().write(bytes);exchange.close();
        };
        for(String path:java.util.List.of("/v1/messages","/v1/responses","/v1/chat/completions"))modelServer.createContext(path,handler);
        for(String protocol:java.util.List.of("chat","responses","anthropic")){
            current.set(protocol);status.set(200);malformed.set(false);
            String input="{\"name\":\"Probe fixture\",\"baseUrl\":\"http://127.0.0.1:"+modelServer.getAddress().getPort()+"/v1\",\"apiKey\":\"top-secret-key\",\"wireApi\":\""+protocol+"\"}";
            var saved=http.exchange(url("/api/model-providers"),HttpMethod.POST,body(input),String.class);assertEquals(HttpStatus.CREATED,saved.getStatusCode());
            String id=mapper.readTree(saved.getBody()).path("id").asText();
            assertEquals(HttpStatus.OK,http.exchange(url("/api/model-providers/"+id+"/models/refresh"),HttpMethod.POST,body("{}"),String.class).getStatusCode());
            String endpoint=url("/api/model-providers/"+id+"/probe");
            var success=http.exchange(endpoint,HttpMethod.POST,body("{\"model\":\"model-small\"}"),String.class);
            assertEquals(HttpStatus.OK,success.getStatusCode());assertTrue(mapper.readTree(success.getBody()).path("success").asBoolean());assertFalse(success.getBody().contains("top-secret-key"));
            status.set(403);
            var denied=http.exchange(endpoint,HttpMethod.POST,body("{\"model\":\"model-small\"}"),String.class);
            assertEquals("permission_or_policy",mapper.readTree(denied.getBody()).path("outcome").asText());assertFalse(denied.getBody().contains("top-secret-key"));
            status.set(200);malformed.set(true);
            var invalid=http.exchange(endpoint,HttpMethod.POST,body("{\"model\":\"model-small\"}"),String.class);
            assertEquals("tool_protocol",mapper.readTree(invalid.getBody()).path("outcome").asText());assertFalse(invalid.getBody().contains("top-secret-key"));
        }
    }
    @Test void anthropicCatalogUsesNativeAuthenticationAndBoundedPagination() throws Exception {
        modelServer.removeContext("/v1/models");
        var calls=new java.util.concurrent.atomic.AtomicInteger();
        var cyclic=new java.util.concurrent.atomic.AtomicBoolean();
        modelServer.createContext("/v1/models",exchange->{
            calls.incrementAndGet();
            assertEquals("top-secret-key",exchange.getRequestHeaders().getFirst("x-api-key"));
            assertEquals("2023-06-01",exchange.getRequestHeaders().getFirst("anthropic-version"));
            assertNull(exchange.getRequestHeaders().getFirst("Authorization"));
            String query=exchange.getRequestURI().getRawQuery();
            String data=query==null||cyclic.get()?"{\"data\":[{\"id\":\"claude-first\"}],\"has_more\":true,\"last_id\":\"claude-first\"}":"{\"data\":[{\"id\":\"claude-second\"}],\"has_more\":false}";
            if(query!=null)assertEquals("after_id=claude-first",query);
            byte[] bytes=data.getBytes(StandardCharsets.UTF_8);exchange.getResponseHeaders().add("Content-Type","application/json");exchange.sendResponseHeaders(200,bytes.length);exchange.getResponseBody().write(bytes);exchange.close();
        });
        var mapper=com.fasterxml.jackson.databind.json.JsonMapper.builder().findAndAddModules().build();
        String input="{\"name\":\"Messages fixture\",\"baseUrl\":\"http://127.0.0.1:"+modelServer.getAddress().getPort()+"/v1\",\"apiKey\":\"top-secret-key\",\"wireApi\":\"anthropic\"}";
        var saved=http.exchange(url("/api/model-providers"),HttpMethod.POST,body(input),String.class);
        assertEquals(HttpStatus.CREATED,saved.getStatusCode());assertFalse(saved.getBody().contains("top-secret-key"));
        String id=mapper.readTree(saved.getBody()).path("id").asText();
        var refreshed=http.exchange(url("/api/model-providers/"+id+"/models/refresh"),HttpMethod.POST,body("{}"),String.class);
        assertEquals(HttpStatus.OK,refreshed.getStatusCode());assertEquals(2,calls.get());
        assertEquals(2,mapper.readTree(refreshed.getBody()).path("models").size());
        SecurityContextHolder.getContext().setAuthentication(new UsernamePasswordAuthenticationToken(accountId,null,java.util.List.of()));
        try {
            var task=new TaskEntity(UUID.randomUUID(),UUID.randomUUID(),"fixture","claude-second",Instant.now());
            providers.bind(task,UUID.fromString(id));task.transition(TaskStatus.QUEUED);UUID runner=UUID.randomUUID();assertTrue(task.claim(runner,Instant.now()));
            assertEquals("anthropic",providers.resolve(task,runner,1).wireApi());
        }finally{SecurityContextHolder.clearContext();}
        cyclic.set(true);
        var failed=http.exchange(url("/api/model-providers/"+id+"/models/refresh"),HttpMethod.POST,body("{}"),String.class);
        assertEquals(HttpStatus.BAD_REQUEST,failed.getStatusCode());
        var listed=http.exchange(url("/api/model-providers"),HttpMethod.GET,body(""),String.class);
        assertTrue(listed.getBody().contains("claude-second"),"failed refresh must retain the previous catalog");
    }
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
