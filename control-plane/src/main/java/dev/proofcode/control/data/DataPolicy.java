package dev.proofcode.control.data;
import com.fasterxml.jackson.databind.JsonNode;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.*;
import org.springframework.http.HttpStatus;
import org.springframework.web.server.ResponseStatusException;

public final class DataPolicy {
    private DataPolicy(){}
    static ResponseStatusException bad(String msg){return new ResponseStatusException(HttpStatus.BAD_REQUEST,msg);}
    static ResponseStatusException conflict(String msg){return new ResponseStatusException(HttpStatus.CONFLICT,msg);}
    static ResponseStatusException forbidden(){return new ResponseStatusException(HttpStatus.FORBIDDEN,"data operation is outside the permitted scope");}
    static String hash(String text){try{return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(text.getBytes(StandardCharsets.UTF_8)));}catch(Exception e){throw new IllegalStateException(e);}}
    static void fields(JsonNode node,String... allowed){if(node==null||!node.isObject())throw bad("object required");Set<String> keys=Set.of(allowed);node.fieldNames().forEachRemaining(k->{if(!keys.contains(k))throw bad("unknown IR field: "+k);});}
    static String required(JsonNode node,String key){JsonNode v=node.get(key);if(v==null||!v.isTextual()||v.asText().isBlank())throw bad(key+" is required");return v.asText();}
    static String identifier(String value){if(value==null||!value.matches("[A-Za-z_][A-Za-z0-9_]{0,62}"))throw bad("invalid database identifier");return "\""+value+"\"";}
    static int integer(JsonNode n,String key,int min,int max,int fallback){if(!n.has(key))return fallback;JsonNode v=n.get(key);if(!v.isIntegralNumber()||!v.canConvertToInt()||v.intValue()<min||v.intValue()>max)throw bad("invalid "+key);return v.intValue();}
}
