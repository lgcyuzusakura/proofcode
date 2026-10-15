package dev.proofcode.control.source;

import jakarta.persistence.*;
import com.fasterxml.jackson.annotation.JsonIgnore;
import java.time.Instant;
import java.util.UUID;

@Entity @Table(name="source_snapshots")
public class SourceSnapshot {
    @Id private UUID id;
    @Column(name="project_id",nullable=false) private UUID projectId;
    @Column(name="workspace_id",nullable=false) private UUID workspaceId;
    @Column(name="manifest_hash",nullable=false,length=64) private String manifestHash;
    @Column(name="file_count",nullable=false) private int fileCount;
    @Column(name="total_bytes",nullable=false) private long totalBytes;
    @JsonIgnore @Column(nullable=false,columnDefinition="text") private String content;
    @Column(name="created_at",nullable=false) private Instant createdAt;
    protected SourceSnapshot(){}
    public SourceSnapshot(UUID id,UUID projectId,UUID workspaceId,String manifestHash,int fileCount,long totalBytes,String content){this.id=id;this.projectId=projectId;this.workspaceId=workspaceId;this.manifestHash=manifestHash;this.fileCount=fileCount;this.totalBytes=totalBytes;this.content=content;this.createdAt=Instant.now();}
    public UUID getId(){return id;} public UUID getProjectId(){return projectId;} public UUID getWorkspaceId(){return workspaceId;} public String getManifestHash(){return manifestHash;} public int getFileCount(){return fileCount;} public long getTotalBytes(){return totalBytes;} public Instant getCreatedAt(){return createdAt;}
    @JsonIgnore public String getContent(){return content;}
}
