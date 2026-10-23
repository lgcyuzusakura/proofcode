package dev.proofcode.control.agentplatform;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.sun.net.httpserver.HttpServer;
import dev.proofcode.control.account.AccountService;
import dev.proofcode.control.project.*;
import dev.proofcode.control.provider.*;
import dev.proofcode.control.session.WorkspaceService;
import dev.proofcode.control.task.*;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.*;
import org.junit.jupiter.api.*;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.security.authentication.UsernamePasswordAuthenticationToken;
import org.springframework.security.core.context.SecurityContextHolder;
import org.springframework.web.server.ResponseStatusException;
import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest(properties={"spring.task.scheduling.enabled=false","DATA_SNAPSHOT_KEY=MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=","proofcode.providers.allow-private-hosts=true","proofcode.mcp.allow-private-hosts=true"})
class AgentModelIntegrationTest {
 @MockBean JmsTemplate jms;
 @MockBean StringRedisTemplate redis;
 @MockBean TaskSocketHandler sockets;
 @Autowired AgentPlatformService agents;
 @Autowired ProviderService providers;
 @Autowired AccountService accounts;
 @Autowired ProjectRepository projects;
 @Autowired WorkspaceService scopes;
 @Autowired TaskService tasks;
 @Autowired TaskRepository taskRepository;
 @Autowired AgentResourceRepository agentResources;
 @Autowired McpServerService mcpServers;
 @Autowired McpServerController mcpApi;
 @Autowired ObjectMapper json;
 @Autowired PersonalizationService preferences;
 @Autowired SkillService skills;
 HttpServer server; UUID project,owner; WorkspaceService.Scope scope;
 @BeforeEach void setup() throws Exception {
  server=HttpServer.create(new InetSocketAddress("127.0.0.1",0),0);
  server.createContext("/v1/models",exchange->{byte[] body="{\"data\":[{\"id\":\"model-small\"},{\"id\":\"model-large\"}]}".getBytes(StandardCharsets.UTF_8);exchange.sendResponseHeaders(200,body.length);exchange.getResponseBody().write(body);exchange.close();});server.start();
  owner=accounts.register("agent-model-"+UUID.randomUUID(),"fixture-password").user().id();login(owner);
  project=UUID.randomUUID();var p=new ProjectEntity(project,"model-test","https://example.com/repo.git","main",Instant.now());p.setOwnerId(owner);projects.save(p);scope=scopes.resolve(project,null,null);
 }
 @AfterEach void cleanup(){server.stop(0);SecurityContextHolder.clearContext();}
 void login(UUID id){SecurityContextHolder.getContext().setAuthentication(new UsernamePasswordAuthenticationToken(id,null,List.of()));}
 ProviderService.View provider(){var p=providers.save(null,new ProviderService.Input("Fixture","http://127.0.0.1:"+server.getAddress().getPort()+"/v1","fixture-key","chat",null,null));return providers.refresh(p.id());}
 AgentPlatformService.Input input(long version,AgentPlatformService.ModelChoice choice){return new AgentPlatformService.Input("代码专家",version,new AgentPlatformService.Profile("检查测试",List.of(),List.of(),false,choice),null);}
 TaskEntity create(String model,UUID provider,String key){return tasks.createWithProvider(project,"fix tests",model,key,scope.workspaceId(),scope.conversationId(),null,null,null,null,"CODE",null,provider).task();}

