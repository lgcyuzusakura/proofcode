package dev.proofcode.control.session;
import dev.proofcode.control.task.TaskRepository;
import java.util.*;
import org.springframework.web.bind.annotation.*;
import org.springframework.http.HttpStatus;
import org.springframework.web.server.ResponseStatusException;
@RestController public class ConversationMessageController {
    private final ConversationMessageService messages;private final WorkspaceService scopes;private final TaskRepository tasks;
    public ConversationMessageController(ConversationMessageService messages,WorkspaceService scopes,TaskRepository tasks){this.messages=messages;this.scopes=scopes;this.tasks=tasks;}
    @PostMapping("/api/projects/{projectId}/default-scope") public WorkspaceService.Scope defaults(@PathVariable UUID projectId){return scopes.resolve(projectId,null,null);}
    @GetMapping("/api/projects/{projectId}/workspaces/{workspaceId}/conversations/{conversationId}/messages") public List<ConversationMessage> list(@PathVariable UUID projectId,@PathVariable UUID workspaceId,@PathVariable UUID conversationId,@RequestParam(defaultValue="0") long afterSequence){return messages.list(projectId,workspaceId,conversationId,afterSequence);}
    @GetMapping("/internal/tasks/{taskId}/conversation") public org.springframework.http.ResponseEntity<List<ConversationContextService.InputMessage>> history(@PathVariable UUID taskId,@RequestParam int attempt){var task=tasks.findById(taskId).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));if(task.getAttempt()!=attempt)throw new ResponseStatusException(HttpStatus.CONFLICT,"conversation attempt mismatch");var history=messages.history(task);return org.springframework.http.ResponseEntity.ok().header("X-Context-Selection-Hash",task.getContextSelectionHash()==null?"":task.getContextSelectionHash()).body(history);}
}
