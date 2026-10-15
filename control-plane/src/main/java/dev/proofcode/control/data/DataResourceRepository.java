package dev.proofcode.control.data;
import java.util.*;
import org.springframework.data.jpa.repository.JpaRepository;
import jakarta.persistence.LockModeType;
import org.springframework.data.jpa.repository.*;
import org.springframework.data.repository.query.Param;
public interface DataResourceRepository extends JpaRepository<DataResource,UUID> {
    List<DataResource> findByProjectId(UUID projectId);
    @Lock(LockModeType.PESSIMISTIC_WRITE) @Query("select r from DataResource r where r.id=:id") Optional<DataResource> lockById(@Param("id")UUID id);
}
