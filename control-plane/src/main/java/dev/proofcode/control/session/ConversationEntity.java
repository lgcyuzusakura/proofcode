package dev.proofcode.control.session;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name="conversations",uniqueConstraints=@UniqueConstraint(name="uq_conversation_scope_id",columnNames={"project_id","workspace_id","id"}))
public class ConversationEntity {
    @Id private UUID id;
    @Column(name="project_id",nullable=false) private UUID projectId;
    @Column(name="workspace_id",nullable=false) private UUID workspaceId;
    @Column(nullable=false,length=200) private String title;
    @Column(name="is_default",nullable=false) private boolean defaultConversation;
    @Column(name="created_at",nullable=false) private Instant createdAt;
    protected ConversationEntity() {}
    public ConversationEntity(UUID id,UUID projectId,UUID workspaceId,String title,boolean defaultConversation,Instant now){
        this.id=id;this.projectId=projectId;this.workspaceId=workspaceId;this.title=title;this.defaultConversation=defaultConversation;this.createdAt=now;
    }
    public UUID getId(){return id;} public UUID getProjectId(){return projectId;} public UUID getWorkspaceId(){return workspaceId;}
    public String getTitle(){return title;} public boolean isDefaultConversation(){return defaultConversation;} public Instant getCreatedAt(){return createdAt;}
}
