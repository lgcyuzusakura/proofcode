package dev.proofcode.control.source;
import java.util.*;
import org.springframework.data.jpa.repository.JpaRepository;
public interface SourceSnapshotRepository extends JpaRepository<SourceSnapshot,UUID>{
    Optional<SourceSnapshot> findByProjectIdAndWorkspaceIdAndManifestHash(UUID projectId,UUID workspaceId,String manifestHash);
    @org.springframework.data.jpa.repository.Query("select s from SourceSnapshot s, TaskEntity t where t.resultSourceSnapshotId=s.id and t.projectId=:projectId and t.workspaceId=:workspaceId and t.status=dev.proofcode.control.task.TaskStatus.SUCCEEDED order by t.updatedAt desc")
    List<SourceSnapshot> completedSources(@org.springframework.data.repository.query.Param("projectId") UUID projectId,@org.springframework.data.repository.query.Param("workspaceId") UUID workspaceId,org.springframework.data.domain.Pageable page);
}
