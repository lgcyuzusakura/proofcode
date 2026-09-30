package dev.proofcode.control;

import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.jms.core.JmsTemplate;

@SpringBootTest(properties={"spring.task.scheduling.enabled=false"})
class ContextLoadTest {
    @MockBean JmsTemplate jmsTemplate;
    @MockBean StringRedisTemplate redisTemplate;

    @Test void contextLoads() {}
}
