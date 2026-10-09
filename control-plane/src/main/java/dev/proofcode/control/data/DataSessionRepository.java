package dev.proofcode.control.data;
import jakarta.persistence.LockModeType;
import java.util.*;
import org.springframework.data.jpa.repository.*;
import org.springframework.data.repository.query.Param;
public interface DataSessionRepository extends JpaRepository<DataSession,UUID> {
    List<DataSession> findByProjectIdOrderByCreatedAtDesc(UUID projectId);
    @Lock(LockModeType.PESSIMISTIC_WRITE) @Query("select s from DataSession s where s.id=:id") Optional<DataSession> lockById(@Param("id") UUID id);
}
