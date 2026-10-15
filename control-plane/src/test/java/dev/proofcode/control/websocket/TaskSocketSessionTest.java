package dev.proofcode.control.websocket;

import dev.proofcode.control.account.AccountService;
import dev.proofcode.control.project.*;
import dev.proofcode.control.task.*;
import java.net.URI;
import java.time.Instant;
import java.util.*;
import org.junit.jupiter.api.Test;
import org.springframework.web.socket.*;
import static org.mockito.Mockito.*;

class TaskSocketSessionTest {
    @Test void logoutRevokesOpenSocketBeforeTheNextEvent() throws Exception {
        var accounts=mock(AccountService.class);
        var tasks=mock(TaskRepository.class);
        var projects=mock(ProjectRepository.class);
        UUID owner=UUID.randomUUID(),projectId=UUID.randomUUID(),taskId=UUID.randomUUID();
        var project=new ProjectEntity(projectId,"Private",null,"main",Instant.now());project.setOwnerId(owner);
        var task=new TaskEntity(taskId,projectId,"hello","model",null,Instant.now());
        when(tasks.findById(taskId)).thenReturn(Optional.of(task));
        when(projects.findById(projectId)).thenReturn(Optional.of(project));
        when(accounts.authenticate("session-token")).thenReturn(owner);
        var session=mock(WebSocketSession.class);
        when(session.getUri()).thenReturn(URI.create("ws://localhost/ws/tasks/"+taskId+"?token=session-token"));
        when(session.getAttributes()).thenReturn(new HashMap<>(Map.of("taskId",taskId.toString())));
        when(session.isOpen()).thenReturn(true);
        var handler=new TaskSocketHandler("change-me",accounts,tasks,projects);
        handler.afterConnectionEstablished(session);
        handler.broadcast(taskId,"first");
        verify(session).sendMessage(any(TextMessage.class));
        clearInvocations(session);
        when(accounts.authenticate("session-token")).thenReturn(null);
        handler.broadcast(taskId,"private-event");
        verify(session).close(any(CloseStatus.class));
        verify(session,never()).sendMessage(any(TextMessage.class));
    }
}
