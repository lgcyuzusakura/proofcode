package dev.proofcode.control.agentplatform;

import com.fasterxml.jackson.databind.*;
import com.sun.net.httpserver.*;
import dev.proofcode.control.account.*;
import dev.proofcode.control.project.*;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.*;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.*;
import org.junit.jupiter.api.condition.EnabledIfEnvironmentVariable;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.http.*;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.security.authentication.UsernamePasswordAuthenticationToken;
import org.springframework.security.core.context.SecurityContextHolder;
import org.springframework.web.server.ResponseStatusException;
import static org.junit.jupiter.api.Assertions.*;

@EnabledIfEnvironmentVariable(named="PROOFCODE_MCP_INSPECT_TEST_BIN",matches=".+")
@SpringBootTest(webEnvironment=SpringBootTest.WebEnvironment.RANDOM_PORT,properties={"spring.task.scheduling.enabled=false","DATA_SNAPSHOT_KEY=MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=","proofcode.mcp.allow-private-hosts=true","proofcode.mcp.inspect-bin=${PROOFCODE_MCP_INSPECT_TEST_BIN}"})
class McpOAuthIntegrationTest {
 @MockBean JmsTemplate jms; @MockBean StringRedisTemplate redis; @MockBean TaskSocketHandler sockets;
 @Autowired McpServerService servers; @Autowired McpOAuthService oauth; @Autowired McpOAuthGrantRepository grants; @Autowired AgentResourceRepository resources;
 @Autowired AccountService accounts; @Autowired ProjectRepository projects; @Autowired ObjectMapper json; @Autowired TestRestTemplate http;
 HttpServer fixture;ExecutorService executor;UUID owner,project;String token;AtomicInteger exchanges=new AtomicInteger(),refreshes=new AtomicInteger(),invalidRequests=new AtomicInteger();
 Map<String,String> challenges=new ConcurrentHashMap<>();
 @BeforeEach void setup()throws Exception{
  var user=accounts.register("oauth-"+UUID.randomUUID(),"fixture-password");owner=user.user().id();token=user.token();login(owner);project=UUID.randomUUID();var p=new ProjectEntity(project,"OAuth fixture","https://example.com/repo.git","main",Instant.now());p.setOwnerId(owner);projects.save(p);
  fixture=HttpServer.create(new InetSocketAddress("127.0.0.1",0),0);executor=Executors.newCachedThreadPool();fixture.setExecutor(executor);fixture.createContext("/",this::handle);fixture.start();
 }
 @AfterEach void cleanup(){fixture.stop(0);executor.shutdownNow();SecurityContextHolder.clearContext();}
 void login(UUID id){SecurityContextHolder.getContext().setAuthentication(new UsernamePasswordAuthenticationToken(id,null,List.of()));}
 String url(String path){return "http://127.0.0.1:"+fixture.getAddress().getPort()+path;}
 static Map<String,String> form(String text){Map<String,String> result=new HashMap<>();for(String item:text.split("&")){var pair=item.split("=",2);result.put(URLDecoder.decode(pair[0],StandardCharsets.UTF_8),pair.length==1?"":URLDecoder.decode(pair[1],StandardCharsets.UTF_8));}return result;}
 void handle(HttpExchange request)throws java.io.IOException{
  try{String path=request.getRequestURI().getPath();Object result;
   if(path.equals("/mcp")){request.getResponseHeaders().add("WWW-Authenticate","Bearer resource_metadata=\""+url("/.well-known/oauth-protected-resource")+"\"");request.sendResponseHeaders(401,-1);return;}
   if(path.equals("/.well-known/oauth-protected-resource"))result=Map.of("resource",url("/mcp"),"authorization_servers",List.of(url("")));
   else if(path.equals("/.well-known/oauth-authorization-server"))result=Map.of("issuer",url(""),"authorization_endpoint",url("/authorize"),"token_endpoint",url("/token"),"code_challenge_methods_supported",List.of("S256"));
   else if(path.equals("/token")){
    var fields=form(new String(request.getRequestBody().readAllBytes(),StandardCharsets.UTF_8));
    if(!url("/mcp").equals(fields.get("resource"))||!"fixture-client".equals(fields.get("client_id"))||!"Basic Zml4dHVyZS1jbGllbnQ6Zml4dHVyZS1jbGllbnQtc2VjcmV0".equals(request.getRequestHeaders().getFirst("Authorization"))){invalidRequests.incrementAndGet();request.sendResponseHeaders(400,-1);return;}
    if("authorization_code".equals(fields.get("grant_type"))){String challenge=challenges.remove(fields.get("code"));String verifier=fields.get("code_verifier");String actual;try{actual=Base64.getUrlEncoder().withoutPadding().encodeToString(MessageDigest.getInstance("SHA-256").digest(verifier.getBytes(StandardCharsets.UTF_8)));}catch(Exception e){throw new java.io.IOException(e);}if(challenge==null||!challenge.equals(actual)){invalidRequests.incrementAndGet();request.sendResponseHeaders(400,-1);return;}exchanges.incrementAndGet();result=Map.of("access_token","fixture-access-one","refresh_token","fixture-refresh-one","token_type","Bearer","expires_in",3600);}
    else {if(!"fixture-refresh-one".equals(fields.get("refresh_token"))&&!"fixture-refresh-two".equals(fields.get("refresh_token"))){invalidRequests.incrementAndGet();request.sendResponseHeaders(400,-1);return;}refreshes.incrementAndGet();result=Map.of("access_token","fixture-access-two","refresh_token","fixture-refresh-two","token_type","Bearer","expires_in",3600);}
   }else{request.sendResponseHeaders(404,-1);return;}
   byte[] bytes=json.writeValueAsBytes(result);request.getResponseHeaders().add("Content-Type","application/json");request.sendResponseHeaders(200,bytes.length);request.getResponseBody().write(bytes);
  }finally{request.close();}
 }
 McpServerService.View configured(){var server=servers.save(project,null,new McpServerService.Input("docs"+UUID.randomUUID().toString().substring(0,8),url("/mcp"),"",false,true,0));var status=oauth.save(project,server.id(),server.version(),new McpOAuthService.Input("fixture-client","fixture-client-secret","docs.read offline_access",false));return servers.list(project).stream().filter(s->s.id().equals(server.id())&&s.version()==status.serverVersion()).findFirst().orElseThrow();}
 record Pending(String state,String code){}
 Pending pending(McpServerService.View server){var begun=oauth.begin(project,server.id(),server.version());var query=form(URI.create(begun.authorizationUrl()).getRawQuery());assertEquals("S256",query.get("code_challenge_method"));assertEquals(url("/mcp"),query.get("resource"));String code=UUID.randomUUID().toString();challenges.put(code,query.get("code_challenge"));return new Pending(query.get("state"),code);}
 ResponseEntity<String> callback(Pending value){return http.getForEntity("/api/mcp/oauth/callback?state="+value.state()+"&code="+value.code(),String.class);}
 McpOAuthGrant grant(McpServerService.View server){return grants.findFirstByServerIdAndServerVersionOrderByCreatedAtDesc(server.id(),server.version()).orElseThrow();}

