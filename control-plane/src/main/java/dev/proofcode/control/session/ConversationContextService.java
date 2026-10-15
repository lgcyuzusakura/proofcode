package dev.proofcode.control.session;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.task.TaskEntity;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.*;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;

/** The transcript is an archive; only this projection is provided to the runner. */
@Service
public class ConversationContextService {
    private final ConversationRepository conversations;
    private final ConversationMessageRepository messages;
    private final ContextPackRepository packs;
    private final WorkspaceService scopes;
    private final ObjectMapper json;
    private final dev.proofcode.control.task.TaskRepository tasks;
    public ConversationContextService(ConversationRepository conversations,ConversationMessageRepository messages,ContextPackRepository packs,WorkspaceService scopes,ObjectMapper json,dev.proofcode.control.task.TaskRepository tasks){
        this.conversations=conversations;this.messages=messages;this.packs=packs;this.scopes=scopes;this.json=json;this.tasks=tasks;
    }
    public record Item(UUID messageId,String state,String replacement){}
    public record View(String mode,List<Item> items,List<UUID> packIds){}
    public record InputMessage(UUID id,long sequence,String role,String content,UUID sourceConversationId,String sourceHash){
        public String getContent(){return content;} public long getSequence(){return sequence;}
    }
    public record PackManifest(List<InputMessage> messages,Map<UUID,String> sourceProjections){}
    public record PackInfo(UUID id,String name,UUID sourceConversationId,int messageCount,boolean stale){}
    public record Preview(long version,View view,List<InputMessage> messages,List<PackInfo> packs,int excludedTurns,long estimatedTokens,String selectionHash,String estimateMethod){}
    public record Snapshot(long viewVersion,String selectionHash,List<InputMessage> messages){}
    public static View automatic(){return new View("AUTO",List.of(),List.of());}
    private <T> T read(String value,Class<T> type){try{return json.readValue(value,type);}catch(Exception e){throw new IllegalStateException("invalid stored context",e);}}
    private String write(Object value){try{return json.writeValueAsString(value);}catch(Exception e){throw new IllegalStateException(e);}}
    private View view(ConversationEntity conversation){return conversation.getContextView()==null?automatic():read(conversation.getContextView(),View.class);}
    private static ResponseStatusException conflict(String reason){return new ResponseStatusException(HttpStatus.CONFLICT,reason);}
    public Preview preview(UUID project,UUID workspace,UUID conversation){return project(scopes.requireConversation(project,workspace,conversation),Long.MAX_VALUE,false);}
    @Transactional
    public Preview update(UUID project,UUID workspace,UUID conversation,long expectedVersion,View next){
        scopes.requireConversation(project,workspace,conversation);
        var entity=conversations.lockById(conversation).orElseThrow();
        if(entity.getContextVersion()!=expectedVersion)throw conflict("context changed; reload before saving");
        validate(entity,next);
        entity.updateContext(write(next));
        return project(entity,Long.MAX_VALUE,false);
    }
    private void validate(ConversationEntity entity,View next){
        if(next==null||next.mode()==null||!Set.of("AUTO","FOCUS").contains(next.mode())||next.items()==null||next.packIds()==null||next.items().size()>10000||next.packIds().size()>20)throw new IllegalArgumentException("invalid context view");
        Set<UUID> seen=new HashSet<>();
        for(Item item:next.items()){
            if(item==null||item.messageId()==null||item.state()==null||!seen.add(item.messageId())||!Set.of("INCLUDE","EXCLUDE","REPLACE").contains(item.state()))throw new IllegalArgumentException("invalid or duplicate context item");
            var message=messages.findById(item.messageId()).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));
            if(!entity.getId().equals(message.getConversationId())||!"user".equals(message.getRole()))throw new IllegalArgumentException("context edits require the user message of a complete turn");
            if("REPLACE".equals(item.state())&&(item.replacement()==null||item.replacement().isBlank()||item.replacement().length()>16000))throw new IllegalArgumentException("replacement summary is required, up to 16000 characters");
            if(!"REPLACE".equals(item.state())&&item.replacement()!=null)throw new IllegalArgumentException("replacement is only allowed for REPLACE");
        }
        if(new HashSet<>(next.packIds()).size()!=next.packIds().size())throw new IllegalArgumentException("duplicate context packs");
        for(UUID id:next.packIds()){if(id==null)throw new IllegalArgumentException("invalid context pack");requirePack(entity,id);}
    }
    private ContextPack requirePack(ConversationEntity target,UUID id){
        var pack=packs.findById(id).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));
        if(!pack.getProjectId().equals(target.getProjectId())||!pack.getWorkspaceId().equals(target.getWorkspaceId()))throw new ResponseStatusException(HttpStatus.NOT_FOUND);
        scopes.requireConversation(pack.getProjectId(),pack.getWorkspaceId(),pack.getSourceConversationId());
        return pack;
    }
    private String projectionIdentity(ConversationEntity entity){var current=view(entity);return hash(write(new View(current.mode(),current.items(),List.of())));}
    private boolean stale(PackManifest manifest){
        return manifest.sourceProjections().entrySet().stream().anyMatch(entry->conversations.findById(entry.getKey()).map(value->!projectionIdentity(value).equals(entry.getValue())).orElse(true));
    }
    private Preview project(ConversationEntity entity,long before,boolean strict){
        View current=view(entity);
        List<InputMessage> input=new ArrayList<>();List<PackInfo> mounted=new ArrayList<>();
        Set<String> identities=new HashSet<>();
        for(UUID id:current.packIds()){
            var pack=requirePack(entity,id);var manifest=read(pack.getManifest(),PackManifest.class);boolean stale=stale(manifest);
            mounted.add(new PackInfo(id,pack.getName(),pack.getSourceConversationId(),manifest.messages().size(),stale));
            if(stale){if(strict)throw conflict("mounted context pack is stale; detach it or create a fresh pack");continue;}
            for(var item:manifest.messages())if(identities.add(item.sourceConversationId()+":"+item.id()))input.add(item);
        }
        boolean manual="FOCUS".equals(current.mode());
        Map<UUID,Item> edits=new HashMap<>();if(manual)current.items().forEach(item->edits.put(item.messageId(),item));
        var archive=before==Long.MAX_VALUE?messages.findByConversationIdOrderBySequenceAsc(entity.getId()):messages.findByConversationIdAndSequenceLessThanOrderBySequenceAsc(entity.getId(),before);
        // Pair user and final answer by task/attempt. Never include a partial tool protocol.
        Map<String,List<ConversationMessage>> turns=new LinkedHashMap<>();
        archive.forEach(message->turns.computeIfAbsent(message.getTaskId()+":"+message.getAttempt(),key->new ArrayList<>()).add(message));
        int excluded=0;
        for(var turn:turns.values()){
            var user=turn.stream().filter(message->"user".equals(message.getRole())).findFirst().orElse(null);
            if(user==null)continue;
            Item edit=edits.get(user.getId());String state=edit==null?("FOCUS".equals(current.mode())?"EXCLUDE":"INCLUDE"):edit.state();
            if("EXCLUDE".equals(state)){excluded++;continue;}
            if("REPLACE".equals(state)){
                var item=new InputMessage(user.getId(),user.getSequence(),"user","[User-edited summary of an earlier turn]\n"+edit.replacement().trim(),entity.getId(),hash(write(turn)));
                if(identities.add(item.sourceConversationId()+":"+item.id()))input.add(item);
            }else for(var message:turn){
                if(!Set.of("COMPLETED","SUCCEEDED").contains(message.getStatus())||message.getContent().isBlank())continue;
                var item=new InputMessage(message.getId(),message.getSequence(),message.getRole(),message.getContent(),entity.getId(),hash(message.getContent()));
                if(identities.add(item.sourceConversationId()+":"+item.id()))input.add(item);
            }
        }
        if(strict)checkLimit(input);
        // Conservative UTF-8 byte estimate; provider usage remains the billing authority.
        long estimate=input.stream().mapToLong(item->item.content().getBytes(StandardCharsets.UTF_8).length+4L).sum();
        return new Preview(entity.getContextVersion(),current,List.copyOf(input),mounted,excluded,estimate,hash(write(input)),"UTF-8 bytes + 4 per message; excludes system/tools/current request; estimate only");
    }
    private static void checkLimit(List<InputMessage> values){
        if(values.size()>1000||values.stream().mapToLong(item->item.content().getBytes(StandardCharsets.UTF_8).length).sum()>2L<<20)throw new ResponseStatusException(HttpStatus.PAYLOAD_TOO_LARGE,"selected context exceeds 1000 messages or 2 MiB; exclude turns or replace them with summaries");
    }
    @Transactional
    public void capture(TaskEntity task,long before){
        if(task.getContextSnapshot()!=null||task.getExperimentId()!=null)return;
        var entity=conversations.lockById(task.getConversationId()).orElseThrow();
        var projection=project(entity,before,true);
        task.captureContext(write(new Snapshot(projection.version(),projection.selectionHash(),projection.messages())));
    }
    @Transactional
    public List<InputMessage> history(TaskEntity task){
        if(task.getContextSnapshot()==null){
            // Legacy retries use the first attempt boundary, never their own failed transcript.
            var first=messages.findFirstByTaskIdAndRoleOrderByAttemptAsc(task.getId(),"user");
            if(first.isEmpty())return List.of();
            capture(task,first.get().getSequence());
            tasks.save(task);
        }
        return read(task.getContextSnapshot(),Snapshot.class).messages();
    }
    @Transactional
    public ContextPack savePack(UUID project,UUID workspace,UUID conversation,long version,String name,List<UUID> selected){
        scopes.requireConversation(project,workspace,conversation);
        var entity=conversations.lockById(conversation).orElseThrow();
        if(entity.getContextVersion()!=version)throw conflict("context changed; preview again");
        if(name==null||name.isBlank()||name.trim().length()>120||selected==null||selected.isEmpty()||new HashSet<>(selected).size()!=selected.size())throw new IllegalArgumentException("pack name and selected message IDs are required");
        var projection=project(entity,Long.MAX_VALUE,true);
        var input=projection.messages().stream().filter(item->selected.contains(item.id())).toList();
        if(input.size()!=selected.size())throw new IllegalArgumentException("pack contains excluded, stale or unknown messages");
        Set<String> selectedTurns=new HashSet<>();
        for(var item:input){var original=messages.findById(item.id()).orElseThrow();selectedTurns.add(original.getTaskId()+":"+original.getAttempt());}
        for(var item:projection.messages()){
            var original=messages.findById(item.id()).orElseThrow();
            if(selectedTurns.contains(original.getTaskId()+":"+original.getAttempt())&&!selected.contains(item.id()))throw new IllegalArgumentException("select complete turns when creating a context pack");
        }
        Map<UUID,String> versions=new LinkedHashMap<>();versions.put(conversation,projectionIdentity(entity));
        for(UUID id:view(entity).packIds())versions.putAll(read(requirePack(entity,id).getManifest(),PackManifest.class).sourceProjections());
        var pack=new ContextPack(project,workspace,conversation,name.trim(),write(new PackManifest(input,versions)));
        return packs.save(pack);
    }
    public List<PackInfo> listPacks(UUID project,UUID workspace){
        scopes.requireWorkspace(project,workspace);
        return packs.findByProjectIdAndWorkspaceIdOrderByCreatedAtDesc(project,workspace).stream().map(pack->{var manifest=read(pack.getManifest(),PackManifest.class);return new PackInfo(pack.getId(),pack.getName(),pack.getSourceConversationId(),manifest.messages().size(),stale(manifest));}).toList();
    }
    @Transactional
    public ConversationEntity fork(UUID project,UUID workspace,UUID source,long version,String title,List<UUID> selected){
        var pack=savePack(project,workspace,source,version,title,selected);
        var target=scopes.createConversation(project,workspace,title);
        target.updateContext(write(new View("AUTO",List.of(),List.of(pack.getId()))));
        return target;
    }
    public static String hash(String text){try{return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(text.getBytes(StandardCharsets.UTF_8)));}catch(Exception e){throw new IllegalStateException(e);}}
}
