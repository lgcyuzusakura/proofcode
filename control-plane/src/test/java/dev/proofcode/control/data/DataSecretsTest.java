package dev.proofcode.control.data;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.Base64;
import org.junit.jupiter.api.Test;
import org.springframework.mock.env.MockEnvironment;
import org.springframework.web.server.ResponseStatusException;
import static org.junit.jupiter.api.Assertions.*;

class DataSecretsTest {
    private final ObjectMapper json=new ObjectMapper();
    private static String key(int marker){byte[] bytes=new byte[32];java.util.Arrays.fill(bytes,(byte)marker);return Base64.getEncoder().encodeToString(bytes);}
    @Test void executionPinsCredentialsAndReturnsCopiesEvenWhenConfigurationChanges(){
        MockEnvironment env=new MockEnvironment().withProperty("DATA_SECRET_PG","{\"host\":\"first\"}");DataSecrets secrets=new DataSecrets(env,json);
        secrets.withPinned("PG",()->{ObjectNode first=(ObjectNode)secrets.resolve("PG");assertEquals("first",first.path("host").asText());first.put("host","caller-mutated");env.setProperty("DATA_SECRET_PG","{\"host\":\"second\"}");assertEquals("first",secrets.resolve("PG").path("host").asText());return null;});
        assertEquals("second",secrets.resolve("PG").path("host").asText());
    }
    @Test void nestedPinsRestoreOuterExecutionAndExceptionClearsThreadLocalState(){
        MockEnvironment env=new MockEnvironment().withProperty("DATA_SECRET_PG","{\"host\":\"outer\"}");DataSecrets secrets=new DataSecrets(env,json);
        assertThrows(IllegalStateException.class,()->secrets.withPinned("PG",()->{assertEquals("outer",secrets.resolve("PG").path("host").asText());env.setProperty("DATA_SECRET_PG","{\"host\":\"inner\"}");secrets.withPinned("PG",()->{assertEquals("inner",secrets.resolve("PG").path("host").asText());return null;});assertEquals("outer",secrets.resolve("PG").path("host").asText());throw new IllegalStateException("provider failure");}));
        assertEquals("inner",secrets.resolve("PG").path("host").asText());
    }
    @Test void encryptionKeyIsPinnedForTheExecutionAndCiphertextIsAuthenticated(){
        MockEnvironment env=new MockEnvironment().withProperty("DATA_SNAPSHOT_KEY",key(1));DataSecrets secrets=new DataSecrets(env,json);
        String ciphertext=secrets.withPinned("PG",()->{String value=secrets.seal("private recovery rows");env.setProperty("DATA_SNAPSHOT_KEY",key(2));assertEquals("private recovery rows",secrets.unseal(value));assertNotEquals(value,secrets.seal("private recovery rows"));return value;});
        assertThrows(ResponseStatusException.class,()->secrets.unseal(ciphertext));env.setProperty("DATA_SNAPSHOT_KEY",key(1));assertEquals("private recovery rows",secrets.unseal(ciphertext));byte[] bytes=Base64.getDecoder().decode(ciphertext);bytes[bytes.length-1]^=1;assertThrows(ResponseStatusException.class,()->secrets.unseal(Base64.getEncoder().encodeToString(bytes)));
    }
    @Test void malformedSecretAndInvalidSnapshotKeyAreRejected(){
        MockEnvironment env=new MockEnvironment().withProperty("DATA_SECRET_PG","[]").withProperty("DATA_SNAPSHOT_KEY","not-base64");DataSecrets secrets=new DataSecrets(env,json);
        assertThrows(ResponseStatusException.class,()->secrets.resolve("PG"));assertThrows(ResponseStatusException.class,()->secrets.resolve("../PG"));assertThrows(ResponseStatusException.class,()->secrets.seal("x"));
    }
}
