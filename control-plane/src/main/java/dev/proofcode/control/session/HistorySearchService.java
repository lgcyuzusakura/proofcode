package dev.proofcode.control.session;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.account.AccountService;
import jakarta.persistence.EntityManager;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.*;
import org.springframework.http.HttpStatus;
import org.springframework.security.core.context.SecurityContextHolder;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;

/** Explicit account-scoped keyword search; results are never injected into model context. */
@Service
public class HistorySearchService {
 private final EntityManager database;private final WorkspaceService scopes;private final ObjectMapper json;
 public HistorySearchService(EntityManager database,WorkspaceService scopes,ObjectMapper json){this.database=database;this.scopes=scopes;this.json=json;}
 public enum Kind { ALL, CONVERSATION, MESSAGE, TASK }
 public record Hit(Kind kind,UUID id,UUID projectId,String projectName,UUID workspaceId,UUID conversationId,String conversationTitle,UUID taskId,UUID messageId,String role,String excerpt,Instant createdAt){}
 public record Page(List<Hit> items,String nextCursor){}
 private record Cursor(String fingerprint,Instant snapshot,Instant before,Kind kind,UUID id){}
 private static final Comparator<Hit> ORDER=Comparator.comparing(Hit::createdAt).reversed().thenComparing(Hit::kind).thenComparing(hit->hit.id().toString());

 @Transactional(readOnly=true)
 public Page search(String text,UUID project,Kind kind,String cursor,int limit){
  var auth=SecurityContextHolder.getContext().getAuthentication();UUID owner=AccountService.currentId();
  if(auth==null||!auth.isAuthenticated()||(owner==null&&!"user".equals(auth.getPrincipal())))throw new ResponseStatusException(HttpStatus.UNAUTHORIZED,"请先登录后搜索历史");
  String needle=text==null?"":text.strip().toLowerCase(Locale.ROOT);
  if(needle.length()>100||needle.chars().anyMatch(Character::isISOControl)||limit<1||limit>50||kind==null)throw new IllegalArgumentException("搜索词最多 100 字符，每页 1–50 条");
  if(project!=null)scopes.requireProject(project);
  String fingerprint=ConversationContextService.hash(owner+"|"+project+"|"+kind+"|"+needle);
  Cursor after=decode(cursor,fingerprint);Instant snapshot=after==null?Instant.now():after.snapshot();
  List<Hit> candidates=new ArrayList<>();
  for(Kind type:List.of(Kind.CONVERSATION,Kind.MESSAGE,Kind.TASK)){
   if(kind!=Kind.ALL&&kind!=type)continue;
   String entity,content,fields,joins;
   switch(type){
    case CONVERSATION->{entity="ConversationEntity";content="e.title";fields="e.workspaceId, e.id, e.title, null, null";joins="";}
    case MESSAGE->{entity="ConversationMessage";content="e.content";fields="e.workspaceId, e.conversationId, c.title, e.taskId, e.role";joins=" join ConversationEntity c on c.id=e.conversationId and c.projectId=e.projectId and c.workspaceId=e.workspaceId";}
    case TASK->{entity="TaskEntity";content="concat(e.prompt, ' ', coalesce(e.result, ''))";fields="e.workspaceId, e.conversationId, c.title, e.id, null";joins=" join ConversationEntity c on c.id=e.conversationId and c.projectId=e.projectId and c.workspaceId=e.workspaceId";}
    default->throw new IllegalArgumentException("无效的搜索范围");
   }
   String snippet="substring("+content+", greatest(1, locate(:needle, lower("+content+"))-70), 280)";
   String query="select e.id, p.id, p.name, "+fields+", "+snippet+", e.createdAt from "+entity+" e join ProjectEntity p on p.id=e.projectId"+joins+" where "+(owner==null?"p.ownerId is null":"p.ownerId=:owner")+" and e.createdAt<=:snapshot";
   if(project!=null)query+=" and p.id=:project";
   if(!needle.isEmpty())query+=" and lower("+content+") like :pattern escape '!'";
   if(after!=null){
    int comparison=type.compareTo(after.kind());
    query+=" and (e.createdAt<:before"+(comparison>0?" or e.createdAt=:before":comparison==0?" or (e.createdAt=:before and cast(e.id as string)>:afterId)":"")+")";
   }
   query+=" order by e.createdAt desc, cast(e.id as string) asc";
   var request=database.createQuery(query,Object[].class).setParameter("needle",needle).setParameter("snapshot",snapshot).setMaxResults(limit+1).setHint("jakarta.persistence.query.timeout",5000);
   if(owner!=null)request.setParameter("owner",owner);if(project!=null)request.setParameter("project",project);
   if(!needle.isEmpty())request.setParameter("pattern","%"+needle.replace("!","!!").replace("%","!%").replace("_","!_")+"%");
   if(after!=null){request.setParameter("before",after.before());if(type==after.kind())request.setParameter("afterId",after.id().toString());}
   for(Object[] row:request.getResultList()){
    UUID id=(UUID)row[0];candidates.add(new Hit(type,id,(UUID)row[1],(String)row[2],(UUID)row[3],(UUID)row[4],(String)row[5],(UUID)row[6],type==Kind.MESSAGE?id:null,(String)row[7],(String)row[8],(Instant)row[9]));
   }
  }
  candidates.sort(ORDER);boolean more=candidates.size()>limit;var items=List.copyOf(candidates.subList(0,Math.min(limit,candidates.size())));
  String next=null;if(more){var last=items.get(items.size()-1);next=encode(new Cursor(fingerprint,snapshot,last.createdAt(),last.kind(),last.id()));}
  return new Page(items,next);
 }
 private Cursor decode(String raw,String fingerprint){
  if(raw==null||raw.isEmpty())return null;
  try{
   if(raw.length()>1024)throw new Exception();var value=json.readValue(Base64.getUrlDecoder().decode(raw),Cursor.class);
   if(!Objects.equals(value.fingerprint(),fingerprint)||value.snapshot()==null||value.before()==null||value.id()==null||value.kind()==null||value.kind()==Kind.ALL||value.before().isAfter(value.snapshot())||value.snapshot().isAfter(Instant.now().plusSeconds(1)))throw new Exception();return value;
  }catch(Exception invalid){throw new IllegalArgumentException("搜索分页已失效，请重新搜索");}
 }
 private String encode(Cursor value){try{return Base64.getUrlEncoder().withoutPadding().encodeToString(json.writeValueAsString(value).getBytes(StandardCharsets.UTF_8));}catch(Exception failure){throw new IllegalStateException("无法生成搜索分页");}}
}
