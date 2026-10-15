package dev.proofcode.control.session;
import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;
@Entity @Table(name="conversation_messages")
public class ConversationMessage {
    @Id private UUID id;
    @Column(name="project_id",nullable=false) private UUID projectId;
    @Column(name="workspace_id",nullable=false) private UUID workspaceId;
    @Column(name="conversation_id",nullable=false) private UUID conversationId;
    @Column(nullable=false) private long sequence;
    @Column(nullable=false,length=12) private String role;
    @Column(nullable=false,columnDefinition="text") private String content;
    @Column(name="task_id",nullable=false) private UUID taskId;
    @Column(nullable=false) private int attempt;
    @Column(nullable=false,length=24) private String status;
    @Column(name="created_at",nullable=false) private Instant createdAt;
    protected ConversationMessage(){}
    public ConversationMessage(UUID id,UUID projectId,UUID workspaceId,UUID conversationId,long sequence,String role,String content,UUID taskId,int attempt,String status){this.id=id;this.projectId=projectId;this.workspaceId=workspaceId;this.conversationId=conversationId;this.sequence=sequence;this.role=role;this.content=content;this.taskId=taskId;this.attempt=attempt;this.status=status;this.createdAt=Instant.now();}
    public void finish(String content,String status){this.content=content;this.status=status;}
    public UUID getId(){return id;}public UUID getProjectId(){return projectId;}public UUID getWorkspaceId(){return workspaceId;}public UUID getConversationId(){return conversationId;}public long getSequence(){return sequence;}public String getRole(){return role;}public String getContent(){return content;}public UUID getTaskId(){return taskId;}public int getAttempt(){return attempt;}public String getStatus(){return status;}public Instant getCreatedAt(){return createdAt;}
}
