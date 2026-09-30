package dev.proofcode.control.websocket;

import java.util.Map;
import org.springframework.context.annotation.Configuration;
import org.springframework.http.server.ServerHttpRequest;
import org.springframework.http.server.ServerHttpResponse;
import org.springframework.web.socket.WebSocketHandler;
import org.springframework.web.socket.config.annotation.EnableWebSocket;
import org.springframework.web.socket.config.annotation.WebSocketConfigurer;
import org.springframework.web.socket.config.annotation.WebSocketHandlerRegistry;
import org.springframework.web.socket.server.HandshakeInterceptor;

@Configuration
@EnableWebSocket
public class WebSocketConfig implements WebSocketConfigurer {
    private final TaskSocketHandler handler;public WebSocketConfig(TaskSocketHandler handler){this.handler=handler;}
    @Override public void registerWebSocketHandlers(WebSocketHandlerRegistry registry){registry.addHandler(handler,"/ws/tasks/{taskId}").addInterceptors(new TaskIdInterceptor()).setAllowedOriginPatterns("*");}
    static class TaskIdInterceptor implements HandshakeInterceptor{
        @Override public boolean beforeHandshake(ServerHttpRequest request,ServerHttpResponse response,WebSocketHandler wsHandler,Map<String,Object> attributes){String path=request.getURI().getPath();attributes.put("taskId",path.substring(path.lastIndexOf('/')+1));return true;}
        @Override public void afterHandshake(ServerHttpRequest request,ServerHttpResponse response,WebSocketHandler wsHandler,Exception exception){}
    }
}

