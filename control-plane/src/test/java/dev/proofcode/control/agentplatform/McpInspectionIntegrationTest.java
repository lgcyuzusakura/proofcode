package dev.proofcode.control.agentplatform;

import com.fasterxml.jackson.databind.*;
import com.sun.net.httpserver.*;
import dev.proofcode.control.account.AccountService;
import dev.proofcode.control.project.*;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.time.*;
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
import static org.junit.jupiter.api.Assertions.*;

/** Opt-in because the actual, separately compiled Go binary must be present. */
@EnabledIfEnvironmentVariable(named="PROOFCODE_MCP_INSPECT_TEST_BIN",matches=".+")
@SpringBootTest(webEnvironment=SpringBootTest.WebEnvironment.RANDOM_PORT,properties={"spring.task.scheduling.enabled=false","DATA_SNAPSHOT_KEY=MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=","proofcode.mcp.allow-private-hosts=true","proofcode.mcp.inspect-bin=${PROOFCODE_MCP_INSPECT_TEST_BIN}"})
class McpInspectionIntegrationTest {
 @MockBean JmsTemplate jms;
 @MockBean StringRedisTemplate redis;
 @MockBean TaskSocketHandler sockets;
 @Autowired McpServerService services;
 @Autowired McpInspector inspector;
 @Autowired AccountService accounts;
 @Autowired ProjectRepository projects;
 @Autowired ObjectMapper json;
 @Autowired TestRestTemplate http;
 UUID project,owner;String token;HttpServer server;ExecutorService executor;
 AtomicInteger calls=new AtomicInteger(),initialized=new AtomicInteger(),closed=new AtomicInteger();
 volatile CountDownLatch entered,release;

 @BeforeEach void setup()throws Exception{
  var account=accounts.register("mcp-probe-"+UUID.randomUUID(),"fixture-password");owner=account.user().id();token=account.token();login(owner);
  project=project(owner);
  executor=Executors.newCachedThreadPool(r->{var thread=new Thread(r,"mcp-probe-fixture");thread.setDaemon(true);return thread;});
  server=HttpServer.create(new InetSocketAddress("127.0.0.1",0),0);server.setExecutor(executor);server.createContext("/",this::handle);server.start();
 }
 void login(UUID value){SecurityContextHolder.getContext().setAuthentication(new UsernamePasswordAuthenticationToken(value,null,List.of()));}
 UUID project(UUID user){UUID id=UUID.randomUUID();var p=new ProjectEntity(id,"MCP fixture","https://example.com/repo.git","main",Instant.now());p.setOwnerId(user);projects.save(p);return id;}
 @AfterEach void cleanup(){if(release!=null)release.countDown();server.stop(0);executor.shutdownNow();SecurityContextHolder.clearContext();}
 String endpoint(String path){return "http://127.0.0.1:"+server.getAddress().getPort()+path;}
 McpServerService.View saved(String path,boolean enabled){return services.save(project,null,new McpServerService.Input("docs",endpoint(path),"fixture-secret",false,enabled,0));}
 ResponseEntity<JsonNode> probe(UUID target,McpServerService.View value,long version,String auth){
  var headers=new HttpHeaders();headers.setBearerAuth(auth);
  return http.exchange("/api/projects/"+target+"/mcp-servers/"+value.id()+"/probe?version="+version,HttpMethod.POST,new HttpEntity<>(null,headers),JsonNode.class);
 }
 void handle(HttpExchange exchange)throws java.io.IOException{
  try{
   boolean keyed=exchange.getRequestURI().getPath().equals("/keyed");
   if(keyed?!"fixture-header-key".equals(exchange.getRequestHeaders().getFirst("X-Api-Key")):!"Bearer fixture-secret".equals(exchange.getRequestHeaders().getFirst("Authorization"))){exchange.sendResponseHeaders(401,-1);return;}
   if(exchange.getRequestMethod().equals("DELETE")){closed.incrementAndGet();exchange.sendResponseHeaders(204,-1);return;}
   String path=exchange.getRequestURI().getPath();
   if(path.equals("/401")||path.equals("/403")||path.equals("/302")){
    exchange.getResponseHeaders().add("Location",endpoint("/mcp"));exchange.sendResponseHeaders(Integer.parseInt(path.substring(1)),-1);return;
   }
   var request=json.readTree(exchange.getRequestBody());String method=request.path("method").asText();Object result;
   if(method.equals("initialize")){
    initialized.incrementAndGet();exchange.getResponseHeaders().add("Mcp-Session-Id","fixture-session");
    result=Map.of("protocolVersion","2025-03-26","capabilities",Map.of("tools",Map.of()));
   }else if(method.equals("notifications/initialized")){exchange.sendResponseHeaders(202,-1);return;}
   else if(method.equals("tools/list")){
    if(entered!=null){entered.countDown();try{release.await(5,TimeUnit.SECONDS);}catch(InterruptedException e){Thread.currentThread().interrupt();}}
    result=Map.of("tools",List.of(Map.of("name","search","description","Search documents; fixture-secret fixture-header-key", "inputSchema",Map.of("type","object","properties",Map.of("query",Map.of("type","string","description","fixture-header-key"))))));
   }else{calls.incrementAndGet();result=Map.of();}
   byte[] body=json.writeValueAsBytes(Map.of("jsonrpc","2.0","id",request.path("id").asLong(),"result",result));
   exchange.getResponseHeaders().add("Content-Type","application/json");exchange.sendResponseHeaders(200,body.length);exchange.getResponseBody().write(body);
  }finally{exchange.close();}
 }

