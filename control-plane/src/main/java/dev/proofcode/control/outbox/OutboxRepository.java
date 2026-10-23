package dev.proofcode.control.outbox;
import java.time.Instant;import java.util.List;import java.util.UUID;import org.springframework.data.domain.Pageable;import org.springframework.data.jpa.repository.JpaRepository;
public interface OutboxRepository extends JpaRepository<OutboxEntity,UUID>{
    List<OutboxEntity> findByDeliveredAtIsNullAndNextAttemptAtBeforeOrderByCreatedAtAsc(Instant now,Pageable pageable);
    @org.springframework.data.jpa.repository.Lock(jakarta.persistence.LockModeType.PESSIMISTIC_WRITE)
    @org.springframework.data.jpa.repository.Query("select m from OutboxEntity m where m.deliveredAt is null and m.nextAttemptAt < :now order by m.createdAt, m.id")
    List<OutboxEntity> lockPending(@org.springframework.data.repository.query.Param("now") Instant now,Pageable pageable);
    @org.springframework.data.jpa.repository.Lock(jakarta.persistence.LockModeType.PESSIMISTIC_WRITE)
    @org.springframework.data.jpa.repository.Query("select m from OutboxEntity m where m.id = :id")
    java.util.Optional<OutboxEntity> lockById(@org.springframework.data.repository.query.Param("id") UUID id);
}

