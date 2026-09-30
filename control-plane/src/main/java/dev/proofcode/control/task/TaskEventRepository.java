package dev.proofcode.control.task;
import java.util.List;import java.util.UUID;import org.springframework.data.jpa.repository.JpaRepository;
public interface TaskEventRepository extends JpaRepository<TaskEventEntity,Long>{List<TaskEventEntity> findByTaskIdOrderBySequenceAsc(UUID taskId);List<TaskEventEntity> findByTaskIdAndSequenceGreaterThanOrderBySequenceAsc(UUID taskId,long sequence);boolean existsByTaskIdAndSequence(UUID taskId,long sequence);}
