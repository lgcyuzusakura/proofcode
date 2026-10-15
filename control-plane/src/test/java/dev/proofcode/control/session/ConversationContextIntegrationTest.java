package dev.proofcode.control.session;

import dev.proofcode.control.project.ProjectController;
import dev.proofcode.control.task.*;
import dev.proofcode.control.websocket.TaskSocketHandler;
import java.util.*;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.web.server.ResponseStatusException;
import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest(properties="spring.task.scheduling.enabled=false")
class ConversationContextIntegrationTest {
    @MockBean JmsTemplate jms; @MockBean StringRedisTemplate redis; @MockBean TaskSocketHandler sockets;
    @Autowired ProjectController projects; @Autowired WorkspaceService scopes; @Autowired TaskService tasks;
    @Autowired ConversationMessageService messages; @Autowired ConversationContextService context;
    @Autowired ConversationMessageRepository archive; @Autowired TaskRepository taskRepository;
    private WorkspaceService.Scope scope(){
        var project=projects.create(new ProjectController.CreateProject("Context test","https://example.com/context.git","main","REMOTE_REPOSITORY",null,UUID.randomUUID().toString())).getBody();
        return scopes.resolve(project.getId(),null,null);
    }
    private TaskEntity task(WorkspaceService.Scope scope,String prompt,String mode){return tasks.createWithOutcome(scope.projectId(),prompt,"fixture",UUID.randomUUID().toString(),scope.workspaceId(),scope.conversationId(),null,null,null,null,mode,null).task();}
    private TaskEntity turn(WorkspaceService.Scope scope,String prompt,String answer){var task=task(scope,prompt,"CHAT");task.claim(UUID.randomUUID(),java.time.Instant.now());task.complete(answer);taskRepository.saveAndFlush(task);messages.finish(task,answer,"SUCCEEDED");return task;}
    private UUID user(WorkspaceService.Scope scope,TaskEntity task){return messages.list(scope.projectId(),scope.workspaceId(),scope.conversationId(),0).stream().filter(item->item.getTaskId().equals(task.getId())&&item.getRole().equals("user")).findFirst().orElseThrow().getId();}
    private ConversationContextService.Preview preview(WorkspaceService.Scope scope){return context.preview(scope.projectId(),scope.workspaceId(),scope.conversationId());}
    private ConversationContextService.Preview edit(WorkspaceService.Scope scope,List<ConversationContextService.Item> items){var old=preview(scope);return context.update(scope.projectId(),scope.workspaceId(),scope.conversationId(),old.version(),new ConversationContextService.View("FOCUS",java.util.stream.Stream.concat(messages.list(scope.projectId(),scope.workspaceId(),scope.conversationId(),0).stream().filter(message->message.getRole().equals("user")&&items.stream().noneMatch(item->item.messageId().equals(message.getId()))).map(message->new ConversationContextService.Item(message.getId(),"INCLUDE",null)),items.stream()).toList(),old.view().packIds()));}
    @Test void exclusionChangesChatAndCodeInputWithoutAddingIgnoreMessagesOrDeletingArchive(){
        var scope=scope();var irrelevant=turn(scope,"unrelated weather SECRET_BRANCH","SECRET_BRANCH answer");var relevant=turn(scope,"Use Java 17","Java 17 confirmed");
        edit(scope,List.of(new ConversationContextService.Item(user(scope,irrelevant),"EXCLUDE",null)));
        var chat=task(scope,"Continue","CHAT");var code=task(scope,"Implement","CODE");
        for(var task:List.of(chat,code)){
            var history=messages.history(task);assertEquals(2,history.size());
            assertEquals("Use Java 17",history.get(0).content());assertFalse(history.toString().contains("SECRET_BRANCH"));assertFalse(history.toString().contains("ignore"));
            assertNotNull(taskRepository.findById(task.getId()).orElseThrow().getContextSnapshot());
        }
        assertTrue(messages.list(scope.projectId(),scope.workspaceId(),scope.conversationId(),0).stream().anyMatch(item->item.getContent().contains("SECRET_BRANCH")));
        assertEquals(irrelevant.getPrompt(),taskRepository.findById(irrelevant.getId()).orElseThrow().getPrompt());
    }
    @Test void snapshotsFreezeBeforeLaterEditsAndRetriesKeepTheirOriginalHistory(){
        var scope=scope();var first=turn(scope,"original topic","answer");var second=task(scope,"current","CHAT");
        var history=messages.history(second);String hash=second.getContextSelectionHash();
        edit(scope,List.of(new ConversationContextService.Item(user(scope,first),"EXCLUDE",null)));
        assertEquals(history,messages.history(second));tasks.cancel(second.getId());var retry=tasks.retry(second.getId());
        assertEquals(hash,retry.getContextSelectionHash());assertEquals(history,messages.history(retry));
        assertFalse(messages.history(task(scope,"new task","CHAT")).toString().contains("original topic"));
    }
    @Test void replacementAndFocusAreRealInputsAndCanBeRestored(){
        var scope=scope();var first=turn(scope,"long old discussion unrelated secret","old answer");turn(scope,"other tangent","other answer");
        var current=preview(scope);
        var changed=context.update(scope.projectId(),scope.workspaceId(),scope.conversationId(),current.version(),new ConversationContextService.View("FOCUS",List.of(new ConversationContextService.Item(user(scope,first),"REPLACE","Only preserve Java 17")),List.of()));
        assertEquals(1,changed.messages().size());assertTrue(changed.messages().get(0).content().contains("Only preserve Java 17"));assertFalse(changed.messages().toString().contains("unrelated secret"));assertEquals(1,changed.excludedTurns());
        var restored=context.update(scope.projectId(),scope.workspaceId(),scope.conversationId(),changed.version(),ConversationContextService.automatic());
        assertTrue(restored.messages().toString().contains("unrelated secret"));assertEquals(0,restored.excludedTurns());
    }
    @Test void automaticIgnoresStoredManualChoicesAndSwitchingBackRestoresThem(){
        var scope=scope();var tangent=turn(scope,"independent tangent","tangent answer");var relevant=turn(scope,"project requirement","requirement answer");
        var items=List.of(new ConversationContextService.Item(user(scope,tangent),"EXCLUDE",null),new ConversationContextService.Item(user(scope,relevant),"REPLACE","keep only project facts"));
        var manual=context.update(scope.projectId(),scope.workspaceId(),scope.conversationId(),0,new ConversationContextService.View("FOCUS",items,List.of()));
        assertEquals(1,manual.messages().size());assertFalse(manual.messages().toString().contains("independent tangent"));
        var auto=context.update(scope.projectId(),scope.workspaceId(),scope.conversationId(),manual.version(),new ConversationContextService.View("AUTO",items,List.of()));
        assertEquals(4,auto.messages().size());assertEquals(0,auto.excludedTurns());assertTrue(auto.messages().toString().contains("independent tangent"));assertFalse(auto.messages().toString().contains("keep only project facts"));assertEquals(items,auto.view().items());
        var again=context.update(scope.projectId(),scope.workspaceId(),scope.conversationId(),auto.version(),new ConversationContextService.View("FOCUS",auto.view().items(),List.of()));
        assertEquals(manual.messages(),again.messages());
    }
    @Test void packForkCarriesOnlySelectedMaterialAndCanBeDetached(){
        var scope=scope();turn(scope,"excluded tangent","bad answer");var selected=turn(scope,"use Java 17","good answer");
        var original=preview(scope);var ids=original.messages().stream().filter(item->archive.findById(item.id()).orElseThrow().getTaskId().equals(selected.getId())).map(ConversationContextService.InputMessage::id).toList();
        var fork=context.fork(scope.projectId(),scope.workspaceId(),scope.conversationId(),original.version(),"Java constraints",ids);
        var next=new WorkspaceService.Scope(scope.projectId(),scope.workspaceId(),fork.getId());
        assertEquals(List.of("use Java 17","good answer"),preview(next).messages().stream().map(ConversationContextService.InputMessage::content).toList());
        assertEquals("use Java 17",messages.history(task(next,"continue","CODE")).get(0).content());
        var state=preview(next);context.update(next.projectId(),next.workspaceId(),next.conversationId(),state.version(),new ConversationContextService.View("AUTO",List.of(),List.of()));
        assertTrue(preview(next).packs().isEmpty());
    }
    @Test void modifiedSourcesInvalidatePacksAndBlockNewTasksButNotExistingSnapshots(){
        var scope=scope();var first=turn(scope,"sensitive obsolete branch","answer");var current=preview(scope);
        var fork=context.fork(scope.projectId(),scope.workspaceId(),scope.conversationId(),current.version(),"old pack",current.messages().stream().map(ConversationContextService.InputMessage::id).toList());
        var target=new WorkspaceService.Scope(scope.projectId(),scope.workspaceId(),fork.getId());var existing=task(target,"old task","CHAT");
        edit(scope,List.of(new ConversationContextService.Item(user(scope,first),"EXCLUDE",null)));
        assertTrue(preview(target).packs().get(0).stale());assertFalse(preview(target).messages().toString().contains("sensitive obsolete branch"));
        assertEquals(409,assertThrows(ResponseStatusException.class,()->task(target,"next","CHAT")).getStatusCode().value());
        assertTrue(messages.history(existing).toString().contains("sensitive obsolete branch"));
    }
    @Test void rejectsVersionConflictsAndForeignMessageOrPackIds(){
        var scope=scope();var other=scope();var first=turn(scope,"first","answer");var foreign=turn(other,"private","answer");
        var original=preview(scope);edit(scope,List.of(new ConversationContextService.Item(user(scope,first),"EXCLUDE",null)));
        assertEquals(409,assertThrows(ResponseStatusException.class,()->context.update(scope.projectId(),scope.workspaceId(),scope.conversationId(),original.version(),ConversationContextService.automatic())).getStatusCode().value());
        assertThrows(IllegalArgumentException.class,()->edit(scope,List.of(new ConversationContextService.Item(user(other,foreign),"INCLUDE",null))));
        var from=preview(other);var pack=context.savePack(other.projectId(),other.workspaceId(),other.conversationId(),from.version(),"private pack",from.messages().stream().map(ConversationContextService.InputMessage::id).toList());
        assertEquals(404,assertThrows(ResponseStatusException.class,()->context.update(scope.projectId(),scope.workspaceId(),scope.conversationId(),preview(scope).version(),new ConversationContextService.View("AUTO",List.of(),List.of(pack.getId())))).getStatusCode().value());
    }
    @Test void packRequiresWholeCompletedQuestionAnswerPair(){
        var scope=scope();var task=task(scope,"question","CHAT");messages.finish(task,"answer","SUCCEEDED");
        var current=preview(scope);assertEquals(2,current.messages().size());
        assertThrows(IllegalArgumentException.class,()->context.savePack(scope.projectId(),scope.workspaceId(),scope.conversationId(),current.version(),"partial",List.of(current.messages().get(0).id())));
        var pack=context.savePack(scope.projectId(),scope.workspaceId(),scope.conversationId(),current.version(),"complete",current.messages().stream().map(ConversationContextService.InputMessage::id).toList());
        assertNotNull(pack.getId());
    }
    @Test void mountingAndDetachingPackDoesNotInvalidateItsOwnSourceMaterial(){
        var scope=scope();turn(scope,"keep this topic","answer");var current=preview(scope);
        var pack=context.savePack(scope.projectId(),scope.workspaceId(),scope.conversationId(),current.version(),"self pack",current.messages().stream().map(ConversationContextService.InputMessage::id).toList());
        var mounted=context.update(scope.projectId(),scope.workspaceId(),scope.conversationId(),current.version(),new ConversationContextService.View("AUTO",List.of(),List.of(pack.getId())));
        assertFalse(mounted.packs().get(0).stale());assertEquals(2,mounted.messages().size());
        var excluded=context.update(scope.projectId(),scope.workspaceId(),scope.conversationId(),mounted.version(),new ConversationContextService.View("FOCUS",List.of(new ConversationContextService.Item(current.messages().get(0).id(),"EXCLUDE",null)),List.of(pack.getId())));
        assertTrue(excluded.packs().get(0).stale());assertTrue(excluded.messages().isEmpty());
    }
    @Test void filteringPrecedesHistoryLimitsAndPreviewNeverCreatesTasksOrMessages(){
        var scope=scope();var first=turn(scope,"x".repeat((2<<20)+1),"answer");
        assertThrows(ResponseStatusException.class,()->task(scope,"too large","CHAT"));
        long tasksBefore=taskRepository.count(),messagesBefore=archive.count();
        var selected=edit(scope,List.of(new ConversationContextService.Item(user(scope,first),"EXCLUDE",null)));
        assertTrue(selected.messages().isEmpty());preview(scope);assertEquals(tasksBefore,taskRepository.count());assertEquals(messagesBefore,archive.count());
        assertTrue(messages.history(task(scope,"clean","CHAT")).isEmpty());
    }
    @Test void legacyRetryDoesNotReimportItsOwnEarlierAttempt(){
        var scope=scope();turn(scope,"prior history","answer");var task=task(scope,"own request","CHAT");tasks.cancel(task.getId());
        var legacy=taskRepository.findById(task.getId()).orElseThrow();
        // Simulate a pre-migration task with no captured projection.
        org.springframework.test.util.ReflectionTestUtils.setField(legacy,"contextSnapshot",null);taskRepository.saveAndFlush(legacy);
        var retry=tasks.retry(task.getId());assertFalse(messages.history(retry).toString().contains("own request"));assertTrue(messages.history(retry).toString().contains("prior history"));
    }
}
