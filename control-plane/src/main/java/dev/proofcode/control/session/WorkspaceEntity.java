package dev.proofcode.control.session;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name="project_workspaces", uniqueConstraints=@UniqueConstraint(name="uq_workspace_project_id",columnNames={"project_id","id"}))
public class WorkspaceEntity {
    public enum Kind { REMOTE_REPOSITORY, LOCAL_FOLDER, SCRATCH }
    @Id private UUID id;
    @Column(name="project_id",nullable=false) private UUID projectId;
    @Column(nullable=false,length=120) private String name;
    @Enumerated(EnumType.STRING) @Column(nullable=false,length=30) private Kind kind;
    @Column(name="is_default",nullable=false) private boolean defaultWorkspace;
    @Column(name="created_at",nullable=false) private Instant createdAt;
    protected WorkspaceEntity() {}
    public WorkspaceEntity(UUID id,UUID projectId,String name,Kind kind,boolean defaultWorkspace,Instant now) {
        this.id=id;this.projectId=projectId;this.name=name;this.kind=kind;this.defaultWorkspace=defaultWorkspace;this.createdAt=now;
    }
    public UUID getId(){return id;} public UUID getProjectId(){return projectId;} public String getName(){return name;}
    public Kind getKind(){return kind;} public boolean isDefaultWorkspace(){return defaultWorkspace;} public Instant getCreatedAt(){return createdAt;}
}
