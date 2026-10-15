package dev.proofcode.control.experiment;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name="experiment_runs",uniqueConstraints={@UniqueConstraint(name="uq_experiment_group_repetition",columnNames={"experiment_id","experiment_group","repetition"}),@UniqueConstraint(name="uq_experiment_run_task",columnNames="task_id")})
public class ExperimentRunEntity {
    @Id private UUID id;
    @Column(name="experiment_id",nullable=false) private UUID experimentId;
    @Column(name="project_id",nullable=false) private UUID projectId;
    @Column(name="task_id",nullable=false) private UUID taskId;
    @Enumerated(EnumType.STRING) @org.hibernate.annotations.JdbcTypeCode(org.hibernate.type.SqlTypes.VARCHAR) @Column(name="experiment_group",nullable=false,length=1) private ExperimentProfile.Group group;
    @Column(nullable=false) private int repetition;
    @Column(name="created_at",nullable=false) private Instant createdAt;
    protected ExperimentRunEntity(){}
    public ExperimentRunEntity(UUID id,UUID experimentId,UUID projectId,UUID taskId,ExperimentProfile.Group group,int repetition,Instant now){this.id=id;this.experimentId=experimentId;this.projectId=projectId;this.taskId=taskId;this.group=group;this.repetition=repetition;this.createdAt=now;}
    public UUID getId(){return id;}public UUID getExperimentId(){return experimentId;}public UUID getProjectId(){return projectId;}public UUID getTaskId(){return taskId;}public ExperimentProfile.Group getGroup(){return group;}public int getRepetition(){return repetition;}public Instant getCreatedAt(){return createdAt;}
}