 @Test void importsRealMarkdownAndPinsDefaultAndExplicitSkillVersionsAcrossRetry() throws Exception {
  var p=provider();String markdown="---\nname: api-review\ndescription: |\n  Review APIs and tests.\nlicense: MIT\n---\n\n# Workflow\nInspect tests before changes.\n";
  var saved=skills.importMarkdown(project,new SkillService.ImportInput(markdown,null,0,true));
  assertEquals("api-review",saved.name());assertEquals("SKILL.md",saved.source());assertEquals("Review APIs and tests.",saved.description());assertEquals(64,saved.hash().length());
  var task=create("model-small",p.id(),"skill-pinned");String frozen=task.getAgentSnapshot();
  var snapshot=json.readValue(frozen,AgentPlatformService.Snapshot.class);assertEquals(1,snapshot.skills().size());assertEquals(saved.hash(),snapshot.skills().get(0).hash());
  var changed=skills.save(project,saved.id(),new SkillService.Input(saved.name(),saved.description(),"New workflow",true,saved.version()));assertNotEquals(saved.hash(),changed.hash());
  assertEquals(frozen,create("model-small",p.id(),"skill-pinned").getAgentSnapshot());
  var custom=agents.save(project,"AGENT",null,new AgentPlatformService.Input("Reviewer",0,new AgentPlatformService.Profile("",List.of(),List.of(),true,null,List.of(),List.of(saved.id())),null));agents.select(project,scope.conversationId(),new AgentPlatformService.Selection(custom.id()));
  assertEquals("New workflow",json.readValue(create("model-small",p.id(),null).getAgentSnapshot(),AgentPlatformService.Snapshot.class).skills().get(0).content());
  var disabled=skills.save(project,changed.id(),new SkillService.Input(changed.name(),changed.description(),changed.content(),false,changed.version()));
  assertThrows(ResponseStatusException.class,()->create("model-small",p.id(),null));
  skills.delete(project,disabled.id(),disabled.version());assertThrows(ResponseStatusException.class,()->create("model-small",p.id(),null));
  var failed=taskRepository.findById(task.getId()).orElseThrow();failed.claim(UUID.randomUUID(),Instant.now());failed.fail("fixture");taskRepository.saveAndFlush(failed);
  assertEquals(frozen,tasks.retry(task.getId()).getAgentSnapshot());
  agents.select(project,scope.conversationId(),new AgentPlatformService.Selection(null));assertTrue(json.readValue(create("model-small",p.id(),null).getAgentSnapshot(),AgentPlatformService.Snapshot.class).skills().isEmpty());
 }

 @Test void skillsRejectStaleDuplicateForeignUnsafeAndOversizedImports(){
  var saved=skills.save(project,null,new SkillService.Input("review","Review APIs","Workflow",true,0));
  assertThrows(ResponseStatusException.class,()->skills.save(project,null,new SkillService.Input("review","Duplicate","Workflow",true,0)));
  assertThrows(ResponseStatusException.class,()->skills.save(project,saved.id(),new SkillService.Input("review","Stale","Workflow",true,99)));
  assertThrows(ResponseStatusException.class,()->skills.delete(project,saved.id(),99));
  assertThrows(IllegalArgumentException.class,()->skills.pin(project,List.of(saved.id(),saved.id())));
  assertThrows(IllegalArgumentException.class,()->skills.pin(project,Arrays.asList((UUID)null)));
  assertThrows(IllegalArgumentException.class,()->skills.save(project,null,new SkillService.Input("../unsafe","x","y",true,0)));
  assertThrows(IllegalArgumentException.class,()->skills.save(project,null,new SkillService.Input("big","x","y".repeat(24001),true,0)));
  assertThrows(IllegalArgumentException.class,()->skills.save(project,null,new SkillService.Input("long-line","x","中".repeat(6000),true,0)));
  for(String markdown:List.of("no header","---\nname: x\n---\nbody","---\nname: one\nname: two\ndescription: x\n---\nbody","---\nname: x\ndescription: !!java.net.URL [https://example.com]\n---\nbody"))assertThrows(IllegalArgumentException.class,()->skills.importMarkdown(project,new SkillService.ImportInput(markdown,null,0,true)));
  UUID other=UUID.randomUUID();var project2=new ProjectEntity(other,"other","https://example.com/other.git","main",Instant.now());project2.setOwnerId(owner);projects.save(project2);
  assertTrue(skills.list(other).isEmpty());assertThrows(ResponseStatusException.class,()->skills.pin(other,List.of(saved.id())));
  UUID foreign=accounts.register("skill-other-"+UUID.randomUUID(),"fixture-password").user().id();login(foreign);
  assertThrows(ResponseStatusException.class,()->skills.list(project));assertThrows(ResponseStatusException.class,()->skills.pin(project,List.of(saved.id())));
 }

