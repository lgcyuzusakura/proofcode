package dev.proofcode.control.agentplatform;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.session.WorkspaceService;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.*;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.dao.OptimisticLockingFailureException;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;
import org.yaml.snakeyaml.LoaderOptions;
import org.yaml.snakeyaml.Yaml;
import org.yaml.snakeyaml.constructor.SafeConstructor;

@Service
public class SkillService {
 private static final int MAX_BODY=24000, MAX_SELECTED=16, MAX_TOTAL=80000;
 private final AgentResourceRepository resources;
 private final WorkspaceService scopes;
 private final ObjectMapper json;
 public SkillService(AgentResourceRepository resources,WorkspaceService scopes,ObjectMapper json){this.resources=resources;this.scopes=scopes;this.json=json;}
 public record Body(String description,String content,boolean enabled,String source){}
 public record Input(String name,String description,String content,boolean enabled,long version){}
 public record ImportInput(String markdown,UUID id,long version,boolean enabled){}
 public record View(UUID id,String name,String description,String content,boolean enabled,String source,long version,String hash){}
 public record Pinned(UUID id,String name,String description,String content,String source,long version,String hash){}

 public List<View> list(UUID project){scopes.requireProject(project);return resources.findByProjectIdAndKindOrderByNameAsc(project,"SKILL").stream().map(this::view).toList();}
 @Transactional public View save(UUID project,UUID id,Input input){return persist(project,id,input,"editor");}
 @Transactional public View importMarkdown(UUID project,ImportInput input){
  scopes.requireProject(project);
  if(input==null||input.markdown()==null||input.markdown().length()>MAX_BODY+8192)throw invalid("SKILL.md 过大或内容为空");
  String markdown=input.markdown().replace("\r\n","\n");if(markdown.startsWith("\uFEFF"))markdown=markdown.substring(1);
  if(!markdown.startsWith("---\n"))throw invalid("SKILL.md 需要以 YAML 名称和简介开头");
  int end=markdown.indexOf("\n---\n",4);
  if(end<0||end>8192)throw invalid("SKILL.md 的 YAML 头部缺失或过长");
  Map<?,?> metadata;
  try{
   LoaderOptions options=new LoaderOptions();options.setAllowDuplicateKeys(false);options.setMaxAliasesForCollections(0);options.setNestingDepthLimit(8);options.setCodePointLimit(8192);
   Object parsed=new Yaml(new SafeConstructor(options)).load(markdown.substring(4,end));
   if(!(parsed instanceof Map<?,?> map))throw new IllegalArgumentException();metadata=map;
  }catch(Exception e){throw invalid("SKILL.md 的 YAML 头部无效，请检查名称、简介和重复字段");}
  if(!(metadata.get("name") instanceof String name)||!(metadata.get("description") instanceof String description))throw invalid("SKILL.md 需要字符串类型的 name 和 description");
  return persist(project,input.id(),new Input(name,description,markdown.substring(end+5).strip(),input.enabled(),input.version()),"SKILL.md");
 }
 private View persist(UUID project,UUID id,Input input,String source){
  scopes.requireProject(project);
  if(input==null||input.name()==null||!input.name().matches("[a-z0-9]+(?:-[a-z0-9]+)*")||input.name().length()>64)throw invalid("Skill 名称请使用不超过 64 位的小写字母、数字和连字符");
  if(input.description()==null||input.description().isBlank()||input.description().length()>500||input.content()==null||input.content().isBlank()||input.content().length()>MAX_BODY||input.content().indexOf('\0')>=0)throw invalid("请填写简介（最多 500 字符）和工作流程（最多 24000 字符）");
  if(input.content().lines().anyMatch(line->line.getBytes(StandardCharsets.UTF_8).length>16000))throw invalid("Skill 单行过长，请拆分段落或代码行后保存");
  var duplicate=resources.findByProjectIdAndKindAndName(project,"SKILL",input.name());
  if(duplicate.isPresent()&&!duplicate.get().id.equals(id))throw conflict("当前项目已有同名 Skill，请修改名称或编辑原条目");
  AgentResource value;
  if(id==null){if(resources.findByProjectIdAndKindOrderByNameAsc(project,"SKILL").size()>=100)throw conflict("每个项目最多保存 100 个 Skills");value=new AgentResource(project,"SKILL",input.name(),"{}");}
  else{value=require(project,id);if(value.version!=input.version())throw conflict("Skill 已发生变化，请重新打开后保存");value.name=input.name();}
  value.body=encode(new Body(input.description().strip(),input.content().strip(),input.enabled(),source));
  try{return view(resources.saveAndFlush(value));}
  catch(DataIntegrityViolationException|OptimisticLockingFailureException e){throw conflict("Skill 已发生变化或名称重复，请重新加载");}
 }
 @Transactional public void delete(UUID project,UUID id,long version){scopes.requireProject(project);var value=require(project,id);if(value.version!=version)throw conflict("Skill 已发生变化，请重新打开后删除");resources.delete(value);}
 // Null is reserved for the default agent's enabled catalog; custom agents bind explicit IDs.
 public List<Pinned> pin(UUID project,List<UUID> ids){
  scopes.requireProject(project);List<AgentResource> values;
  if(ids==null)values=resources.findByProjectIdAndKindOrderByNameAsc(project,"SKILL").stream().filter(v->body(v).enabled()).toList();
  else{
   if(ids.size()>MAX_SELECTED||ids.stream().anyMatch(Objects::isNull)||new HashSet<>(ids).size()!=ids.size())throw invalid("最多选择 16 个 Skills，不能重复或包含空项");
   values=ids.stream().map(id->require(project,id)).sorted(Comparator.comparing(v->v.name)).toList();
  }
  if(values.size()>MAX_SELECTED)throw conflict("默认智能体最多使用 16 个已启用 Skills，请停用部分条目或选择自定义智能体");
  List<Pinned> result=new ArrayList<>();int total=0;
  for(var value:values){var body=body(value);if(!body.enabled())throw conflict("所选 Skill 已停用，请取消勾选或重新启用");total+=body.content().length();if(total>MAX_TOTAL)throw conflict("所选 Skills 总正文超过 80000 字符，请减少选择");result.add(new Pinned(value.id,value.name,body.description(),body.content(),body.source(),value.version,hash(body.content())));}
  return List.copyOf(result);
 }
 private AgentResource require(UUID project,UUID id){return resources.findById(id).filter(v->v.projectId.equals(project)&&v.kind.equals("SKILL")).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND,"Skill 不属于当前项目或已删除"));}
 private Body body(AgentResource value){try{return json.readValue(value.body,Body.class);}catch(Exception e){throw new IllegalStateException("saved skill is invalid",e);}}
 private View view(AgentResource value){var body=body(value);return new View(value.id,value.name,body.description(),body.content(),body.enabled(),body.source(),value.version,hash(body.content()));}
 private String encode(Object value){try{return json.writeValueAsString(value);}catch(Exception e){throw new IllegalStateException(e);}}
 private static String hash(String value){try{return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(value.getBytes(StandardCharsets.UTF_8)));}catch(Exception e){throw new IllegalStateException(e);}}
 private static IllegalArgumentException invalid(String value){return new IllegalArgumentException(value);}
 private static ResponseStatusException conflict(String value){return new ResponseStatusException(HttpStatus.CONFLICT,value);}
}
