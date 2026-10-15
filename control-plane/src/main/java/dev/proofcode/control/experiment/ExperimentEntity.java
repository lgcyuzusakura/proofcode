package dev.proofcode.control.experiment;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name="experiments",uniqueConstraints=@UniqueConstraint(name="uq_experiment_project_idempotency",columnNames={"project_id","idempotency_key"}))
public class ExperimentEntity {
    @Id private UUID id;
    @Column(name="project_id",nullable=false) private UUID projectId;
    @Column(name="workspace_id",nullable=false) private UUID workspaceId;
    @Column(name="conversation_id",nullable=false) private UUID conversationId;
    @Column(nullable=false,length=160) private String name;
    @Column(nullable=false,columnDefinition="text") private String prompt;
    @Column(nullable=false,length=200) private String model;
    @Column(name="repository_url",nullable=false,columnDefinition="text") private String repositoryUrl;
    @Column(name="source_revision",nullable=false,length=64) private String sourceRevision;
    @Column(name="max_steps",nullable=false) private int maxSteps;
    @Column(name="test_command",nullable=false,columnDefinition="text") private String testCommand;
    @Column(nullable=false) private double temperature;
    @Column(nullable=false) private int repetitions;
    @Column(name="profile_version",nullable=false,length=80) private String profileVersion;
    @Column(name="idempotency_key",length=200) private String idempotencyKey;
    @Column(name="request_hash",nullable=false,length=64) private String requestHash;
    @Column(name="created_at",nullable=false) private Instant createdAt;
    protected ExperimentEntity(){}
    public ExperimentEntity(UUID id,UUID projectId,UUID workspaceId,UUID conversationId,String name,String prompt,String model,String repositoryUrl,String sourceRevision,int maxSteps,String testCommand,double temperature,int repetitions,String idempotencyKey,String requestHash,Instant now){
        this.id=id;this.projectId=projectId;this.workspaceId=workspaceId;this.conversationId=conversationId;this.name=name;this.prompt=prompt;this.model=model;this.repositoryUrl=repositoryUrl;this.sourceRevision=sourceRevision;this.maxSteps=maxSteps;this.testCommand=testCommand;this.temperature=temperature;this.repetitions=repetitions;this.idempotencyKey=idempotencyKey;this.requestHash=requestHash;this.createdAt=now;this.profileVersion=ExperimentProfile.VERSION;
    }
    public UUID getId(){return id;}public UUID getProjectId(){return projectId;}public UUID getWorkspaceId(){return workspaceId;}public UUID getConversationId(){return conversationId;}
    public String getName(){return name;}public String getPrompt(){return prompt;}public String getModel(){return model;}public String getRepositoryUrl(){return repositoryUrl;}public String getSourceRevision(){return sourceRevision;}
    public int getMaxSteps(){return maxSteps;}public String getTestCommand(){return testCommand;}public double getTemperature(){return temperature;}public int getRepetitions(){return repetitions;}
    public String getProfileVersion(){return profileVersion;}public String getIdempotencyKey(){return idempotencyKey;}public String getRequestHash(){return requestHash;}public Instant getCreatedAt(){return createdAt;}
}