 @Test void skillCatalogBudgetsAreExplicitAndUnboundAgentsDoNotInheritProjectSkills() throws Exception {
  var p=provider();for(int i=0;i<4;i++)skills.save(project,null,new SkillService.Input("skill-"+i,"Workflow",("x".repeat(11999)+"\n").repeat(2),true,0));
  assertThrows(ResponseStatusException.class,()->create("model-small",p.id(),null));
  var custom=agents.save(project,"AGENT",null,new AgentPlatformService.Input("No skills",0,new AgentPlatformService.Profile("",List.of(),List.of(),false),null));agents.select(project,scope.conversationId(),new AgentPlatformService.Selection(custom.id()));
  assertTrue(json.readValue(create("model-small",p.id(),null).getAgentSnapshot(),AgentPlatformService.Snapshot.class).skills().isEmpty());
  var ids=skills.list(project).stream().limit(2).map(SkillService.View::id).toList();assertEquals(2,skills.pin(project,ids).size());
  assertThrows(IllegalArgumentException.class,()->skills.pin(project,java.util.stream.Stream.generate(UUID::randomUUID).limit(17).toList()));
 }

 @Test void personalPreferencesFollowAccountAcrossProjectsButProjectMemoryStaysScoped() throws Exception {
  var initial=preferences.get();assertEquals(0,initial.version());assertTrue(initial.values().empty());
  var values=new PersonalizationService.Values(" 中文 ","先给结论","遵循项目规范","说明验证结果");
  var saved=preferences.save(new PersonalizationService.Input(0,values));assertEquals(1,saved.version());assertEquals("中文",saved.values().responseLanguage());
  assertEquals(409,assertThrows(ResponseStatusException.class,()->preferences.save(new PersonalizationService.Input(0,values))).getStatusCode().value());
  var p=provider();
  agents.save(project,"MEMORY",null,new AgentPlatformService.Input("测试约束",0,null,new AgentPlatformService.Memory("run project tests","tests",false,true)));
  var first=create("model-small",p.id(),null);var firstSnapshot=json.readValue(first.getAgentSnapshot(),AgentPlatformService.Snapshot.class);
  assertEquals(saved.values(),firstSnapshot.personalization().values());assertTrue(firstSnapshot.personalization().present());
  assertEquals(1,firstSnapshot.memories().size());var evidence=firstSnapshot.memories().get(0);assertEquals("project",evidence.scope());assertTrue(evidence.score()>0);assertTrue(evidence.reason().contains("匹配"));
  UUID second=UUID.randomUUID();var project2=new ProjectEntity(second,"second","https://example.com/second.git","main",Instant.now());project2.setOwnerId(owner);projects.save(project2);
  var scope2=scopes.resolve(second,null,null);
  var secondTask=tasks.createWithProvider(second,"fix tests","model-small",null,scope2.workspaceId(),scope2.conversationId(),null,null,null,null,"CODE",null,p.id()).task();
  var secondSnapshot=json.readValue(secondTask.getAgentSnapshot(),AgentPlatformService.Snapshot.class);
  assertEquals(saved.values(),secondSnapshot.personalization().values());assertTrue(secondSnapshot.memories().isEmpty());
  UUID other=accounts.register("preference-other-"+UUID.randomUUID(),"fixture-password").user().id();login(other);
  assertTrue(preferences.get().values().empty());assertFalse(preferences.capture().present());
  preferences.save(new PersonalizationService.Input(0,new PersonalizationService.Values("English","","","")));
  login(owner);assertEquals(saved,preferences.get());
  SecurityContextHolder.clearContext();assertThrows(ResponseStatusException.class,()->preferences.get());assertFalse(preferences.capture().present());
 }

