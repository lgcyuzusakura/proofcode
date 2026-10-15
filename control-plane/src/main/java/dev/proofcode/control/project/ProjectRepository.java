package dev.proofcode.control.project;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;
import jakarta.persistence.LockModeType;
import java.util.Optional;
public interface ProjectRepository extends JpaRepository<ProjectEntity, UUID> {
    java.util.List<ProjectEntity> findByOwnerIdOrderByCreatedAtDesc(UUID ownerId);
    @org.springframework.data.jpa.repository.Modifying
    @org.springframework.data.jpa.repository.Query("update ProjectEntity p set p.ownerId=:owner where p.ownerId is null")
    int claimUnowned(@org.springframework.data.repository.query.Param("owner") UUID owner);
    Optional<ProjectEntity> findByBootstrapId(String bootstrapId);
    @Lock(LockModeType.PESSIMISTIC_WRITE)
    @Query("select p from ProjectEntity p where p.id = :id")
    Optional<ProjectEntity> lockById(@Param("id") UUID id);
}
