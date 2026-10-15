package dev.proofcode.control.session;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

@Entity @Table(name="context_packs")
public class ContextPack {
    @Id private UUID id;
    @Column(name="project_id",nullable=false) private UUID projectId;
    @Column(name="workspace_id",nullable=false) private UUID workspaceId;
    @Column(name="source_conversation_id",nullable=false) private UUID sourceConversationId;
    @Column(nullable=false,length=120) private String name;
    @Column(nullable=false,columnDefinition="text") private String manifest;
    @Column(name="created_at",nullable=false) private Instant createdAt;
    protected ContextPack(){}
    public ContextPack(UUID projectId,UUID workspaceId,UUID sourceConversationId,String name,String manifest){
        this.id=UUID.randomUUID();this.projectId=projectId;this.workspaceId=workspaceId;this.sourceConversationId=sourceConversationId;this.name=name;this.manifest=manifest;this.createdAt=Instant.now();
    }
    public UUID getId(){return id;} public UUID getProjectId(){return projectId;} public UUID getWorkspaceId(){return workspaceId;}
    public UUID getSourceConversationId(){return sourceConversationId;} public String getName(){return name;}
    @com.fasterxml.jackson.annotation.JsonIgnore public String getManifest(){return manifest;}
    public Instant getCreatedAt(){return createdAt;}
}
