package dev.proofcode.control.task;

import java.time.Instant;
import java.util.UUID;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.*;

class TaskEntityLeaseTest {
    @Test
    void verifyingLeaseCanBeRenewedAndReclaimedAfterExpiry() {
        Instant now=Instant.now();
        TaskEntity task=new TaskEntity(UUID.randomUUID(),UUID.randomUUID(),"Fix code","model",now);
        task.transition(TaskStatus.QUEUED);
        UUID first=UUID.randomUUID();
        UUID second=UUID.randomUUID();
        assertTrue(task.claim(first,now));
        task.transition(TaskStatus.VERIFYING);
        assertTrue(task.renew(first,now.plusSeconds(10)));
        assertEquals(TaskStatus.VERIFYING,task.getStatus());
        assertFalse(task.renew(second,now.plusSeconds(10)));
        assertFalse(task.claim(second,now.plusSeconds(11)));
        assertTrue(task.claim(second,now.plusSeconds(56)));
        assertEquals(TaskStatus.RUNNING,task.getStatus());
    }
}
