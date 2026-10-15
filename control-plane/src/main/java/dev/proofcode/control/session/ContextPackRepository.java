package dev.proofcode.control.session;
import java.util.*;
import org.springframework.data.jpa.repository.JpaRepository;
public interface ContextPackRepository extends JpaRepository<ContextPack,UUID> {
    List<ContextPack> findByProjectIdAndWorkspaceIdOrderByCreatedAtDesc(UUID projectId,UUID workspaceId);
}
