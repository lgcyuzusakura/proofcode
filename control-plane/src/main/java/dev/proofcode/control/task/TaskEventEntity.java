package dev.proofcode.control.task;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;
import com.fasterxml.jackson.annotation.JsonRawValue;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonIgnore;
import org.hibernate.annotations.JdbcTypeCode;
import org.hibernate.type.SqlTypes;

@Entity
@Table(name="task_events",uniqueConstraints=@UniqueConstraint(name="uq_task_event_sequence",columnNames={"task_id","sequence"}))
public class TaskEventEntity {
    @Id @GeneratedValue(strategy=GenerationType.IDENTITY) private Long id;
    @Column(name="task_id",nullable=false) private UUID taskId;
    @Column(name="runner_id") private UUID runnerId;
    @Column(name="run_id",length=200) private String runId;
    @Column(nullable=false) private int attempt;
    @Column(name="event_key") private String eventKey;
    @Column(nullable=false) private long sequence;
    @Column(nullable=false) private String type;
    @JdbcTypeCode(SqlTypes.JSON) @Column(nullable=false,columnDefinition="jsonb") private String payload;
    @Column(name="created_at",nullable=false) private Instant createdAt;
    protected TaskEventEntity(){}
    public TaskEventEntity(UUID taskId,long sequence,String type,String payload,Instant createdAt){this(taskId,1,sequence,null,type,payload,createdAt);}
    public TaskEventEntity(UUID taskId,long sequence,String eventKey,String type,String payload,Instant createdAt){this(taskId,1,sequence,eventKey,type,payload,createdAt);}
    public TaskEventEntity(UUID taskId,int attempt,long sequence,String eventKey,String type,String payload,Instant createdAt){this(taskId,null,null,attempt,sequence,eventKey,type,payload,createdAt);}
    public TaskEventEntity(UUID taskId,UUID runnerId,String runId,int attempt,long sequence,String eventKey,String type,String payload,Instant createdAt){this.taskId=taskId;this.runnerId=runnerId;this.runId=runId;this.attempt=attempt;this.sequence=sequence;this.eventKey=eventKey;this.type=type;this.payload=payload;this.createdAt=createdAt;}
    public void setSequence(long sequence){this.sequence=sequence;}
    @JsonProperty("version") public String getVersion(){return "v1";}
    @JsonIgnore public Long getId(){return id;} public UUID getTaskId(){return taskId;} public UUID getRunnerId(){return runnerId;} public String getRunId(){return runId;} public int getAttempt(){return attempt;} public long getSequence(){return sequence;} @JsonIgnore public String getEventKey(){return eventKey;} public String getType(){return type;} @JsonRawValue public String getPayload(){return payload;} @JsonProperty("timestamp") public Instant getTimestamp(){return createdAt;} @JsonIgnore public Instant getCreatedAt(){return createdAt;}
}
