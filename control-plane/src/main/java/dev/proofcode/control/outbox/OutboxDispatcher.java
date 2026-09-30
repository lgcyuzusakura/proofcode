package dev.proofcode.control.outbox;

import java.time.Instant;
import org.springframework.data.domain.PageRequest;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;
import org.springframework.transaction.annotation.Transactional;

@Component
public class OutboxDispatcher {
    private final OutboxRepository repository; private final JmsTemplate jms;
    public OutboxDispatcher(OutboxRepository repository,JmsTemplate jms){this.repository=repository;this.jms=jms;this.jms.setDeliveryPersistent(true);}
    @Scheduled(fixedDelayString="${proofcode.outbox-delay-ms:500}")
    @Transactional
    public void dispatch(){for(OutboxEntity message:repository.findByDeliveredAtIsNullAndNextAttemptAtBeforeOrderByCreatedAtAsc(Instant.now().plusMillis(1),PageRequest.of(0,50))){try{jms.convertAndSend(message.getDestination(),message.getPayload(),value->{value.setStringProperty("taskId",message.getAggregateId().toString());value.setStringProperty("outboxId",message.getId().toString());return value;});message.delivered();}catch(RuntimeException error){message.failed();}}}
}

