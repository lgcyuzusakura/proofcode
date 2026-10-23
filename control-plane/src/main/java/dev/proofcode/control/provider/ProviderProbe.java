package dev.proofcode.control.provider;

import com.fasterxml.jackson.databind.*;
import java.util.*;
import java.net.http.*;
import java.nio.ByteBuffer;
import java.io.ByteArrayOutputStream;
import java.util.concurrent.*;

// A bounded diagnostic of one selected model, never a tool executor. A successful
// sample proves this request shape, not general model quality or capabilities.
final class ProviderProbe {
    static final String TOOL="proofcode_probe";
    static final String PROMPT="Call proofcode_probe with ok=true. This is a connection test.";
    static String endpoint(String protocol){return switch(protocol){case "anthropic"->"/messages";case "responses"->"/responses";case "chat"->"/chat/completions";default->throw new IllegalArgumentException("unsupported provider protocol");};}
    static Map<String,Object> payload(String protocol,String model){
        var schema=Map.of("type","object","properties",Map.of("ok",Map.of("type","boolean")),"required",List.of("ok"),"additionalProperties",false);
        var function=Map.of("name",TOOL,"description","Return a connection test value; no side effects.","parameters",schema);
        Map<String,Object> payload=new LinkedHashMap<>();payload.put("model",model);payload.put("stream",false);
        switch(protocol){
            case "anthropic" -> {payload.put("max_tokens",128);payload.put("messages",List.of(Map.of("role","user","content",PROMPT)));payload.put("tools",List.of(Map.of("name",TOOL,"description","Connection test only.","input_schema",schema)));payload.put("tool_choice",Map.of("type","tool","name",TOOL));}
            case "responses" -> {payload.put("max_output_tokens",128);payload.put("store",false);payload.put("input",List.of(Map.of("role","user","content",PROMPT)));var tool=new LinkedHashMap<String,Object>(function);tool.put("type","function");payload.put("tools",List.of(tool));payload.put("tool_choice",Map.of("type","function","name",TOOL));}
            case "chat" -> {payload.put("max_tokens",128);payload.put("messages",List.of(Map.of("role","user","content",PROMPT)));payload.put("tools",List.of(Map.of("type","function","function",function)));payload.put("tool_choice",Map.of("type","function","function",Map.of("name",TOOL)));}
            default -> throw new IllegalArgumentException("unsupported provider protocol");
        }
        return payload;
    }
    static boolean valid(String protocol,JsonNode root,ObjectMapper json){
        if(root==null||root.hasNonNull("error"))return false;
        try{
            JsonNode args;
            switch(protocol){
                case "anthropic" -> {
                    if(!"message".equals(root.path("type").asText())||!"tool_use".equals(root.path("stop_reason").asText()))return false;
                    var blocks=root.path("content");if(!blocks.isArray())return false;
                    var calls=new ArrayList<JsonNode>();for(var block:blocks)if("tool_use".equals(block.path("type").asText()))calls.add(block);
                    if(calls.size()!=1||!TOOL.equals(calls.get(0).path("name").asText())||calls.get(0).path("id").asText().isBlank())return false;
                    args=calls.get(0).path("input");
                }
                case "responses" -> {
                    if(!"completed".equals(root.path("status").asText()))return false;
                    var calls=new ArrayList<JsonNode>();for(var item:root.path("output"))if("function_call".equals(item.path("type").asText()))calls.add(item);
                    if(calls.size()!=1||!TOOL.equals(calls.get(0).path("name").asText())||calls.get(0).path("call_id").asText().isBlank())return false;
                    args=json.readTree(calls.get(0).path("arguments").asText());
                }
                case "chat" -> {
                    var choices=root.path("choices");if(!choices.isArray()||choices.size()!=1||!"tool_calls".equals(choices.path(0).path("finish_reason").asText()))return false;
                    var calls=choices.path(0).path("message").path("tool_calls");
                    if(!calls.isArray()||calls.size()!=1||calls.path(0).path("id").asText().isBlank()||!TOOL.equals(calls.path(0).path("function").path("name").asText()))return false;
                    args=json.readTree(calls.path(0).path("function").path("arguments").asText());
                }
                default -> {return false;}
            }
            return args!=null&&args.isObject()&&args.size()==1&&args.path("ok").isBoolean()&&args.path("ok").booleanValue();
        }catch(Exception ignored){return false;}
    }
    static String failure(int code){return switch(code){case 401->"authentication";case 403->"permission_or_policy";case 429,529->"rate_or_overload";case 400,413,422->"request_or_context";case 404->"endpoint_or_model";default->"provider";};}
    // Complete only after the bounded body arrives. Waiting on an InputStream
    // response would leave subsequent reads outside the diagnostic deadline.
    static HttpResponse<byte[]> send(HttpClient client,HttpRequest request) throws Exception {
        return send(client,request,20000);
    }
    static HttpResponse<byte[]> send(HttpClient client,HttpRequest request,long timeoutMillis) throws Exception {
        var pending=client.sendAsync(request,info->new BoundedBody());
        try{return pending.get(timeoutMillis,TimeUnit.MILLISECONDS);}finally{if(!pending.isDone())pending.cancel(true);}
    }
    private static final class BoundedBody implements HttpResponse.BodySubscriber<byte[]> {
        private final CompletableFuture<byte[]> result=new CompletableFuture<>();
        private final ByteArrayOutputStream bytes=new ByteArrayOutputStream();
        private Flow.Subscription subscription;
        public CompletionStage<byte[]> getBody(){return result;}
        public void onSubscribe(Flow.Subscription value){subscription=value;value.request(1);}
        public void onNext(List<ByteBuffer> chunks){
            for(var chunk:chunks){if(chunk.remaining()>(256<<10)-bytes.size()){subscription.cancel();result.completeExceptionally(new IllegalArgumentException("response limit exceeded"));return;}var data=new byte[chunk.remaining()];chunk.get(data);bytes.writeBytes(data);}
            subscription.request(1);
        }
        public void onError(Throwable value){result.completeExceptionally(value);}
        public void onComplete(){result.complete(bytes.toByteArray());}
    }
}
