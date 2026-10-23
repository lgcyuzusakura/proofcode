package dev.proofcode.control.task;
import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;
@Entity @Table(name="task_instructions",uniqueConstraints={@UniqueConstraint(columnNames={"task_id","attempt","sequence"}),@UniqueConstraint(columnNames={"task_id","attempt","client_key"})})
public class TaskInstruction {
 @Id public UUID id;
 @Column(name="task_id",nullable=false) public UUID taskId;
 @Column(nullable=false) public int attempt;
 @Column(nullable=false) public long sequence;
 @Column(nullable=false,columnDefinition="text") public String content;
 @Column(name="client_key",nullable=false,length=80) public String clientKey;
 @Column(name="created_at",nullable=false) public Instant createdAt;
 @Column(name="delivered_at") public Instant deliveredAt;
 protected TaskInstruction(){}
 public TaskInstruction(UUID task,int attempt,long sequence,String content,String key){id=UUID.randomUUID();taskId=task;this.attempt=attempt;this.sequence=sequence;this.content=content;clientKey=key;createdAt=Instant.now();}
}
