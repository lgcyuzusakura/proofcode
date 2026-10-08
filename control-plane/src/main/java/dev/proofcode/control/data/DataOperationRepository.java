package dev.proofcode.control.data;
import jakarta.persistence.LockModeType;
import java.util.*;
import org.springframework.data.jpa.repository.*;
import org.springframework.data.repository.query.Param;
public interface DataOperationRepository extends JpaRepository<DataOperation,UUID> {
    @Lock(LockModeType.PESSIMISTIC_WRITE) @Query("select o from DataOperation o where o.id=:id") Optional<DataOperation> lockById(@Param("id")UUID id);
    Optional<DataOperation> findByTaskIdAndAttemptAndIdempotencyKey(UUID task,int attempt,String key);
    List<DataOperation> findByProjectIdOrderByCreatedAtDesc(UUID project);
}
