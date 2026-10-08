package dev.proofcode.control.experiment;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.project.ProjectRepository;
import dev.proofcode.control.session.WorkspaceService;
import dev.proofcode.control.task.TaskEntity;
import dev.proofcode.control.task.TaskEventEntity;
import dev.proofcode.control.task.TaskEventRepository;
import dev.proofcode.control.task.TaskRepository;
import dev.proofcode.control.task.TaskService;
import dev.proofcode.control.task.TaskStatus;
import java.time.Instant;
import java.util.ArrayList;
import java.util.EnumMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.UUID;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyList;
import static org.mockito.Mockito.*;

class ExperimentComparisonSnapshotTest {
    @Test
    void terminalEvidenceOverridesStaleTaskSnapshotsWithoutPromotingMissingOrOlderAttempts() {
        var experiments = mock(ExperimentRepository.class);
        var runs = mock(ExperimentRunRepository.class);
        var tasks = mock(TaskRepository.class);
        var events = mock(TaskEventRepository.class);
        var json = new ObjectMapper();
        var service = new ExperimentService(experiments, runs, mock(TaskService.class), tasks,
                events, mock(ProjectRepository.class), mock(WorkspaceService.class), json);
        UUID experimentId = UUID.randomUUID();
        UUID projectId = UUID.randomUUID();
        String commit = "0123456789abcdef0123456789abcdef01234567";
        Instant now = Instant.now();
        var experiment = new ExperimentEntity(experimentId, projectId, UUID.randomUUID(), UUID.randomUUID(),
                "Concurrent comparison", "Fix arithmetic", "model", "https://example.com/repo.git",
                commit, 15, "go test ./...", .25, 1, null, "hash", now);
        List<ExperimentRunEntity> planned = new ArrayList<>();
        List<TaskEntity> earlierTaskSnapshot = new ArrayList<>();
        List<TaskEventEntity> laterEventSnapshot = new ArrayList<>();
        for (var group : ExperimentProfile.Group.values()) {
            UUID runId = UUID.randomUUID();
            var task = new TaskEntity(UUID.randomUUID(), projectId, "Fix arithmetic", "model", now);
            task.bindExperiment(experimentId, runId, group);
            task.transition(TaskStatus.QUEUED);
            if (group != ExperimentProfile.Group.E) task.transition(TaskStatus.RUNNING);
            if (group == ExperimentProfile.Group.F) {
                task.fail("first attempt failed");
                task.retry();
                task.transition(TaskStatus.RUNNING);
            }
            planned.add(new ExperimentRunEntity(runId, experimentId, projectId, task.getId(), group, 1, now));
            earlierTaskSnapshot.add(task);
            String type = switch (group) {
                case A, D, F -> "task.completed";
                case B -> "task.failed";
                case C -> "task.cancelled";
                case E -> null;
            };
            if (type == null) continue;
            var payload = json.createObjectNode();
            if (group != ExperimentProfile.Group.D) {
                payload.putObject("experimentResult")
                        .put("profileVersion", ExperimentProfile.VERSION)
                        .put("profileApplied", true)
                        .put("sourceRevision", commit)
                        .put("experimentGroup", group.name())
                        .put("taskCompleted", type.equals("task.completed"))
                        .put("testsPassed", type.equals("task.completed"));
            }
            // F's event is from its first attempt. It must not complete attempt 2.
            laterEventSnapshot.add(new TaskEventEntity(task.getId(), 1, 1, null, type, payload.toString(), now));
        }
        when(experiments.findById(experimentId)).thenReturn(Optional.of(experiment));
        when(runs.findByExperimentIdOrderByGroupAscRepetitionAsc(experimentId)).thenReturn(planned);
        when(tasks.findAllById(any())).thenReturn(earlierTaskSnapshot);
        when(events.findByTaskIdInAndTypeInOrderBySequenceDesc(anyList(), anyList())).thenReturn(laterEventSnapshot);

        var comparison = service.compare(projectId, experimentId);
        Map<ExperimentProfile.Group, ExperimentService.GroupComparison> groups = new EnumMap<>(ExperimentProfile.Group.class);
        comparison.groups().forEach(group -> groups.put(group.group(), group));
        assertEquals(6, comparison.runCount());
        assertEquals(1, groups.get(ExperimentProfile.Group.A).succeededRuns());
        assertEquals(1.0, groups.get(ExperimentProfile.Group.A).taskStatusSuccessRate());
        assertEquals("SUCCEEDED", groups.get(ExperimentProfile.Group.A).results().get(0).status());
        assertEquals(1, groups.get(ExperimentProfile.Group.B).failedRuns());
        assertEquals(0.0, groups.get(ExperimentProfile.Group.B).taskStatusSuccessRate());
        assertEquals(1, groups.get(ExperimentProfile.Group.B).booleanMetrics().get("testsPassed").falseCount());
        assertEquals(1, groups.get(ExperimentProfile.Group.C).cancelledRuns());
        var unknownEvidence = groups.get(ExperimentProfile.Group.D);
        assertEquals(1, unknownEvidence.terminalRuns());
        assertEquals(1, unknownEvidence.succeededRuns());
        assertEquals(0, unknownEvidence.validRuns());
        assertEquals(1, unknownEvidence.unknownResults());
        assertNull(unknownEvidence.booleanMetrics().get("testsPassed").rate());
        for (var group : List.of(ExperimentProfile.Group.E, ExperimentProfile.Group.F)) {
            var unfinished = groups.get(group);
            assertEquals(1, unfinished.plannedRuns());
            assertEquals(0, unfinished.terminalRuns());
            assertEquals(0, unfinished.succeededRuns());
            assertNull(unfinished.taskStatusSuccessRate());
            assertNull(unfinished.booleanMetrics().get("testsPassed").rate());
        }
        assertEquals("QUEUED", groups.get(ExperimentProfile.Group.E).results().get(0).status());
        assertEquals("RUNNING", groups.get(ExperimentProfile.Group.F).results().get(0).status());
        assertEquals(2, groups.get(ExperimentProfile.Group.F).results().get(0).attempt());
        assertTrue(earlierTaskSnapshot.stream().filter(task -> task.getExperimentGroup() != ExperimentProfile.Group.E)
                .allMatch(task -> task.getStatus() == TaskStatus.RUNNING));
    }
}
