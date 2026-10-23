package dev.proofcode.control.agentplatform;
import java.util.*;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
public interface AgentResourceRepository extends JpaRepository<AgentResource,UUID> {
 List<AgentResource> findByProjectIdAndKindOrderByNameAsc(UUID projectId,String kind);
 Optional<AgentResource> findByProjectIdAndKindAndName(UUID projectId,String kind,String name);
 @Query("select resource.version from AgentResource resource where resource.projectId = :projectId and resource.id = :id and resource.kind = :kind")
 Optional<Long> currentVersion(UUID projectId,UUID id,String kind);
}