 @Test void explicitSavedProbeUsesRealHelperWithoutCallingToolsOrReturningToken(){
  var saved=saved("/mcp",true);assertEquals(0,initialized.get());
  var response=probe(project,saved,saved.version(),token);
  assertEquals(HttpStatus.OK,response.getStatusCode());assertEquals("no-store",response.getHeaders().getCacheControl());
  var body=response.getBody();assertEquals(saved.version(),body.path("savedVersion").asLong());var result=body.path("result");
  assertTrue(result.path("ok").asBoolean(),body.toString());assertEquals("2025-03-26",result.path("protocol").asText());assertEquals("mcp__docs__search",result.path("tools").get(0).path("qualifiedName").asText());
  assertFalse(body.toString().contains("fixture-secret"));assertEquals(0,calls.get());assertEquals(1,initialized.get());assertEquals(1,closed.get());
 }
 @Test void remoteAuthAndRedirectFailuresRemainHttp200DiagnosticReports(){
  for(String code:List.of("401","403","302")){
   var saved=services.save(project,null,new McpServerService.Input("docs"+code,endpoint("/"+code),"fixture-secret",false,true,0));
   var response=probe(project,saved,saved.version(),token);assertEquals(HttpStatus.OK,response.getStatusCode());
   assertEquals(Integer.parseInt(code),response.getBody().path("result").path("httpStatus").asInt());assertFalse(response.getBody().path("result").path("ok").asBoolean());
  }
  assertEquals(0,initialized.get());assertEquals(0,calls.get());
 }
 @Test void customHeaderOnlyAuthenticationWorksThroughSavedHttpApiAndActualHelper(){
  var headers=new HttpHeaders();headers.setBearerAuth(token);
  var input=new McpServerService.Input("keyed",endpoint("/keyed"),"",false,"X-Api-Key","fixture-header-key",false,true,0);
  var savedResponse=http.exchange("/api/projects/"+project+"/mcp-servers",HttpMethod.POST,new HttpEntity<>(input,headers),McpServerService.View.class);
  assertEquals(HttpStatus.OK,savedResponse.getStatusCode());var saved=savedResponse.getBody();assertTrue(saved.hasHeader());assertFalse(saved.hasBearer());
  var response=probe(project,saved,saved.version(),token);assertTrue(response.getBody().path("result").path("ok").asBoolean(),response.getBody().toString());
  assertFalse(response.getBody().toString().contains("fixture-header-key"));assertEquals(1,initialized.get());assertEquals(1,closed.get());assertEquals(0,calls.get());
 }
 @Test void disabledServiceMayBeDiagnosedButStaleForeignAndDeletedEntriesNeverConnect(){
  var saved=saved("/mcp",false);assertTrue(probe(project,saved,saved.version(),token).getBody().path("result").path("ok").asBoolean());
  assertEquals(HttpStatus.CONFLICT,probe(project,saved,99,token).getStatusCode());
  assertEquals(HttpStatus.NOT_FOUND,probe(project(owner),saved,saved.version(),token).getStatusCode());
  var foreign=accounts.register("mcp-other-"+UUID.randomUUID(),"fixture-password");
  assertEquals(HttpStatus.NOT_FOUND,probe(project,saved,saved.version(),foreign.token()).getStatusCode());
  services.delete(project,saved.id(),saved.version());assertEquals(HttpStatus.NOT_FOUND,probe(project,saved,saved.version(),token).getStatusCode());assertEquals(1,initialized.get());
 }
 @Test void concurrentConfigurationChangeDiscardsOutdatedReport()throws Exception{
  var saved=saved("/mcp",true);entered=new CountDownLatch(1);release=new CountDownLatch(1);
  var pending=executor.submit(()->probe(project,saved,saved.version(),token));assertTrue(entered.await(4,TimeUnit.SECONDS));
  services.save(project,saved.id(),new McpServerService.Input(saved.name(),saved.url(),null,false,false,saved.version()));release.countDown();
  assertEquals(HttpStatus.CONFLICT,pending.get(5,TimeUnit.SECONDS).getStatusCode());assertEquals(0,calls.get());
 }
 @Test void boundedParallelismRejectsThirdProbeAndSlotsRecover()throws Exception{
  var saved=saved("/mcp",true);entered=new CountDownLatch(2);release=new CountDownLatch(1);
  var first=executor.submit(()->probe(project,saved,saved.version(),token));var second=executor.submit(()->probe(project,saved,saved.version(),token));assertTrue(entered.await(5,TimeUnit.SECONDS));
  var third=probe(project,saved,saved.version(),token);assertEquals("busy",third.getBody().path("result").path("code").asText());release.countDown();
  assertTrue(first.get(5,TimeUnit.SECONDS).getBody().path("result").path("ok").asBoolean());assertTrue(second.get(5,TimeUnit.SECONDS).getBody().path("result").path("ok").asBoolean());
  entered=null;assertTrue(probe(project,saved,saved.version(),token).getBody().path("result").path("ok").asBoolean());
 }
 @Test void parentDeadlineStopsAnActualHelper()throws Exception{
  var saved=saved("/mcp",true);entered=new CountDownLatch(1);release=new CountDownLatch(1);
  var config=services.inspectConfiguration(project,saved.id(),saved.version());
  try(var bounded=new McpInspector(json,System.getenv("PROOFCODE_MCP_INSPECT_TEST_BIN"),150)){
   assertTimeoutPreemptively(Duration.ofSeconds(3),()->assertEquals("timeout",bounded.inspect(config).code()));
  }
 }
}
