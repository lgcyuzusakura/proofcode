package dev.proofcode.control.outbox;

import java.time.Instant;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.web.servlet.MockMvc;
import com.fasterxml.jackson.databind.ObjectMapper;
import static org.junit.jupiter.api.Assertions.*;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;

@SpringBootTest(properties={"spring.datasource.url=jdbc:h2:mem:local-delivery;MODE=PostgreSQL;DB_CLOSE_DELAY=-1","PROOFCODE_DATABASE_PASSWORD=fixture-only"})
@ActiveProfiles("desktop")
@AutoConfigureMockMvc
class LocalDeliveryIntegrationTest {
    @Autowired MockMvc http;
    @Autowired OutboxRepository messages;
    @Autowired ObjectMapper json;
    @Autowired org.springframework.context.ApplicationContext context;

    @Test void durableLeaseFencesStaleWorkersAndAcknowledgementIsIdempotent() throws Exception {
        assertTrue(context.getBeansOfType(OutboxDispatcher.class).isEmpty());
        assertTrue(context.getBeansOfType(org.springframework.jms.core.JmsTemplate.class).isEmpty());
        assertTrue(context.getBeansOfType(org.springframework.data.redis.core.StringRedisTemplate.class).isEmpty());
        messages.deleteAll();
        var message=messages.saveAndFlush(new OutboxEntity(UUID.randomUUID(),UUID.randomUUID(),"test.tasks","{\"prompt\":\"preserve exact task\"}",Instant.now()));
        http.perform(post("/internal/local-deliveries/claim")).andExpect(status().isForbidden());
        http.perform(post("/internal/local-deliveries/claim").header("Authorization","Bearer test-token")).andExpect(status().isForbidden());
        var first=claim();
        assertEquals("preserve exact task",first.path("task").path("prompt").asText());
        http.perform(post("/internal/local-deliveries/claim").header("Authorization","Bearer runner-token")).andExpect(status().isNoContent());
        var expired=messages.findById(message.getId()).orElseThrow();
        expired.lease(UUID.fromString(first.path("token").asText()),Instant.now().minusSeconds(1));messages.saveAndFlush(expired);
        var second=claim();
        assertNotEquals(first.path("token"),second.path("token"));
        receipt(first,"ack",409);
        receipt(second,"renew",204);
        receipt(second,"release",204);
        receipt(second,"ack",409);
        var retry=messages.findById(message.getId()).orElseThrow();
        retry.lease(null,Instant.now().minusSeconds(1));messages.saveAndFlush(retry);
        var third=claim();
        receipt(third,"ack",204);receipt(third,"ack",204);
        assertNotNull(messages.findById(message.getId()).orElseThrow().getDeliveredAt());
        http.perform(post("/internal/local-deliveries/claim").header("Authorization","Bearer runner-token")).andExpect(status().isNoContent());
    }
    private com.fasterxml.jackson.databind.JsonNode claim() throws Exception {
        return json.readTree(http.perform(post("/internal/local-deliveries/claim").header("Authorization","Bearer runner-token")).andExpect(status().isOk()).andReturn().getResponse().getContentAsString());
    }
    private void receipt(com.fasterxml.jackson.databind.JsonNode delivery,String action,int status) throws Exception {
        http.perform(post("/internal/local-deliveries/"+delivery.path("id").asText()+"/"+action).header("Authorization","Bearer runner-token").contentType("application/json").content("{\"token\":\""+delivery.path("token").asText()+"\"}")).andExpect(status().is(status));
    }
}