 @Test void projectMemoryRetrievalRanksFieldRelevanceAndHonorsPinnedAndEnabledState() {
  agents.save(project,"MEMORY",null,new AgentPlatformService.Input("正文提及",0,null,new AgentPlatformService.Memory("Spring Boot 部署 Spring Boot 部署 Spring Boot","general",false,true)));
  agents.save(project,"MEMORY",null,new AgentPlatformService.Input("Spring Boot 部署",0,null,new AgentPlatformService.Memory("使用发布流水线", "release pipeline",false,true)));
  agents.save(project,"MEMORY",null,new AgentPlatformService.Input("固定约束",0,null,new AgentPlatformService.Memory("始终保留", "unrelated",true,true)));
  agents.save(project,"MEMORY",null,new AgentPlatformService.Input("停用约束",0,null,new AgentPlatformService.Memory("不应检索", "Spring Boot 部署",false,false)));
  var evidence=agents.retrieve(project,"Spring Boot 部署");
  assertEquals(List.of("固定约束","Spring Boot 部署","正文提及"),evidence.stream().map(AgentPlatformService.MemoryEvidence::name).toList());
  assertTrue(evidence.get(1).score()>evidence.get(2).score());
  assertTrue(evidence.get(1).reason().contains("稀有度"));
  assertTrue(agents.retrieve(project,"completely unrelated query").stream().allMatch(value->value.name().equals("固定约束")));
 }

 @Test void projectMemoryRetrievalMeetsLabeledDeveloperQueryQualityFloor() {
  record Case(String query,String expected){ }
  List<Case> cases=List.of(
   new Case("测试优先再修改代码", "测试优先"),
   new Case("聊天长会话滚动阅读位置", "聊天阅读位置"),
   new Case("应用补丁保留已有本地修改", "补丁冲突保护"),
   new Case("MCP OAuth 授权过期刷新", "MCP 授权续期"),
   new Case("回答中文简洁表达风格", "交互偏好"),
   new Case("PostgreSQL 数据库迁移版本", "数据库迁移")
  );
  Map<String,String> corpus=new LinkedHashMap<>();
  corpus.put("测试优先", "先运行单元测试和回归测试，再修改代码；修复后再次测试。|TDD 测试 单测 验证");
  corpus.put("聊天阅读位置", "长对话向上阅读时保留阅读锚点，新消息不要强制滚到底部。|聊天 滚动 长历史 阅读位置 锚点");
  corpus.put("补丁冲突保护", "应用 Agent 补丁前检查工作区，保留用户未保存草稿和本地修改。|Git patch apply 草稿 冲突");
  corpus.put("MCP 授权续期", "远程 MCP OAuth access token 过期后通过 refresh token 刷新，并保留授权状态。|MCP OAuth token refresh bearer");
  corpus.put("交互偏好", "使用中文，先给结论，表达简洁；当前用户要求优先于旧偏好。|中文 简短 语气风格");
  corpus.put("数据库迁移", "PostgreSQL schema 通过版本化迁移管理，迁移需要回滚检查和集成测试。|PostgreSQL 数据库 migration schema");
  corpus.put("通用工程原则", "修改代码前阅读现有实现，避免破坏兼容性，并运行工程验证。|代码 测试 项目");
  corpus.put("浏览器预览", "内部浏览器支持链接导航、页面检查和错误反馈。|浏览器 页面 URL");
  corpus.put("MCP 设置", "MCP 服务可以配置地址、认证头和工具目录。|MCP 工具 服务 配置");
  corpus.put("数据库规范", "数据库访问必须遵循项目的已有连接与安全约束。|数据库 表 查询");
  corpus.forEach((name,value)->{
   String[] fields=value.split("\\|",2);
   agents.save(project,"MEMORY",null,new AgentPlatformService.Input(name,0,null,new AgentPlatformService.Memory(fields[0],fields[1],false,true)));
  });

  int relevantInTopThree=0;double reciprocalRankSum=0;
  for(Case item:cases){
   var ranked=agents.retrieve(project,item.query());
   int rank=1;
   while(rank<=ranked.size()&&!ranked.get(rank-1).name().equals(item.expected()))rank++;
   if(rank<=3)relevantInTopThree++;
   if(rank<=5)reciprocalRankSum+=1.0/rank;
  }
  double recallAtThree=(double)relevantInTopThree/cases.size();
  double meanReciprocalRank=reciprocalRankSum/cases.size();
  assertTrue(recallAtThree>=0.83,"labeled project-memory Recall@3 was "+recallAtThree);
  assertTrue(meanReciprocalRank>=0.65,"labeled project-memory MRR@5 was "+meanReciprocalRank);
 }

