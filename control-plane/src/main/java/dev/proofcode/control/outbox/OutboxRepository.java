package dev.proofcode.control.outbox;
import java.time.Instant;import java.util.List;import java.util.UUID;import org.springframework.data.domain.Pageable;import org.springframework.data.jpa.repository.JpaRepository;
public interface OutboxRepository extends JpaRepository<OutboxEntity,UUID>{List<OutboxEntity> findByDeliveredAtIsNullAndNextAttemptAtBeforeOrderByCreatedAtAsc(Instant now,Pageable pageable);}

