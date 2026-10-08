package dev.proofcode.control.experiment;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;
public interface ExperimentRepository extends JpaRepository<ExperimentEntity,UUID>{
    List<ExperimentEntity> findByProjectIdOrderByCreatedAtDesc(UUID projectId);
    Optional<ExperimentEntity> findByProjectIdAndIdempotencyKey(UUID projectId,String idempotencyKey);
}
