package dev.proofcode.control.task;
import java.util.List;import java.util.UUID;import org.springframework.data.jpa.repository.JpaRepository;
public interface TaskEventRepository extends JpaRepository<TaskEventEntity,Long>{
    List<TaskEventEntity> findTop500ByTaskIdOrderBySequenceAsc(UUID taskId);
    List<TaskEventEntity> findTop500ByTaskIdAndSequenceGreaterThanOrderBySequenceAsc(UUID taskId,long sequence);
    List<TaskEventEntity> findByTaskIdAndAttemptAndTypeInOrderBySequenceDesc(UUID taskId,int attempt,List<String> types);
    List<TaskEventEntity> findByTaskIdInAndTypeInOrderBySequenceDesc(List<UUID> taskIds,List<String> types);
    boolean existsByTaskIdAndSequence(UUID taskId,long sequence);
    boolean existsByTaskIdAndEventKey(UUID taskId,String eventKey);
    @org.springframework.data.jpa.repository.Query("select e from TaskEventEntity e where e.taskId=:taskId and e.type in :types and e.sequence<:before order by e.sequence desc")
    List<TaskEventEntity> history(UUID taskId,List<String> types,long before,org.springframework.data.domain.Pageable page);
    @org.springframework.data.jpa.repository.Query("select coalesce(max(e.sequence), 0) from TaskEventEntity e where e.taskId = :taskId") long maxSequence(@org.springframework.data.repository.query.Param("taskId") UUID taskId);
}
