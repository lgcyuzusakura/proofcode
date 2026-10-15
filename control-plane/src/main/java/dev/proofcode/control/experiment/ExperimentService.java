package dev.proofcode.control.experiment;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.project.ProjectEntity;
import dev.proofcode.control.project.ProjectRepository;
import dev.proofcode.control.session.WorkspaceService;
import dev.proofcode.control.task.TaskEntity;
import dev.proofcode.control.task.TaskEventEntity;
import dev.proofcode.control.task.TaskEventRepository;
import dev.proofcode.control.task.TaskService;
import dev.proofcode.control.task.TaskRepository;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.time.Instant;
import java.util.*;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;

@Service
public class ExperimentService {
    private final ExperimentRepository experiments;
    private final ExperimentRunRepository runs;
    private final TaskService taskService;
    private final TaskRepository tasks;
    private final TaskEventRepository events;
    private final ProjectRepository projects;
    private final WorkspaceService scopes;
    private final ObjectMapper json;

    public ExperimentService(ExperimentRepository experiments, ExperimentRunRepository runs, TaskService taskService, TaskRepository tasks,
            TaskEventRepository events, ProjectRepository projects, WorkspaceService scopes, ObjectMapper json) {
        this.experiments = experiments; this.runs = runs; this.taskService = taskService; this.tasks = tasks; this.events = events;
        this.projects = projects; this.scopes = scopes; this.json = json;
    }

    @Transactional
    public CreateResult create(CreateRequest request, String idempotencyKey) {
        TaskService.validateExecution(request.baseCommit(), request.maxSteps(), request.testCommand(), request.temperature());
        if(request.projectId()==null||request.baseCommit()==null||request.prompt()==null||request.prompt().isBlank()||request.prompt().length()>131072||request.model()==null||request.model().isBlank()||request.model().length()>200||request.testCommand()==null)throw new IllegalArgumentException("project, prompt, model, baseCommit and testCommand are required");
        if (request.repetitions() < 1 || request.repetitions() > 20) throw new IllegalArgumentException("repetitions must be between 1 and 20");
        if (request.name() == null || request.name().isBlank() || request.name().trim().length() > 160) throw new IllegalArgumentException("experiment name is invalid");
        ProjectEntity project = projects.findById(request.projectId()).orElseThrow();
        var scope = scopes.resolve(request.projectId(), request.workspaceId(), request.conversationId());
        String normalizedKey = idempotencyKey == null || idempotencyKey.isBlank() ? null : idempotencyKey.trim();
        if(normalizedKey!=null&&normalizedKey.length()>200)throw new IllegalArgumentException("Idempotency-Key exceeds 200 characters");
        String hash = requestHash(request, scope.workspaceId(), scope.conversationId());
        if (normalizedKey != null) {
            var existing = experiments.findByProjectIdAndIdempotencyKey(request.projectId(), normalizedKey);
            if (existing.isPresent()) {
                if (!existing.get().getRequestHash().equals(hash)) throw new ResponseStatusException(HttpStatus.CONFLICT, "idempotency key already belongs to another experiment");
                return new CreateResult(existing.get(), runs.findByExperimentIdOrderByGroupAscRepetitionAsc(existing.get().getId()), false);
            }
        }
        Instant now = Instant.now();
        ExperimentEntity experiment = new ExperimentEntity(UUID.randomUUID(), request.projectId(), scope.workspaceId(), scope.conversationId(),
                request.name().trim(), request.prompt(), request.model(), project.getRepositoryUrl(), request.baseCommit().toLowerCase(Locale.ROOT), request.maxSteps(),
                request.testCommand(), request.temperature(), request.repetitions(), normalizedKey, hash, now);
        experiments.save(experiment);
        List<ExperimentRunEntity> createdRuns = new ArrayList<>();
        for (ExperimentProfile.Group group : ExperimentProfile.Group.values()) {
            for (int repetition = 1; repetition <= request.repetitions(); repetition++) {
                UUID runId = UUID.randomUUID();
                TaskEntity task = taskService.createExperimentTask(request.projectId(), scope.workspaceId(), scope.conversationId(), request.prompt(), request.model(), experiment.getSourceRevision(), request.maxSteps(), request.testCommand(), request.temperature(), experiment.getId(), runId, group);
                createdRuns.add(runs.save(new ExperimentRunEntity(runId, experiment.getId(), request.projectId(), task.getId(), group, repetition, now)));
            }
        }
        return new CreateResult(experiment, createdRuns, true);
    }

