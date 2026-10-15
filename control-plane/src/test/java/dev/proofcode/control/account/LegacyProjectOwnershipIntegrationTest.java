package dev.proofcode.control.account;

import dev.proofcode.control.project.*;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.time.Instant;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.jms.core.JmsTemplate;
import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest(properties={"spring.datasource.url=jdbc:h2:mem:legacy-ownership;MODE=PostgreSQL;DB_CLOSE_DELAY=-1", "spring.task.scheduling.enabled=false"})
class LegacyProjectOwnershipIntegrationTest {
    @MockBean JmsTemplate jms;
    @MockBean StringRedisTemplate redis;
    @MockBean TaskSocketHandler sockets;
    @Autowired AccountService accounts;
    @Autowired ProjectRepository projects;

    @Test void firstAccountClaimsLegacyProjectsAndOtherAccountsCannotReassignThem() {
        var legacy=projects.saveAndFlush(new ProjectEntity(UUID.randomUUID(),"Legacy",null,"main","SCRATCH",null,null,Instant.now()));
        var first=accounts.register("legacy-owner", "password-long-enough").user();
        assertEquals(first.id(),projects.findById(legacy.getId()).orElseThrow().getOwnerId());
        var upgrade=projects.saveAndFlush(new ProjectEntity(UUID.randomUUID(),"Upgrade",null,"main","SCRATCH",null,null,Instant.now()));
        accounts.login("legacy-owner", "password-long-enough");
        assertEquals(first.id(),projects.findById(upgrade.getId()).orElseThrow().getOwnerId());
        accounts.register("second-owner", "password-long-enough");
        var unclaimed=projects.saveAndFlush(new ProjectEntity(UUID.randomUUID(),"Unclaimed",null,"main","SCRATCH",null,null,Instant.now()));
        accounts.login("second-owner", "password-long-enough");
        accounts.login("legacy-owner", "password-long-enough");
        assertNull(projects.findById(unclaimed.getId()).orElseThrow().getOwnerId());
        assertEquals(first.id(),projects.findById(legacy.getId()).orElseThrow().getOwnerId());
    }
}
