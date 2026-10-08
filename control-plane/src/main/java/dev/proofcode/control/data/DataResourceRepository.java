package dev.proofcode.control.data;
import java.util.*;
import org.springframework.data.jpa.repository.JpaRepository;
public interface DataResourceRepository extends JpaRepository<DataResource,UUID> {
    List<DataResource> findByProjectId(UUID projectId);
}