 @Test void preferenceEditsAndClearingDoNotAlterExistingTasksOrIdempotentRequestsAndRetry() throws Exception {
  var p=provider();var saved=preferences.save(new PersonalizationService.Input(0,new PersonalizationService.Values("中文","简短回答","","")));
  var task=create("model-small",p.id(),"preference-pinned");String snapshot=task.getAgentSnapshot(),hash=task.getAgentSnapshotHash();
  var changed=preferences.save(new PersonalizationService.Input(saved.version(),new PersonalizationService.Values("English","详细解释","","")));
  assertThrows(ResponseStatusException.class,()->preferences.save(new PersonalizationService.Input(saved.version(),saved.values())));
  assertEquals(snapshot,create("model-small",p.id(),"preference-pinned").getAgentSnapshot());
  var fresh=create("model-small",p.id(),null);assertEquals("English",json.readValue(fresh.getAgentSnapshot(),AgentPlatformService.Snapshot.class).personalization().values().responseLanguage());
  preferences.save(new PersonalizationService.Input(changed.version(),new PersonalizationService.Values("","","","")));
  assertFalse(json.readValue(create("model-small",p.id(),null).getAgentSnapshot(),AgentPlatformService.Snapshot.class).personalization().present());
  var failed=taskRepository.findById(task.getId()).orElseThrow();failed.claim(UUID.randomUUID(),Instant.now());failed.fail("fixture");taskRepository.saveAndFlush(failed);
  var retried=tasks.retry(task.getId());assertEquals(snapshot,retried.getAgentSnapshot());assertEquals(hash,retried.getAgentSnapshotHash());
  assertThrows(IllegalArgumentException.class,()->new PersonalizationService.Values("x".repeat(81),"","",""));
  assertThrows(IllegalArgumentException.class,()->new PersonalizationService.Values("","x".repeat(2001),"",""));
  assertThrows(IllegalArgumentException.class,()->preferences.save(new PersonalizationService.Input(0,null)));
 }

 @Test void bindsAndPinsModelAcrossProfileChangesDeletionRetryAndIdempotency() throws Exception {
  var p=provider();var choice=new AgentPlatformService.ModelChoice(p.id(),"model-large");var profile=agents.save(project,"AGENT",null,input(0,choice));agents.select(project,scope.conversationId(),new AgentPlatformService.Selection(profile.id()));
  var task=create("model-large",p.id(),"model-request");
  var snapshot=json.readValue(task.getAgentSnapshot(),AgentPlatformService.Snapshot.class);assertEquals(choice,snapshot.profile().modelChoice());assertEquals("model-large",task.getModel());assertEquals(p.id(),task.getProviderId());
  String role=task.getAgentSnapshot(),connection=task.getProviderSnapshot();
  agents.save(project,"AGENT",profile.id(),input(profile.version(),new AgentPlatformService.ModelChoice(p.id(),"model-small")));
  assertEquals(task.getId(),create("model-large",p.id(),"model-request").getId());
  assertThrows(ResponseStatusException.class,()->create("model-small",p.id(),"model-request"));
  var mismatch=assertThrows(ResponseStatusException.class,()->create("model-large",p.id(),null));assertEquals(409,mismatch.getStatusCode().value());
  providers.delete(p.id());
  assertEquals(task.getId(),create("model-large",p.id(),"model-request").getId());
  assertThrows(ResponseStatusException.class,()->create("model-small",p.id(),null));
  task.claim(UUID.randomUUID(),Instant.now());task.fail("fixture");taskRepository.saveAndFlush(task);
  var retried=tasks.retry(task.getId());assertEquals(role,retried.getAgentSnapshot());assertEquals(connection,retried.getProviderSnapshot());assertEquals("model-large",retried.getModel());
  UUID runner=UUID.randomUUID();retried.claim(runner,Instant.now());taskRepository.saveAndFlush(retried);assertEquals("fixture-key",providers.resolve(retried,runner,retried.getAttempt()).apiKey());
 }