    public ExperimentEntity require(UUID projectId, UUID experimentId) {
        ExperimentEntity experiment = experiments.findById(experimentId).orElseThrow(() -> new ResponseStatusException(HttpStatus.NOT_FOUND, "experiment not found"));
        if (!projectId.equals(experiment.getProjectId())) throw new ResponseStatusException(HttpStatus.NOT_FOUND, "experiment not found");
        scopes.requireConversation(projectId, experiment.getWorkspaceId(), experiment.getConversationId());
        return experiment;
    }

    public List<ExperimentEntity> list(UUID projectId) { scopes.requireProject(projectId); return experiments.findByProjectIdOrderByCreatedAtDesc(projectId); }

    public Comparison compare(UUID projectId, UUID experimentId) {
        ExperimentEntity experiment = require(projectId, experimentId);
        List<ExperimentRunEntity> experimentRuns = runs.findByExperimentIdOrderByGroupAscRepetitionAsc(experimentId);
        Map<UUID, TaskEntity> taskMap = new HashMap<>();
        List<UUID> taskIds=experimentRuns.stream().map(ExperimentRunEntity::getTaskId).toList();
        for(TaskEntity task:tasks.findAllById(taskIds))taskMap.put(task.getId(),task);
        Map<UUID,TaskEventEntity> terminal=new HashMap<>();
        if(!taskIds.isEmpty())for(TaskEventEntity event:events.findByTaskIdInAndTypeInOrderBySequenceDesc(taskIds,List.of("task.completed","task.failed","task.cancelled"))){
            TaskEntity task=taskMap.get(event.getTaskId());
            if(task!=null&&task.getAttempt()==event.getAttempt())terminal.putIfAbsent(event.getTaskId(),event);
        }
        Map<ExperimentProfile.Group, List<RunResult>> grouped = new EnumMap<>(ExperimentProfile.Group.class);
        for (ExperimentProfile.Group group : ExperimentProfile.Group.values()) grouped.put(group, new ArrayList<>());
        for (ExperimentRunEntity run : experimentRuns) {
            TaskEntity task = taskMap.get(run.getTaskId());
            grouped.get(run.getGroup()).add(resultFor(experiment, run, task,terminal.get(run.getTaskId())));
        }
        List<GroupComparison> groups = new ArrayList<>();
        for (ExperimentProfile.Group group : ExperimentProfile.Group.values()) groups.add(summarize(group, grouped.get(group)));
        return new Comparison(experiment, groups, experimentRuns.size());
    }

