package dev.proofcode.control.agentplatform;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.session.WorkspaceService;
import dev.proofcode.control.task.TaskEntity;
import java.util.*;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;

@Service
public class AgentPlatformService {
 private final AgentResourceRepository resources; private final WorkspaceService scopes; private final ObjectMapper json; private final dev.proofcode.control.provider.ProviderService providers; private final McpServerService mcp; private final PersonalizationService personalization; private final SkillService skills;
 public AgentPlatformService(AgentResourceRepository resources,WorkspaceService scopes,ObjectMapper json,dev.proofcode.control.provider.ProviderService providers,McpServerService mcp,PersonalizationService personalization,SkillService skills){this.resources=resources;this.scopes=scopes;this.json=json;this.providers=providers;this.mcp=mcp;this.personalization=personalization;this.skills=skills;}
 public record ModelChoice(UUID providerId,String model){}
 public record Profile(String instructions,List<String> allowedTools,List<String> mcpServers,boolean readOnly,ModelChoice modelChoice,List<UUID> remoteMcpIds,List<UUID> skillIds){
  public Profile { remoteMcpIds=remoteMcpIds==null?List.of():Collections.unmodifiableList(new ArrayList<>(remoteMcpIds));skillIds=skillIds==null?List.of():Collections.unmodifiableList(new ArrayList<>(skillIds)); }
  public Profile(String instructions,List<String> allowedTools,List<String> mcpServers,boolean readOnly,ModelChoice modelChoice,List<UUID> remoteMcpIds){this(instructions,allowedTools,mcpServers,readOnly,modelChoice,remoteMcpIds,List.of());}
  public Profile(String instructions,List<String> allowedTools,List<String> mcpServers,boolean readOnly){this(instructions,allowedTools,mcpServers,readOnly,null,List.of());}
  public Profile(String instructions,List<String> allowedTools,List<String> mcpServers,boolean readOnly,ModelChoice modelChoice){this(instructions,allowedTools,mcpServers,readOnly,modelChoice,List.of());}
 }
 public record Memory(String content,String keywords,boolean pinned,boolean enabled){}
 public record Input(String name,long version,Profile profile,Memory memory){}
 public record Selection(UUID profileId){}
 public record MemoryEvidence(UUID id,String name,String content,long version,String scope,int score,String reason){}
 public record Snapshot(UUID profileId,String profileName,long profileVersion,Profile profile,List<MemoryEvidence> memories,List<McpServerService.Pinned> remoteMcp,PersonalizationService.Snapshot personalization,List<SkillService.Pinned> skills){}
 public record View(UUID id,String name,long version,Profile profile,Memory memory){}

