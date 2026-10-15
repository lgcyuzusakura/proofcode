package dev.proofcode.control.websocket;

import java.io.IOException;
import java.net.URI;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.Arrays;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import dev.proofcode.control.account.AccountService;
import dev.proofcode.control.task.TaskRepository;
import dev.proofcode.control.project.ProjectRepository;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;
import org.springframework.web.socket.CloseStatus;
import org.springframework.web.socket.TextMessage;
import org.springframework.web.socket.WebSocketSession;
import org.springframework.web.socket.handler.TextWebSocketHandler;

@Component
public class TaskSocketHandler extends TextWebSocketHandler {
    private final ConcurrentHashMap<UUID,Set<WebSocketSession>> sessions=new ConcurrentHashMap<>();private final String token;private final AccountService accounts;private final TaskRepository tasks;private final ProjectRepository projects;
    public TaskSocketHandler(@Value("${proofcode.auth-token}")String token,AccountService accounts,TaskRepository tasks,ProjectRepository projects){this.token=token;this.accounts=accounts;this.tasks=tasks;this.projects=projects;}
    @Override public void afterConnectionEstablished(WebSocketSession session)throws Exception{URI uri=session.getUri();String query=uri==null?null:uri.getRawQuery();String supplied=null;if(query!=null){supplied=Arrays.stream(query.split("&")).map(pair->pair.split("=",2)).filter(pair->pair.length==2&&pair[0].equals("token")).map(pair->URLDecoder.decode(pair[1],StandardCharsets.UTF_8)).findFirst().orElse(null);}UUID account=supplied==null?null:accounts.authenticate(supplied);UUID taskId=UUID.fromString((String)session.getAttributes().get("taskId"));var task=tasks.findById(taskId).orElse(null);var project=task==null?null:projects.findById(task.getProjectId()).orElse(null);boolean legacy=supplied!=null&&!"change-me".equals(token)&&MessageDigest.isEqual(supplied.getBytes(StandardCharsets.UTF_8),token.getBytes(StandardCharsets.UTF_8));if(task==null||project==null||!(account!=null&&account.equals(project.getOwnerId())||legacy&&project.getOwnerId()==null)){session.close(CloseStatus.NOT_ACCEPTABLE.withReason("invalid token or task scope"));return;}session.getAttributes().put("accountToken",supplied);sessions.computeIfAbsent(taskId,key->ConcurrentHashMap.newKeySet()).add(session);}
    @Override public void afterConnectionClosed(WebSocketSession session,CloseStatus status){Object value=session.getAttributes().get("taskId");if(value instanceof String id){Set<WebSocketSession> values=sessions.get(UUID.fromString(id));if(values!=null)values.remove(session);}}
    public void broadcast(UUID taskId,String payload){
        Set<WebSocketSession> values=sessions.get(taskId);
        if(values==null)return;
        for(WebSocketSession session:values){
            if(!session.isOpen()){values.remove(session);continue;}
            try{
                String supplied=(String)session.getAttributes().get("accountToken");
                var task=tasks.findById(taskId).orElse(null);
                var project=task==null?null:projects.findById(task.getProjectId()).orElse(null);
                UUID account=supplied==null?null:accounts.authenticate(supplied);
                boolean legacy=supplied!=null&&!"change-me".equals(token)&&MessageDigest.isEqual(supplied.getBytes(StandardCharsets.UTF_8),token.getBytes(StandardCharsets.UTF_8));
                if(project==null||!(account!=null&&account.equals(project.getOwnerId())||legacy&&project.getOwnerId()==null)){
                    session.close(CloseStatus.NOT_ACCEPTABLE.withReason("session expired or scope changed"));
                    values.remove(session);
                    continue;
                }
                session.sendMessage(new TextMessage(payload));
            }catch(IOException ignored){}
        }
    }
}
