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
    @Column(name="context_version",nullable=false) private long contextVersion;
    @Column(name="context_view",columnDefinition="text") private String contextView;
    @Column(name="archived_at") private Instant archivedAt;
    @com.fasterxml.jackson.annotation.JsonIgnore public String getContextView(){return contextView;}
    public long getContextVersion(){return contextVersion;}
    public void updateContext(String value){contextView=value;contextVersion=Math.addExact(contextVersion,1);}
    public Instant getArchivedAt(){return archivedAt;} public boolean isArchived(){return archivedAt!=null;}
    public void rename(String value){title=value;}
    public void setArchived(boolean value,Instant now){archivedAt=value?now:null;}
    protected ConversationEntity() {}
    public ConversationEntity(UUID id,UUID projectId,UUID workspaceId,String title,boolean defaultConversation,Instant now){
        this.id=id;this.projectId=projectId;this.workspaceId=workspaceId;this.title=title;this.defaultConversation=defaultConversation;this.createdAt=now;
    }
    public UUID getId(){return id;} public UUID getProjectId(){return projectId;} public UUID getWorkspaceId(){return workspaceId;}
    public String getTitle(){return title;} public boolean isDefaultConversation(){return defaultConversation;} public Instant getCreatedAt(){return createdAt;}
}