 public List<View> list(UUID project,String kind){scopes.requireProject(project);validateKind(kind);return resources.findByProjectIdAndKindOrderByNameAsc(project,kind).stream().map(this::view).toList();}
 @Transactional public View save(UUID project,String kind,UUID id,Input input){
  scopes.requireProject(project);validateKind(kind);
  if(input==null||input.name()==null||input.name().isBlank()||input.name().trim().length()>160)throw new IllegalArgumentException("name must contain 1–160 characters");
  Object body;
  if(kind.equals("AGENT")){
   Profile profile=input.profile();
   if(profile==null||profile.instructions()==null||profile.instructions().length()>16000||profile.allowedTools()==null||profile.mcpServers()==null)throw new IllegalArgumentException("invalid agent profile");
   validateNames(profile.allowedTools(),128,"[A-Za-z0-9_.-]{1,128}");validateNames(profile.mcpServers(),16,"[A-Za-z][A-Za-z0-9_-]{0,31}");
   skills.pin(project,profile.skillIds());
   var remote=mcp.pin(project,profile.remoteMcpIds());if(remote.size()+profile.mcpServers().size()>16||remote.stream().anyMatch(v->profile.mcpServers().contains(v.name())))throw new IllegalArgumentException("MCP 服务最多共 16 个，远程服务与运行环境服务的别名不能重复");
   if(profile.modelChoice()!=null){var choice=profile.modelChoice();if(choice.providerId()==null||choice.model()==null||choice.model().isBlank()||choice.model().length()>200)throw new IllegalArgumentException("请选择智能体的模型服务和模型，或使用输入区选择");var provider=providers.view(providers.owned(choice.providerId()));if(provider.models().stream().noneMatch(v->v.id().equals(choice.model())))throw new IllegalArgumentException("智能体模型不在该服务目录中，请刷新目录后重新选择");}
   body=profile;
  }else{
   Memory memory=input.memory();if(memory==null||memory.content()==null||memory.content().isBlank()||memory.content().length()>8000||memory.keywords()==null||memory.keywords().length()>1000)throw new IllegalArgumentException("invalid memory");body=memory;
  }
  AgentResource value;
  if(id==null){if(resources.findByProjectIdAndKindOrderByNameAsc(project,kind).size()>=200)throw conflict("resource limit reached");value=new AgentResource(project,kind,input.name().trim(),encode(body));}
  else{value=require(project,kind,id);if(value.version!=input.version())throw conflict("configuration changed; reload before saving");value.name=input.name().trim();value.body=encode(body);}
  return view(resources.saveAndFlush(value));
 }
 @Transactional public void delete(UUID project,String kind,UUID id,long version){scopes.requireProject(project);validateKind(kind);var value=require(project,kind,id);if(value.version!=version)throw conflict("configuration changed; reload before deleting");resources.delete(value);}
 public Selection selection(UUID project,UUID conversation){scopes.requireConversation(project,conversation);return resources.findByProjectIdAndKindAndName(project,"BINDING",conversation.toString()).map(v->decode(v.body,Selection.class)).orElse(new Selection(null));}
 @Transactional public Selection select(UUID project,UUID conversation,Selection selection){
  scopes.resolve(project,null,conversation);if(selection==null)throw new IllegalArgumentException("selection required");if(selection.profileId()!=null)require(project,"AGENT",selection.profileId());
  var value=resources.findByProjectIdAndKindAndName(project,"BINDING",conversation.toString()).orElseGet(()->new AgentResource(project,"BINDING",conversation.toString(),"{}"));value.body=encode(selection);resources.save(value);return selection;
 }
 // Captured once before enqueue; neither profile edits nor memory edits alter an in-flight task.
 public void capture(TaskEntity task,UUID providerId){
  UUID project=task.getProjectId();
  Selection selection=selection(project,task.getConversationId());
  AgentResource chosen=selection.profileId()==null?null:resources.findById(selection.profileId()).filter(v->v.projectId.equals(project)&&v.kind.equals("AGENT")).orElse(null);
  Profile profile=chosen==null?new Profile("",List.of(),List.of(),false):decode(chosen.body,Profile.class);
  if(profile.modelChoice()!=null&&(!Objects.equals(profile.modelChoice().providerId(),providerId)||!Objects.equals(profile.modelChoice().model(),task.getModel())))throw conflict("智能体模型配置已变化，请重新选择智能体后发送");
  var remote=mcp.pin(project,profile.remoteMcpIds());if(remote.size()+profile.mcpServers().size()>16||remote.stream().anyMatch(v->profile.mcpServers().contains(v.name())))throw conflict("MCP 服务别名重复或数量超限，请重新配置智能体");
  String snapshot=encode(new Snapshot(chosen==null?null:chosen.id,chosen==null?"默认智能体":chosen.name,chosen==null?0:chosen.version,profile,retrieve(project,task.getPrompt()),remote,personalization.capture(),skills.pin(project,chosen==null?null:profile.skillIds())));
  if(snapshot.getBytes(java.nio.charset.StandardCharsets.UTF_8).length>480*1024)throw conflict("智能体配置和 Skills 总内容过大，请减少选择");
  task.captureAgent(snapshot);
 }
 public List<MemoryEvidence> retrieve(UUID project,String query){
  scopes.requireProject(project);
  Set<String> queryTerms=terms(query);
  var corpus=resources.findByProjectIdAndKindOrderByNameAsc(project,"MEMORY").stream().map(v->new Candidate(v,decode(v.body,Memory.class))).filter(v->v.memory().enabled()).toList();
  List<IndexedMemory> indexed=corpus.stream().map(candidate->new IndexedMemory(candidate,frequencies(candidate.resource().name),frequencies(candidate.memory().keywords()),frequencies(candidate.memory().content()))).toList();
  Map<String,Integer> documentFrequency=new HashMap<>();
  for(var candidate:indexed){Set<String> documentTerms=new HashSet<>();documentTerms.addAll(candidate.title().keySet());documentTerms.addAll(candidate.keywords().keySet());documentTerms.addAll(candidate.content().keySet());for(String term:documentTerms)documentFrequency.merge(term,1,Integer::sum);}
  var matches=indexed.stream().map(index->{Candidate candidate=index.candidate();Memory memory=candidate.memory();double lexical=0;int matched=0;
   for(String term:queryTerms){boolean found=false;double rarity=1.0+Math.log((corpus.size()+1.0)/(documentFrequency.getOrDefault(term,0)+0.5));double titleFrequency=index.title().getOrDefault(term,0),keywordFrequency=index.keywords().getOrDefault(term,0),contentFrequency=index.content().getOrDefault(term,0);if(titleFrequency>0){lexical+=4.0*rarity*titleFrequency/(titleFrequency+1.0);found=true;}if(keywordFrequency>0){lexical+=3.0*rarity*keywordFrequency/(keywordFrequency+1.0);found=true;}if(contentFrequency>0){lexical+=rarity*contentFrequency/(contentFrequency+1.0);found=true;}if(found)matched++;}
   String normalizedQuery=normalizePhrase(query);String normalizedAll=normalizePhrase(candidate.resource().name+" "+memory.keywords()+" "+memory.content());if(normalizedQuery.length()>=4&&normalizedAll.contains(normalizedQuery))lexical+=6.0;
   int score=(int)Math.round(lexical*100);return new Ranked(candidate.resource(),memory,matched,score,memory.pinned()?1:0);
  }).filter(v->v.memory().pinned()||v.lexicalScore()>0).sorted(Comparator.comparingInt(Ranked::pinned).reversed().thenComparing(Comparator.comparingInt(Ranked::score).reversed()).thenComparing(v->v.resource().id)).toList();
  List<MemoryEvidence> result=new ArrayList<>();int remaining=12000;
  for(var match:matches){if(result.size()==8)break;if(match.memory().content().length()>remaining)continue;var v=match.resource();String reason=match.pinned()==1?"你将它设为始终参考":("匹配 "+match.lexicalScore()+" 个词项；分数结合标题、关键词、正文、词项稀有度和短语");result.add(new MemoryEvidence(v.id,v.name,match.memory().content(),v.version,"project",match.score(),reason));remaining-=match.memory().content().length();}
  return List.copyOf(result);
 }
 private record Candidate(AgentResource resource,Memory memory){}
 private record IndexedMemory(Candidate candidate,Map<String,Integer> title,Map<String,Integer> keywords,Map<String,Integer> content){}
 private record Ranked(AgentResource resource,Memory memory,int lexicalScore,int score,int pinned){}
 // Latin identifiers plus overlapping CJK bigrams; no network embedding service or automatic capture.
 static Set<String> terms(String text){return frequencies(text).keySet();}
 private static Map<String,Integer> frequencies(String text){Map<String,Integer> result=new HashMap<>();var matcher=java.util.regex.Pattern.compile("[a-z0-9_]{2,}|[\\p{IsHan}]+").matcher(text.toLowerCase(Locale.ROOT));while(matcher.find()){String word=matcher.group();if(word.codePoints().anyMatch(c->Character.UnicodeScript.of(c)==Character.UnicodeScript.HAN)){int[] points=word.codePoints().toArray();for(int i=0;i+1<points.length;i++)result.merge(new String(points,i,2),1,Integer::sum);}else result.merge(word,1,Integer::sum);}return result;}
 private static String normalizePhrase(String text){return text.toLowerCase(Locale.ROOT).replaceAll("[^a-z0-9\\p{IsHan}]+","").trim();}
 private AgentResource require(UUID project,String kind,UUID id){return resources.findById(id).filter(v->v.projectId.equals(project)&&v.kind.equals(kind)).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));}
 private View view(AgentResource v){return new View(v.id,v.name,v.version,v.kind.equals("AGENT")?decode(v.body,Profile.class):null,v.kind.equals("MEMORY")?decode(v.body,Memory.class):null);}
 private static void validateKind(String kind){if(!Set.of("AGENT","MEMORY").contains(kind))throw new IllegalArgumentException("invalid resource kind");}
 private static void validateNames(List<String> names,int max,String pattern){if(names.size()>max||new HashSet<>(names).size()!=names.size()||names.stream().anyMatch(v->v==null||!v.matches(pattern)))throw new IllegalArgumentException("invalid or duplicate tool/server names");}
 private String encode(Object value){try{return json.writeValueAsString(value);}catch(Exception e){throw new IllegalStateException(e);}}
 private <T>T decode(String text,Class<T> type){try{return json.readValue(text,type);}catch(Exception e){throw new IllegalStateException("invalid saved agent resource",e);}}
 private static ResponseStatusException conflict(String message){return new ResponseStatusException(HttpStatus.CONFLICT,message);}
}
