package dev.proofcode.control.data;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

/** A durable project management context independent of a programming task's lifetime. */
@Entity @Table(name="data_sessions")
public class DataSession {
    @Id UUID id;
    @Column(name="project_id",nullable=false) UUID projectId;
    @Column(nullable=false,length=120) String name;
    @Column(nullable=false,length=24) String purpose;
    @Column(nullable=false,length=24) String status="ACTIVE";
    @Column(name="created_at",nullable=false) Instant createdAt;
    @Column(name="expires_at",nullable=false) Instant expiresAt;
    @Version long version;
    protected DataSession(){}
    DataSession(UUID project,String name,String purpose,int minutes){id=UUID.randomUUID();projectId=project;this.name=name;this.purpose=purpose;createdAt=Instant.now();expiresAt=createdAt.plusSeconds(minutes*60L);}
    public UUID getId(){return id;} public UUID getProjectId(){return projectId;}
    public String getName(){return name;} public String getPurpose(){return purpose;} public String getStatus(){return status;}
    public Instant getCreatedAt(){return createdAt;} public Instant getExpiresAt(){return expiresAt;}
}
