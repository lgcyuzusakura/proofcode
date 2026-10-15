package dev.proofcode.control.data;
import java.util.*;
import org.springframework.data.jpa.repository.JpaRepository;
public interface DataAuditRepository extends JpaRepository<DataAudit,UUID> {List<DataAudit> findByProjectIdOrderByCreatedAtDesc(UUID project);}
