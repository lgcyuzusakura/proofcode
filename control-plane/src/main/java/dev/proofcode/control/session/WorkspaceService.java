package dev.proofcode.control.session;

import dev.proofcode.control.project.ProjectRepository;
import java.time.Instant;
import java.util.List;
import java.util.UUID;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;

@Service
public class WorkspaceService {
    private final ProjectRepository projects;
    private final WorkspaceRepository workspaces;
    private final ConversationRepository conversations;
    public WorkspaceService(ProjectRepository projects,WorkspaceRepository workspaces,ConversationRepository conversations){this.projects=projects;this.workspaces=workspaces;this.conversations=conversations;}
    public void requireProject(UUID projectId){if(projectId==null||!projects.existsById(projectId))throw notFound();}
    public WorkspaceEntity requireWorkspace(UUID projectId,UUID workspaceId){
        requireProject(projectId);
        WorkspaceEntity value=workspaces.findById(workspaceId).orElseThrow(WorkspaceService::notFound);
        if(!projectId.equals(value.getProjectId()))throw notFound();return value;
    }
    public ConversationEntity requireConversation(UUID projectId,UUID workspaceId,UUID conversationId){
        requireWorkspace(projectId,workspaceId);
        ConversationEntity value=conversations.findById(conversationId).orElseThrow(WorkspaceService::notFound);
        if(!projectId.equals(value.getProjectId())||!workspaceId.equals(value.getWorkspaceId()))throw notFound();return value;
    }
    @Transactional
    public WorkspaceEntity createWorkspace(UUID projectId,String name,WorkspaceEntity.Kind kind){
        requireProject(projectId);
        if(kind==null)throw new IllegalArgumentException("workspace kind is required");
        return workspaces.save(new WorkspaceEntity(UUID.randomUUID(),projectId,clean(name,120,"workspace name"),kind,false,Instant.now()));
    }
    @Transactional
    public ConversationEntity createConversation(UUID projectId,UUID workspaceId,String title){
        requireWorkspace(projectId,workspaceId);
        return conversations.save(new ConversationEntity(UUID.randomUUID(),projectId,workspaceId,clean(title,200,"conversation title"),false,Instant.now()));
    }
    public List<WorkspaceEntity> listWorkspaces(UUID projectId){requireProject(projectId);return workspaces.findByProjectIdOrderByCreatedAtAsc(projectId);}
    public List<ConversationEntity> listConversations(UUID projectId,UUID workspaceId){requireWorkspace(projectId,workspaceId);return conversations.findByProjectIdAndWorkspaceIdOrderByCreatedAtAsc(projectId,workspaceId);}
    /** A project row lock serializes default-scope creation and prevents duplicate legacy conversations. */
    @Transactional
    public Scope resolve(UUID projectId,UUID workspaceId,UUID conversationId){
        projects.lockById(projectId).orElseThrow(WorkspaceService::notFound);
        if(conversationId!=null){
            ConversationEntity conversation=conversations.findById(conversationId).orElseThrow(WorkspaceService::notFound);
            if(!projectId.equals(conversation.getProjectId())||(workspaceId!=null&&!workspaceId.equals(conversation.getWorkspaceId())))throw notFound();
            requireWorkspace(projectId,conversation.getWorkspaceId());
            return new Scope(projectId,conversation.getWorkspaceId(),conversationId);
        }
        WorkspaceEntity workspace=workspaceId==null?workspaces.findByProjectIdAndDefaultWorkspaceTrue(projectId)
            .orElseGet(()->workspaces.saveAndFlush(new WorkspaceEntity(UUID.randomUUID(),projectId,"Legacy",WorkspaceEntity.Kind.REMOTE_REPOSITORY,true,Instant.now())))
            :requireWorkspace(projectId,workspaceId);
        ConversationEntity conversation=conversations.findByProjectIdAndWorkspaceIdAndDefaultConversationTrue(projectId,workspace.getId())
            .orElseGet(()->conversations.saveAndFlush(new ConversationEntity(UUID.randomUUID(),projectId,workspace.getId(),"Legacy",true,Instant.now())));
        return new Scope(projectId,workspace.getId(),conversation.getId());
    }
    private static String clean(String value,int length,String label){if(value==null||value.isBlank()||value.trim().length()>length)throw new IllegalArgumentException(label+" is invalid");return value.trim();}
    private static ResponseStatusException notFound(){return new ResponseStatusException(HttpStatus.NOT_FOUND,"project workspace or conversation not found");}
    public record Scope(UUID projectId,UUID workspaceId,UUID conversationId){}
}
