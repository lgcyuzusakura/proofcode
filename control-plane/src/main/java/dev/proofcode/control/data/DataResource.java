package dev.proofcode.control.data;

import jakarta.persistence.*;
import com.fasterxml.jackson.annotation.JsonIgnore;
import java.time.Instant;
import java.util.UUID;

@Entity @Table(name="project_data_sources")
public class DataResource {
    @Id UUID id;
    @Column(name="project_id",nullable=false) UUID projectId;
    @Column(nullable=false,length=120) String name;
    @Column(nullable=false,length=24) String provider;
    @Column(nullable=false,length=24) String environment;
    @JsonIgnore @Column(name="secret_ref",nullable=false,length=160) String secretRef;
    @Column(name="allowed_schemas",nullable=false,columnDefinition="text") String allowedSchemas;
    @Column(nullable=false) boolean active=true;
    @Column(name="resource_version",nullable=false) long resourceVersion;
    @Version @Column(name="version",nullable=false) long lockVersion;
    @Column(name="policy_version",nullable=false,length=40) String policyVersion="data-policy-v2";
    @Column(name="created_at",nullable=false) Instant createdAt;
    protected DataResource() {}
    DataResource(UUID project,String provider,String environment,String secret,String schemas) {
        this.id=UUID.randomUUID();this.projectId=project;this.provider=provider;this.environment=environment;this.name=provider+"-"+environment;
        this.secretRef=secret;this.allowedSchemas=schemas;this.createdAt=Instant.now();
    }
    public UUID getId(){return id;} public UUID getProjectId(){return projectId;}
    public String getProvider(){return provider;} public String getEnvironment(){return environment;}
    public String getAllowedSchemas(){return allowedSchemas;} public boolean isActive(){return active;}
    public String getName(){return name;} public long getResourceVersion(){return resourceVersion;}
    public String getPolicyVersion(){return policyVersion;} public Instant getCreatedAt(){return createdAt;}
}
