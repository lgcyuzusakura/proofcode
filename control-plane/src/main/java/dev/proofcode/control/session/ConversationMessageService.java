package dev.proofcode.control.session;
import dev.proofcode.control.task.TaskEntity;
import java.util.*;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service public class ConversationMessageService {
    private final ConversationMessageRepository messages;private final ConversationRepository conversations;private final WorkspaceService scopes;
    private final ConversationContextService context;
    public ConversationMessageService(ConversationMessageRepository messages,ConversationRepository conversations,WorkspaceService scopes,ConversationContextService context){this.messages=messages;this.conversations=conversations;this.scopes=scopes;this.context=context;}
    @Transactional public void start(TaskEntity task){
        if(task.getExperimentId()!=null||task.getConversationId()==null)return;
        conversations.lockById(task.getConversationId()).orElseThrow();
        if(messages.findByTaskIdAndAttemptAndRole(task.getId(),task.getAttempt(),"user").isPresent())return;
        long sequence=messages.maxSequence(task.getConversationId());
        context.capture(task,messages.findFirstByTaskIdAndRoleOrderByAttemptAsc(task.getId(),"user").map(ConversationMessage::getSequence).orElse(sequence+1));
        save(task,sequence+1,"user",task.getPrompt(),"COMPLETED");save(task,sequence+2,"assistant","","PENDING");
    }
    private void save(TaskEntity task,long sequence,String role,String content,String status){messages.save(new ConversationMessage(UUID.randomUUID(),task.getProjectId(),task.getWorkspaceId(),task.getConversationId(),sequence,role,content,task.getId(),task.getAttempt(),status));}
    @Transactional public void finish(TaskEntity task,String content,String status){
        messages.findByTaskIdAndAttemptAndRole(task.getId(),task.getAttempt(),"assistant").ifPresent(message->message.finish(content==null?"":content,status));
    }
    public List<ConversationMessage> list(UUID projectId,UUID workspaceId,UUID conversationId,long after){scopes.requireConversation(projectId,workspaceId,conversationId);return messages.findTop500ByConversationIdAndSequenceGreaterThanOrderBySequenceAsc(conversationId,Math.max(0,after));}
    public List<ConversationContextService.InputMessage> history(TaskEntity task){return context.history(task);}
}
