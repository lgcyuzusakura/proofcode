package dev.proofcode.control.outbox;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name="outbox_messages")
public class OutboxEntity {
    @Id private UUID id;
    @Column(name="aggregate_id",nullable=false) private UUID aggregateId;
    @Column(nullable=false) private String destination;
    @Column(nullable=false,columnDefinition="jsonb") private String payload;
    @Column(nullable=false) private int attempts;
    @Column(name="next_attempt_at",nullable=false) private Instant nextAttemptAt;
    @Column(name="delivered_at") private Instant deliveredAt;
    @Column(name="created_at",nullable=false) private Instant createdAt;
    protected OutboxEntity(){}
    public OutboxEntity(UUID id,UUID aggregateId,String destination,String payload,Instant now){this.id=id;this.aggregateId=aggregateId;this.destination=destination;this.payload=payload;this.nextAttemptAt=now;this.createdAt=now;}
    public void delivered(){this.deliveredAt=Instant.now();} public void failed(){this.attempts++;this.nextAttemptAt=Instant.now().plusSeconds(Math.min(300L,1L<<Math.min(attempts,8)));}
    public UUID getId(){return id;} public UUID getAggregateId(){return aggregateId;} public String getDestination(){return destination;} public String getPayload(){return payload;} public int getAttempts(){return attempts;} public Instant getNextAttemptAt(){return nextAttemptAt;} public Instant getDeliveredAt(){return deliveredAt;}
}