 @Test void rejectsForeignUnknownAndIncompleteBindingsAndCanReturnToComposer() {
  var p=provider();
  assertThrows(IllegalArgumentException.class,()->agents.save(project,"AGENT",null,input(0,new AgentPlatformService.ModelChoice(null,"model-small"))));
  assertThrows(IllegalArgumentException.class,()->agents.save(project,"AGENT",null,input(0,new AgentPlatformService.ModelChoice(p.id(),""))));
  assertThrows(IllegalArgumentException.class,()->agents.save(project,"AGENT",null,input(0,new AgentPlatformService.ModelChoice(p.id(),"not-in-catalog"))));
  assertThrows(ResponseStatusException.class,()->agents.save(project,"AGENT",null,input(0,new AgentPlatformService.ModelChoice(UUID.randomUUID(),"model-small"))));
  var foreign=accounts.register("agent-foreign-"+UUID.randomUUID(),"fixture-password").user().id();login(foreign);var foreignProvider=provider();login(owner);
  assertThrows(ResponseStatusException.class,()->agents.save(project,"AGENT",null,input(0,new AgentPlatformService.ModelChoice(foreignProvider.id(),"model-small"))));
  var profile=agents.save(project,"AGENT",null,input(0,new AgentPlatformService.ModelChoice(p.id(),"model-large")));agents.select(project,scope.conversationId(),new AgentPlatformService.Selection(profile.id()));
  agents.save(project,"AGENT",profile.id(),input(profile.version(),null));
  var task=create("model-small",p.id(),null);assertEquals("model-small",task.getModel());
  agents.delete(project,"AGENT",profile.id(),agents.list(project,"AGENT").get(0).version());
  assertEquals("model-large",create("model-large",p.id(),null).getModel());
 }

 @Test void remoteMcpIsEncryptedProjectScopedPinnedToTaskAndResolvedOnlyUnderLease() throws Exception {
  var provider=provider();
  var saved=mcpServers.save(project,null,new McpServerService.Input("project_docs","http://127.0.0.1:43210/mcp","private-fixture-token",false,"X-Docs-Key","private-header-secret",false,true,0));
  assertTrue(saved.hasBearer());assertTrue(saved.hasHeader());assertEquals("X-Docs-Key",saved.headerName());assertFalse(json.writeValueAsString(saved).contains("private-fixture-token"));assertFalse(json.writeValueAsString(saved).contains("private-header-secret"));
  var resource=agentResources.findById(saved.id()).orElseThrow();assertFalse(resource.body.contains("private-fixture-token"));assertFalse(resource.body.contains("private-header-secret"));
  var profileInput=new AgentPlatformService.Input("项目文档助手",0,new AgentPlatformService.Profile("查项目文档",List.of(),List.of(),false,null,List.of(saved.id())),null);
  var profile=agents.save(project,"AGENT",null,profileInput);agents.select(project,scope.conversationId(),new AgentPlatformService.Selection(profile.id()));
  var task=create("model-small",provider.id(),null);String frozen=task.getAgentSnapshot();
  var snapshot=json.readValue(frozen,AgentPlatformService.Snapshot.class);assertEquals(saved.id(),snapshot.remoteMcp().get(0).id());assertFalse(frozen.contains("private-fixture-token"));assertFalse(frozen.contains("private-header-secret"));
  UUID runner=UUID.randomUUID();task.claim(runner,Instant.now());taskRepository.saveAndFlush(task);
  var resolved=mcpApi.resolve(task.getId(),runner,task.getAttempt()).getBody();assertEquals("private-fixture-token",resolved.get(0).bearer());assertEquals("X-Docs-Key",resolved.get(0).headerName());assertEquals("private-header-secret",resolved.get(0).headerValue());assertEquals(saved.url(),resolved.get(0).url());
  assertThrows(ResponseStatusException.class,()->mcpApi.resolve(task.getId(),UUID.randomUUID(),task.getAttempt()));
  var changed=mcpServers.save(project,saved.id(),new McpServerService.Input("project_docs","http://127.0.0.1:43211/mcp","new-private-token",false,"X-Docs-Key","new-header-secret",false,true,saved.version()));
  mcpServers.delete(project,changed.id(),changed.version());
  assertEquals(frozen,task.getAgentSnapshot());assertEquals("private-fixture-token",mcpServers.connections(snapshot.remoteMcp()).get(0).bearer());assertEquals("private-header-secret",mcpServers.connections(snapshot.remoteMcp()).get(0).headerValue());
  var failureTask=taskRepository.findById(task.getId()).orElseThrow();failureTask.fail("fixture");taskRepository.saveAndFlush(failureTask);var retry=tasks.retry(task.getId());assertEquals(frozen,retry.getAgentSnapshot());
  assertThrows(ResponseStatusException.class,()->mcpServers.save(UUID.randomUUID(),null,new McpServerService.Input("foreign","http://127.0.0.1:43210/mcp","",false,true,0)));
  assertThrows(IllegalArgumentException.class,()->mcpServers.save(project,null,new McpServerService.Input("bad","file:///etc/passwd","",false,true,0)));
 }