 @Test void authorizationUsesActualHelperAndEncryptsTokensAndPkce(){var server=configured();var begun=pending(server);var saved=grant(server);assertNotEquals(begun.state(),saved.stateHash);assertFalse(saved.encryptedConfiguration.contains("fixture-client-secret"));assertFalse(saved.encryptedConfiguration.contains(begun.state()));assertEquals(HttpStatus.OK,callback(begun).getStatusCode());var authorized=grant(server);assertEquals("AUTHORIZED",authorized.status);assertNull(authorized.stateHash);assertFalse(authorized.encryptedTokens.contains("fixture-access"));assertFalse(authorized.encryptedTokens.contains("fixture-refresh"));var view=oauth.status(project,server.id(),server.version());assertEquals("AUTHORIZED",view.state());assertFalse(view.toString().contains("fixture-client-secret"));assertEquals(1,exchanges.get());assertEquals(0,invalidRequests.get());}
 @Test void stateMismatchAndCallbackReplayCannotExchangeTokens(){var server=configured();var begun=pending(server);assertEquals(HttpStatus.BAD_REQUEST,callback(new Pending("wrong-state-"+UUID.randomUUID(),begun.code())).getStatusCode());assertEquals(0,exchanges.get());assertEquals(HttpStatus.OK,callback(begun).getStatusCode());assertEquals(HttpStatus.BAD_REQUEST,callback(begun).getStatusCode());assertEquals(1,exchanges.get());}
 @Test void staleConfigurationCancelAndExpiryRejectCallbacks(){var server=configured();var begun=pending(server);servers.save(project,server.id(),new McpServerService.Input(server.name(),server.url(),"",false,false,server.version()));assertEquals(HttpStatus.BAD_REQUEST,callback(begun).getStatusCode());assertEquals(0,exchanges.get());var next=configured();var cancelled=pending(next);assertEquals("CANCELLED",oauth.cancel(project,next.id(),next.version()).state());assertEquals(HttpStatus.BAD_REQUEST,callback(cancelled).getStatusCode());var expired=pending(next);var record=grant(next);record.pendingUntil=Instant.now().minusSeconds(1);grants.saveAndFlush(record);oauth.expirePending();assertEquals(HttpStatus.BAD_REQUEST,callback(expired).getStatusCode());assertEquals("",grant(next).encryptedConfiguration);}
 @Test void refreshRotationPreservesPinnedGrantAcrossServiceEditsAndDeletion(){var server=configured();assertEquals(HttpStatus.OK,callback(pending(server)).getStatusCode());var pinned=servers.pin(project,List.of(server.id()));String snapshot;try{snapshot=json.writeValueAsString(pinned);}catch(Exception e){throw new RuntimeException(e);}assertFalse(snapshot.contains("fixture-access"));assertFalse(snapshot.contains("fixture-client-secret"));var record=grant(server);record.tokenExpiresAt=Instant.now().minusSeconds(1);grants.saveAndFlush(record);var first=servers.connections(project,pinned).get(0);assertEquals("fixture-access-two",first.bearer());assertEquals(1,refreshes.get());assertEquals(record.id.toString(),first.oauthGrantId());servers.delete(project,server.id(),server.version());assertEquals("fixture-access-two",servers.connections(project,pinned).get(0).bearer());assertEquals(1,refreshes.get());assertFalse(grants.findById(record.id).orElseThrow().encryptedTokens.contains("fixture-refresh-two"));}
 @Test void foreignProjectAndServerReferencesFailWithoutRefresh(){var server=configured();assertEquals(HttpStatus.OK,callback(pending(server)).getStatusCode());var reference=oauth.pin(server.id(),server.version());assertThrows(ResponseStatusException.class,()->oauth.resolve(reference,UUID.randomUUID(),server.id(),server.version(),server.url()));assertThrows(ResponseStatusException.class,()->oauth.resolve(reference,project,UUID.randomUUID(),server.version(),server.url()));assertThrows(ResponseStatusException.class,()->oauth.resolve(reference,project,server.id(),server.version()+1,server.url()));assertThrows(ResponseStatusException.class,()->oauth.resolve(reference,project,server.id(),server.version(),server.url()+"/other"));assertEquals(0,refreshes.get());}
 @Test void publicEndpointsRequireOwnershipAndRemoteFailuresDoNotReturnAccount403(){var server=configured();var headers=new HttpHeaders();headers.setBearerAuth(token);var other=accounts.register("other-"+UUID.randomUUID(),"fixture-password");var foreignHeaders=new HttpHeaders();foreignHeaders.setBearerAuth(other.token());var path="/api/projects/"+project+"/mcp-servers/"+server.id()+"/oauth?version="+server.version();assertEquals(HttpStatus.NOT_FOUND,http.exchange(path,HttpMethod.GET,new HttpEntity<>(null,foreignHeaders),String.class).getStatusCode());var state=http.exchange(path,HttpMethod.GET,new HttpEntity<>(null,headers),String.class);assertEquals(HttpStatus.OK,state.getStatusCode());assertFalse(state.getBody().contains("fixture-client-secret"));assertFalse(state.getBody().contains("encryptedSecret"));}
}
