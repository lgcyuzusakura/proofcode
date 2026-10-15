package dev.proofcode.control.data;
import java.util.*;
import org.springframework.data.jpa.repository.JpaRepository;
public interface DataSchemaRepository extends JpaRepository<DataSchemaSnapshot,UUID> {
    Optional<DataSchemaSnapshot> findFirstByConnectionIdAndSchemaVersionOrderByCreatedAtDesc(UUID connectionId,String schemaVersion);
}