    private RunResult resultFor(ExperimentEntity experiment, ExperimentRunEntity run, TaskEntity task,TaskEventEntity terminalEvent) {
        if (task == null) return RunResult.missing(run, null, null, false, "task missing");
        if (terminalEvent == null) return RunResult.missing(run, task.getAttempt(), task.getStatus().name(), false, "terminal event missing");
        // Task and event queries can observe different committed snapshots. Once
        // current-attempt terminal evidence exists, its type defines the outcome.
        String status = switch (terminalEvent.getType()) {
            case "task.completed" -> "SUCCEEDED";
            case "task.failed" -> "FAILED";
            case "task.cancelled" -> "CANCELLED";
            default -> task.getStatus().name();
        };
        JsonNode payload;
        try { payload = json.readTree(terminalEvent.getPayload()); } catch (JsonProcessingException error) { return RunResult.missing(run, task.getAttempt(), status, true, "invalid terminal payload"); }
        JsonNode result = payload.path("experimentResult");
        if (!result.isObject()) return RunResult.missing(run, task.getAttempt(), status, true, "experimentResult missing");
        String profileVersion = text(result, "profileVersion");
        Boolean profileApplied = result.path("profileApplied").isBoolean()?result.path("profileApplied").asBoolean():null;
        boolean revisionMatches = experiment.getSourceRevision().equals(text(result, "sourceRevision"));
        boolean groupMatches = text(result,"experimentGroup")==null||run.getGroup().name().equals(text(result, "experimentGroup"));
        boolean valid = Boolean.TRUE.equals(profileApplied) && experiment.getProfileVersion().equals(profileVersion) && revisionMatches && groupMatches;
        String reason=text(result,"failureReason");
        if(reason==null&&!valid)reason="profile or source revision mismatch";
        return new RunResult(run.getId(), run.getGroup(), run.getRepetition(), task.getId(), task.getAttempt(),status, true, valid,profileApplied,Boolean.FALSE.equals(profileApplied),reason, mapResult(result));
    }

    private GroupComparison summarize(ExperimentProfile.Group group, List<RunResult> results) {
        long terminal = results.stream().filter(RunResult::terminal).count();
        long applied = results.stream().filter(r->Boolean.TRUE.equals(r.profileApplied())).count();
        long valid = results.stream().filter(RunResult::valid).count();
        long succeeded = results.stream().filter(r -> "SUCCEEDED".equals(r.status())).count();
        long failed=results.stream().filter(r->"FAILED".equals(r.status())).count();
        long cancelled=results.stream().filter(r->"CANCELLED".equals(r.status())).count();
        long configurationFailed=results.stream().filter(RunResult::configurationFailed).count();
        long unknown=results.stream().filter(r->NUMERIC_METRICS.stream().noneMatch(r.observedResult()::containsKey)&&BOOLEAN_METRICS.stream().noneMatch(r.observedResult()::containsKey)).count();
        Map<String, MetricSummary> metrics = new LinkedHashMap<>();
        for (String key : NUMERIC_METRICS) metrics.put(key, metric(results, key));
        Map<String,BooleanSummary> booleanMetrics=new LinkedHashMap<>();
        for(String key:BOOLEAN_METRICS){
            long yes=results.stream().filter(RunResult::valid).filter(r->Boolean.TRUE.equals(r.observedResult().get(key))).count();
            long no=results.stream().filter(RunResult::valid).filter(r->Boolean.FALSE.equals(r.observedResult().get(key))).count();
            long missing=results.size()-yes-no;
            booleanMetrics.put(key,new BooleanSummary(yes,no,missing,missing==0&&!results.isEmpty()?(double)yes/results.size():null));
        }
        return new GroupComparison(group, ExperimentProfile.forGroup(group), results.size(), terminal, applied, valid, succeeded,failed,cancelled,configurationFailed,unknown,
            terminal==results.size()&&!results.isEmpty()?(double)succeeded/results.size():null,metrics,booleanMetrics,results);
    }

    private MetricSummary metric(List<RunResult> results, String key) {
        List<Double> values = results.stream().filter(RunResult::valid).map(r -> r.observedResult().get(key)).filter(Number.class::isInstance).map(v->((Number)v).doubleValue()).sorted().toList();
        if (values.isEmpty()) return new MetricSummary(null, null, 0,results.size());
        double sum = values.stream().mapToDouble(Double::doubleValue).sum();
        double p95 = values.get(Math.min(values.size() - 1, (int) Math.ceil(values.size() * .95) - 1));
        return new MetricSummary(sum / values.size(), p95, values.size(),results.size()-values.size());
    }

