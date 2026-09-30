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
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;
import org.springframework.web.socket.CloseStatus;
import org.springframework.web.socket.TextMessage;
import org.springframework.web.socket.WebSocketSession;
import org.springframework.web.socket.handler.TextWebSocketHandler;

@Component
public class TaskSocketHandler extends TextWebSocketHandler {
    private final ConcurrentHashMap<UUID,Set<WebSocketSession>> sessions=new ConcurrentHashMap<>();private final String token;
    public TaskSocketHandler(@Value("${proofcode.auth-token}")String token){this.token=token;}
    @Override public void afterConnectionEstablished(WebSocketSession session)throws Exception{URI uri=session.getUri();String query=uri==null?null:uri.getRawQuery();String supplied=null;if(query!=null){supplied=Arrays.stream(query.split("&")).map(pair->pair.split("=",2)).filter(pair->pair.length==2&&pair[0].equals("token")).map(pair->URLDecoder.decode(pair[1],StandardCharsets.UTF_8)).findFirst().orElse(null);}if(supplied==null||!MessageDigest.isEqual(supplied.getBytes(StandardCharsets.UTF_8),token.getBytes(StandardCharsets.UTF_8))){session.close(CloseStatus.NOT_ACCEPTABLE.withReason("invalid token"));return;}UUID taskId=UUID.fromString((String)session.getAttributes().get("taskId"));sessions.computeIfAbsent(taskId,key->ConcurrentHashMap.newKeySet()).add(session);}
    @Override public void afterConnectionClosed(WebSocketSession session,CloseStatus status){Object value=session.getAttributes().get("taskId");if(value instanceof String id){Set<WebSocketSession> values=sessions.get(UUID.fromString(id));if(values!=null)values.remove(session);}}
    public void broadcast(UUID taskId,String payload){Set<WebSocketSession> values=sessions.get(taskId);if(values==null)return;for(WebSocketSession session:values){if(!session.isOpen())continue;try{session.sendMessage(new TextMessage(payload));}catch(IOException ignored){}}}
}
