package dev.proofcode.control.session;

import dev.proofcode.control.project.ProjectRepository;
import dev.proofcode.control.project.ProjectEntity;
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
    private final dev.proofcode.control.task.TaskRepository tasks;
    @jakarta.persistence.PersistenceContext private jakarta.persistence.EntityManager entityManager;
    public WorkspaceService(ProjectRepository projects,WorkspaceRepository workspaces,ConversationRepository conversations,dev.proofcode.control.task.TaskRepository tasks){this.projects=projects;this.workspaces=workspaces;this.conversations=conversations;this.tasks=tasks;}
    public void requireProject(UUID projectId){dev.proofcode.control.account.AccountAccess.require(projects,projectId);if(projectId==null||!projects.existsById(projectId))throw notFound();}
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
    public ConversationEntity requireConversation(UUID projectId,UUID conversationId){
        requireProject(projectId);
        ConversationEntity value=conversations.findById(conversationId).orElseThrow(WorkspaceService::notFound);
        if(!projectId.equals(value.getProjectId()))throw notFound();
        requireWorkspace(projectId,value.getWorkspaceId());return value;
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
    @Transactional
    public ConversationEntity updateConversation(UUID projectId,UUID workspaceId,UUID conversationId,String title,Boolean archived){
        requireWorkspace(projectId,workspaceId);
        projects.lockById(projectId).orElseThrow(WorkspaceService::notFound);
        var locked=conversations.lockById(conversationId).orElseThrow(WorkspaceService::notFound);
        entityManager.refresh(locked);
        if(!projectId.equals(locked.getProjectId())||!workspaceId.equals(locked.getWorkspaceId()))throw notFound();
        if(title!=null)locked.rename(clean(title,200,"conversation title"));
        if(archived!=null&&archived!=locked.isArchived()){
            if(locked.isDefaultConversation()&&archived)throw new ResponseStatusException(HttpStatus.CONFLICT,"默认对话不能归档");
            if(archived&&tasks.existsByConversationIdAndStatusIn(conversationId,List.of(dev.proofcode.control.task.TaskStatus.CREATED,dev.proofcode.control.task.TaskStatus.QUEUED,dev.proofcode.control.task.TaskStatus.RUNNING,dev.proofcode.control.task.TaskStatus.WAITING_APPROVAL,dev.proofcode.control.task.TaskStatus.VERIFYING)))throw new ResponseStatusException(HttpStatus.CONFLICT,"请先等待此对话中的活动任务结束");
            locked.setArchived(archived,Instant.now());
        }
        return locked;
    }
    public List<WorkspaceEntity> listWorkspaces(UUID projectId){requireProject(projectId);return workspaces.findByProjectIdOrderByCreatedAtAsc(projectId);}
    public List<ConversationEntity> listConversations(UUID projectId,UUID workspaceId){requireWorkspace(projectId,workspaceId);return conversations.findByProjectIdAndWorkspaceIdOrderByCreatedAtAsc(projectId,workspaceId);}
    /** A project row lock serializes default-scope creation and prevents duplicate legacy conversations. */
    @Transactional
    public Scope resolve(UUID projectId,UUID workspaceId,UUID conversationId){
        requireProject(projectId);
        projects.lockById(projectId).orElseThrow(WorkspaceService::notFound);
        if(conversationId!=null){
            ConversationEntity conversation=conversations.findById(conversationId).orElseThrow(WorkspaceService::notFound);
            if(!projectId.equals(conversation.getProjectId())||(workspaceId!=null&&!workspaceId.equals(conversation.getWorkspaceId())))throw notFound();
            if(conversation.isArchived())throw new ResponseStatusException(HttpStatus.CONFLICT,"此对话已归档，请先恢复后再创建任务");
            requireWorkspace(projectId,conversation.getWorkspaceId());
            return new Scope(projectId,conversation.getWorkspaceId(),conversationId);
        }
        WorkspaceEntity workspace=workspaceId==null?workspaces.findByProjectIdAndDefaultWorkspaceTrue(projectId)
            .orElseGet(()->workspaces.saveAndFlush(new WorkspaceEntity(UUID.randomUUID(),projectId,"默认工作区",defaultKind(projectId),true,Instant.now())))
            :requireWorkspace(projectId,workspaceId);
        ConversationEntity conversation=conversations.findByProjectIdAndWorkspaceIdAndDefaultConversationTrue(projectId,workspace.getId())
            .orElseGet(()->conversations.saveAndFlush(new ConversationEntity(UUID.randomUUID(),projectId,workspace.getId(),"Legacy",true,Instant.now())));
        return new Scope(projectId,workspace.getId(),conversation.getId());
    }
    private WorkspaceEntity.Kind defaultKind(UUID projectId){
        ProjectEntity project=projects.findById(projectId).orElseThrow(WorkspaceService::notFound);
        return switch(project.getSourceKind()) { case "SCRATCH" -> WorkspaceEntity.Kind.SCRATCH; case "LOCAL_FOLDER" -> WorkspaceEntity.Kind.LOCAL_FOLDER; default -> WorkspaceEntity.Kind.REMOTE_REPOSITORY; };
    }
    private static String clean(String value,int length,String label){if(value==null||value.isBlank()||value.trim().length()>length)throw new IllegalArgumentException(label+" is invalid");return value.trim();}
    private static ResponseStatusException notFound(){return new ResponseStatusException(HttpStatus.NOT_FOUND,"project workspace or conversation not found");}
    public record Scope(UUID projectId,UUID workspaceId,UUID conversationId){}
}
