package dev.proofcode.control.task;
import java.util.*;
import org.springframework.data.jpa.repository.JpaRepository;
public interface TaskInstructionRepository extends JpaRepository<TaskInstruction,UUID>{
 List<TaskInstruction> findByTaskIdAndAttemptOrderBySequenceAsc(UUID taskId,int attempt);
 Optional<TaskInstruction> findByTaskIdAndAttemptAndClientKey(UUID taskId,int attempt,String clientKey);
}
