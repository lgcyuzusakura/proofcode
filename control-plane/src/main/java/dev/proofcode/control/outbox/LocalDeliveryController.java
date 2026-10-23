package dev.proofcode.control.outbox;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.time.Instant;
import java.util.UUID;
import org.springframework.context.annotation.Profile;
import org.springframework.data.domain.PageRequest;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.server.ResponseStatusException;

/** The desktop worker leases the same transactional outbox used by team deployments. */
@RestController
@Profile("desktop")
@RequestMapping("/internal/local-deliveries")
public class LocalDeliveryController {
    private final OutboxRepository messages;
    private final ObjectMapper json;
    public LocalDeliveryController(OutboxRepository messages,ObjectMapper json){this.messages=messages;this.json=json;}

    @PostMapping("/claim")
    @Transactional
    public ResponseEntity<Delivery> claim() throws com.fasterxml.jackson.core.JsonProcessingException {
        var pending=messages.lockPending(Instant.now(),PageRequest.of(0,1));
        if(pending.isEmpty())return ResponseEntity.noContent().build();
        var message=pending.get(0);
        UUID token=UUID.randomUUID();
        message.lease(token,Instant.now().plusSeconds(60));
        return ResponseEntity.ok(new Delivery(message.getId(),token,json.readTree(message.getPayload())));
    }

    @PostMapping("/{id}/{action:renew|ack|release}")
    @Transactional
    public ResponseEntity<Void> finish(@PathVariable UUID id,@PathVariable String action,@RequestBody Receipt receipt){
        var message=messages.lockById(id).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));
        if(receipt==null||receipt.token()==null||!receipt.token().equals(message.getDeliveryToken()))throw new ResponseStatusException(HttpStatus.CONFLICT);
        if(message.getDeliveredAt()!=null){
            if(action.equals("ack"))return ResponseEntity.noContent().build();
            throw new ResponseStatusException(HttpStatus.CONFLICT);
        }
        if(!message.getNextAttemptAt().isAfter(Instant.now()))throw new ResponseStatusException(HttpStatus.CONFLICT);
        switch(action){
            case "ack" -> message.delivered();
            case "renew" -> message.lease(receipt.token(),Instant.now().plusSeconds(60));
            case "release" -> {message.failed();message.lease(null,message.getNextAttemptAt());}
            default -> throw new ResponseStatusException(HttpStatus.BAD_REQUEST);
        }
        return ResponseEntity.noContent().build();
    }
    public record Delivery(UUID id,UUID token,JsonNode task){}
    public record Receipt(UUID token){}
}