 @Test void remoteMcpRejectsStaleForeignDisabledDuplicateAndUnsafeCredentialChanges(){
  var saved=mcpServers.save(project,null,new McpServerService.Input("docs","http://127.0.0.1:43210/mcp","fixture-token",false,true,0));
  assertThrows(IllegalArgumentException.class,()->mcpServers.save(project,saved.id(),new McpServerService.Input("docs","http://127.0.0.1:43211/mcp","",false,true,saved.version())));
  assertThrows(ResponseStatusException.class,()->mcpServers.save(project,null,new McpServerService.Input("docs",saved.url(),"",false,true,0)));
  assertThrows(ResponseStatusException.class,()->mcpServers.save(project,saved.id(),new McpServerService.Input("docs",saved.url(),"",false,true,999)));
  assertThrows(IllegalArgumentException.class,()->mcpServers.pin(project,List.of(saved.id(),saved.id())));
  assertThrows(IllegalArgumentException.class,()->mcpServers.pin(project,Arrays.asList((UUID)null)));
  UUID otherProject=UUID.randomUUID();var p=new ProjectEntity(otherProject,"other","https://example.com/other.git","main",Instant.now());p.setOwnerId(owner);projects.save(p);
  assertThrows(ResponseStatusException.class,()->mcpServers.pin(otherProject,List.of(saved.id())));
  var duplicate=new AgentPlatformService.Input("duplicate",0,new AgentPlatformService.Profile("",List.of(),List.of("docs"),false,null,List.of(saved.id())),null);
  assertThrows(IllegalArgumentException.class,()->agents.save(project,"AGENT",null,duplicate));
  var disabled=mcpServers.save(project,saved.id(),new McpServerService.Input("docs",saved.url(),"",false,false,saved.version()));
  assertThrows(ResponseStatusException.class,()->mcpServers.pin(project,List.of(saved.id())));
  var cleared=mcpServers.save(project,saved.id(),new McpServerService.Input("docs","http://127.0.0.1:43211/mcp","",true,true,disabled.version()));assertFalse(cleared.hasBearer());
  var keyed=mcpServers.save(project,null,new McpServerService.Input("keyed","http://127.0.0.1:43210/mcp","",false,"X-Api-Key","fixture-header-secret",false,true,0));
  assertThrows(IllegalArgumentException.class,()->mcpServers.save(project,null,new McpServerService.Input("protected","https://example.com/mcp","",false,"Authorization","fixture",false,true,0)));
  assertThrows(IllegalArgumentException.class,()->mcpServers.save(project,null,new McpServerService.Input("newline","https://example.com/mcp","",false,"X-Api-Key","fixture\r\nInjected: yes",false,true,0)));
  var preserved=mcpServers.save(project,keyed.id(),new McpServerService.Input(keyed.name(),keyed.url(),"",false,keyed.headerName(),"",false,false,keyed.version()));assertTrue(preserved.hasHeader());
  var clearedHeader=mcpServers.save(project,keyed.id(),new McpServerService.Input(keyed.name(),keyed.url(),"",false,"","",true,false,preserved.version()));assertFalse(clearedHeader.hasHeader());
  UUID foreign=accounts.register("mcp-foreign-"+UUID.randomUUID(),"fixture-password").user().id();login(foreign);
  assertThrows(ResponseStatusException.class,()->mcpServers.list(project));assertThrows(ResponseStatusException.class,()->mcpServers.delete(project,cleared.id(),cleared.version()));
 }
}
