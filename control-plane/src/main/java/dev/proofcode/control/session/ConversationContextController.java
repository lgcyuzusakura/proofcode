package dev.proofcode.control.session;
import java.util.*;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/projects/{projectId}/workspaces/{workspaceId}")
public class ConversationContextController {
    private final ConversationContextService context;
    public ConversationContextController(ConversationContextService context){this.context=context;}
    @GetMapping("/conversations/{conversationId}/context") public ConversationContextService.Preview preview(@PathVariable UUID projectId,@PathVariable UUID workspaceId,@PathVariable UUID conversationId){return context.preview(projectId,workspaceId,conversationId);}
    @PatchMapping("/conversations/{conversationId}/context") public ConversationContextService.Preview update(@PathVariable UUID projectId,@PathVariable UUID workspaceId,@PathVariable UUID conversationId,@RequestBody Update request){return context.update(projectId,workspaceId,conversationId,request.version(),request.view());}
    @PostMapping("/conversations/{conversationId}/context/packs") public ContextPack pack(@PathVariable UUID projectId,@PathVariable UUID workspaceId,@PathVariable UUID conversationId,@RequestBody Transfer request){return context.savePack(projectId,workspaceId,conversationId,request.version(),request.title(),request.messageIds());}
    @PostMapping("/conversations/{conversationId}/context/fork") public ConversationEntity fork(@PathVariable UUID projectId,@PathVariable UUID workspaceId,@PathVariable UUID conversationId,@RequestBody Transfer request){return context.fork(projectId,workspaceId,conversationId,request.version(),request.title(),request.messageIds());}
    @GetMapping("/context-packs") public List<ConversationContextService.PackInfo> packs(@PathVariable UUID projectId,@PathVariable UUID workspaceId){return context.listPacks(projectId,workspaceId);}
    public record Update(long version,ConversationContextService.View view){}
    public record Transfer(long version,String title,List<UUID> messageIds){}
}
