package dev.proofcode.control.session;

import jakarta.validation.Valid;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Size;
import java.util.List;
import java.util.UUID;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/projects/{projectId}/workspaces")
public class WorkspaceController {
    private final WorkspaceService service;
    public WorkspaceController(WorkspaceService service){this.service=service;}
    @PostMapping public ResponseEntity<WorkspaceEntity> create(@PathVariable UUID projectId,@Valid @RequestBody CreateWorkspace request){return ResponseEntity.status(201).body(service.createWorkspace(projectId,request.name(),request.kind()));}
    @GetMapping public List<WorkspaceEntity> list(@PathVariable UUID projectId){return service.listWorkspaces(projectId);}
    @GetMapping("/{workspaceId}") public WorkspaceEntity get(@PathVariable UUID projectId,@PathVariable UUID workspaceId){return service.requireWorkspace(projectId,workspaceId);}
    @PostMapping("/{workspaceId}/conversations") public ResponseEntity<ConversationEntity> createConversation(@PathVariable UUID projectId,@PathVariable UUID workspaceId,@Valid @RequestBody CreateConversation request){return ResponseEntity.status(201).body(service.createConversation(projectId,workspaceId,request.title()));}
    @GetMapping("/{workspaceId}/conversations") public List<ConversationEntity> conversations(@PathVariable UUID projectId,@PathVariable UUID workspaceId){return service.listConversations(projectId,workspaceId);}
    @GetMapping("/{workspaceId}/conversations/{conversationId}") public ConversationEntity conversation(@PathVariable UUID projectId,@PathVariable UUID workspaceId,@PathVariable UUID conversationId){return service.requireConversation(projectId,workspaceId,conversationId);}
    public record CreateWorkspace(@NotBlank @Size(max=120) String name,@NotNull WorkspaceEntity.Kind kind){}
    public record CreateConversation(@NotBlank @Size(max=200) String title){}
}
