package dev.proofcode.control.task;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.outbox.OutboxEntity;
import dev.proofcode.control.outbox.OutboxRepository;
import dev.proofcode.control.project.ProjectEntity;
import dev.proofcode.control.project.ProjectRepository;
import java.time.Instant;
import java.util.Map;
import java.util.UUID;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class TaskService {
    private final TaskRepository tasks; private final ProjectRepository projects; private final OutboxRepository outbox; private final ObjectMapper json; private final String queue;
    public TaskService(TaskRepository tasks,ProjectRepository projects,OutboxRepository outbox,ObjectMapper json,@Value("${proofcode.queue}")String queue){this.tasks=tasks;this.projects=projects;this.outbox=outbox;this.json=json;this.queue=queue;}
    @Transactional
    public TaskEntity create(UUID projectId,String prompt,String model,String idempotencyKey){if(idempotencyKey!=null){var existing=tasks.findByProjectIdAndIdempotencyKey(projectId,idempotencyKey);if(existing.isPresent())return existing.get();}ProjectEntity project=projects.findById(projectId).orElseThrow();Instant now=Instant.now();TaskEntity task=new TaskEntity(UUID.randomUUID(),projectId,prompt,model,idempotencyKey,now);task.transition(TaskStatus.QUEUED);tasks.save(task);enqueue(project,task,now);return task;}
    public TaskEntity create(UUID projectId,String prompt,String model){return create(projectId,prompt,model,null);}
    @Transactional
    public TaskEntity retry(UUID taskId){TaskEntity task=tasks.lockById(taskId).orElseThrow();task.retry();ProjectEntity project=projects.findById(task.getProjectId()).orElseThrow();enqueue(project,task,Instant.now());return task;}
    private void enqueue(ProjectEntity project,TaskEntity task,Instant now){try{String payload=json.writeValueAsString(Map.of("version","v1","taskId",task.getId(),"projectId",task.getProjectId(),"repositoryUrl",project.getRepositoryUrl(),"branch",project.getDefaultBranch(),"prompt",task.getPrompt(),"model",task.getModel()));outbox.save(new OutboxEntity(UUID.randomUUID(),task.getId(),queue,payload,now));}catch(JsonProcessingException e){throw new IllegalStateException(e);}}
}