    static final List<String> NUMERIC_METRICS=List.of("durationMs","inputTokens","outputTokens","totalTokens","toolCalls","toolErrors","invalidToolCalls","highRiskBlocked","recallAtK","mrr","ndcg","fileHitRate","lineHitRate","tokensBeforeCompression","tokensAfterCompression","preApprovalExecutions","retrievalCalls","retrievalCacheHits","jevDecisions","jevApplied","deduplicatedMessages");
    static final List<String> BOOLEAN_METRICS=List.of("taskCompleted","testsPassed","recoverySucceeded","patchSucceeded","compressionError","sqlIrAccurate","sqlExecutedCorrectly","explainPassed","dangerousSqlBlocked","falsePositive","migrationRollbackSucceeded","auditComplete");
    static final List<String> MEASUREMENT_NOTES=List.of("compressionTokenBasis","usageReported","snapshotId");
    private Map<String, Object> mapResult(JsonNode result) {
        Map<String, Object> values = new LinkedHashMap<>();
        for (String key : NUMERIC_METRICS) {
            JsonNode value=result.path(key);
            if(value.isNumber()&&Double.isFinite(value.doubleValue())&&value.doubleValue()>=0)values.put(key,value.isIntegralNumber()?value.longValue():value.doubleValue());
        }
        for(String key:BOOLEAN_METRICS)if(result.path(key).isBoolean())values.put(key,result.path(key).booleanValue());
        for(String key:MEASUREMENT_NOTES){
            JsonNode value=result.path(key);
            if(value.isTextual()&&value.textValue().length()<=200)values.put(key,value.textValue());
            else if(value.isBoolean())values.put(key,value.booleanValue());
        }
        return values;
    }
    private static String text(JsonNode node, String field) { return node.path(field).isTextual() ? node.path(field).asText() : null; }
    private String requestHash(CreateRequest request, UUID workspaceId, UUID conversationId) {
        String raw = String.join("\u0000", request.projectId().toString(), workspaceId.toString(), conversationId.toString(), request.name().trim(), request.prompt(), request.model(), request.baseCommit().toLowerCase(Locale.ROOT), Integer.toString(request.maxSteps()), request.testCommand(), Double.toString(request.temperature()), Integer.toString(request.repetitions()));
        try { return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(raw.getBytes(StandardCharsets.UTF_8))); } catch (Exception error) { throw new IllegalStateException(error); }
    }

    public record CreateRequest(UUID projectId, UUID workspaceId, UUID conversationId, String name, String prompt, String model, String baseCommit, int maxSteps, String testCommand, double temperature, int repetitions) {}
    public record CreateResult(ExperimentEntity experiment, List<ExperimentRunEntity> runs, boolean created) {}
    public record Comparison(ExperimentEntity experiment, List<GroupComparison> groups, int runCount) {}
    public record GroupComparison(ExperimentProfile.Group group, ExperimentProfile profile, int plannedRuns, long terminalRuns, long profileAppliedRuns, long validRuns, long succeededRuns,long failedRuns,long cancelledRuns,long configurationFailedRuns,long unknownResults,Double taskStatusSuccessRate, Map<String, MetricSummary> metrics,Map<String,BooleanSummary> booleanMetrics, List<RunResult> results) {}
    public record MetricSummary(Double mean, Double p95, int sampleCount,int missingCount) {}
    public record BooleanSummary(long trueCount,long falseCount,long unknownCount,Double rate){}
    public record RunResult(UUID runId, ExperimentProfile.Group group, int repetition, UUID taskId,Integer attempt, String status, boolean terminal, boolean valid, Boolean profileApplied,boolean configurationFailed, String reason, Map<String, Object> observedResult) {
        static RunResult missing(ExperimentRunEntity run, Integer attempt, String status, boolean terminal, String reason) { return new RunResult(run.getId(), run.getGroup(), run.getRepetition(), run.getTaskId(),attempt, status, terminal, false, null,false, reason, Map.of()); }
    }
}
