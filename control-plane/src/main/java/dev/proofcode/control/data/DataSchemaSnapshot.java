package dev.proofcode.control.data;
import jakarta.persistence.*;
import java.util.UUID;
import java.time.Instant;
@Entity @Table(name="data_schema_snapshots")
public class DataSchemaSnapshot {
    @Id UUID id;
    @Column(name="connection_id",nullable=false) UUID connectionId;
    @Column(name="project_id",nullable=false) UUID projectId;
    @Column(name="schema_version",nullable=false,length=64) String schemaVersion;
    @Column(nullable=false,columnDefinition="text") String metadata;
    @Column(name="created_at",nullable=false) Instant createdAt;
    protected DataSchemaSnapshot(){}
    DataSchemaSnapshot(DataResource resource,String version,String metadata){id=UUID.randomUUID();connectionId=resource.id;projectId=resource.projectId;schemaVersion=version;this.metadata=metadata;createdAt=Instant.now();}
}
