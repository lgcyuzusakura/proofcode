package dev.proofcode.control.task;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name="task_events",uniqueConstraints=@UniqueConstraint(name="uq_task_event_sequence",columnNames={"task_id","sequence"}))
public class TaskEventEntity {
    @Id @GeneratedValue(strategy=GenerationType.IDENTITY) private Long id;
    @Column(name="task_id",nullable=false) private UUID taskId;
    @Column(nullable=false) private long sequence;
    @Column(nullable=false) private String type;
    @Column(nullable=false,columnDefinition="jsonb") private String payload;
    @Column(name="created_at",nullable=false) private Instant createdAt;
    protected TaskEventEntity(){}
    public TaskEventEntity(UUID taskId,long sequence,String type,String payload,Instant createdAt){this.taskId=taskId;this.sequence=sequence;this.type=type;this.payload=payload;this.createdAt=createdAt;}
    public Long getId(){return id;} public UUID getTaskId(){return taskId;} public long getSequence(){return sequence;} public String getType(){return type;} public String getPayload(){return payload;} public Instant getCreatedAt(){return createdAt;}
}

