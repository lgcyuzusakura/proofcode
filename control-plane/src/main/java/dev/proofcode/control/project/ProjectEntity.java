package dev.proofcode.control.project;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name = "projects")
public class ProjectEntity {
    @Id private UUID id;
    @Column(nullable = false, length = 120) private String name;
    @Column(name = "repository_url") private String repositoryUrl;
    @Column(name = "source_kind", nullable = false, length = 24) private String sourceKind;
    @Column(name = "local_handle", length = 200) private String localHandle;
    @Column(name = "bootstrap_id", length = 120) private String bootstrapId;
    @Column(name = "default_branch", nullable = false) private String defaultBranch;
    @Column(name = "created_at", nullable = false) private Instant createdAt;
    protected ProjectEntity() {}
    public ProjectEntity(UUID id, String name, String repositoryUrl, String defaultBranch, Instant createdAt) {
        this(id,name,repositoryUrl,defaultBranch,"REMOTE_REPOSITORY",null,null,createdAt);
    }
    public ProjectEntity(UUID id, String name, String repositoryUrl, String defaultBranch, String sourceKind, String localHandle, String bootstrapId, Instant createdAt) {
        this.id=id; this.name=name; this.repositoryUrl=repositoryUrl; this.defaultBranch=defaultBranch; this.sourceKind=sourceKind; this.localHandle=localHandle; this.bootstrapId=bootstrapId; this.createdAt=createdAt;
    }
    public UUID getId(){return id;} public String getName(){return name;} public String getRepositoryUrl(){return repositoryUrl;}
    public String getDefaultBranch(){return defaultBranch;} public String getSourceKind(){return sourceKind;} public String getLocalHandle(){return localHandle;} public String getBootstrapId(){return bootstrapId;} public Instant getCreatedAt(){return createdAt;}
}

