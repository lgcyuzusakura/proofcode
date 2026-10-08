package dev.proofcode.control.data;

import com.fasterxml.jackson.databind.*;
import org.springframework.core.env.Environment;
import org.springframework.stereotype.Component;
import java.nio.charset.StandardCharsets;
import java.security.*;
import java.util.*;
import javax.crypto.*;
import javax.crypto.spec.*;

/** Secrets are resolved only on the server. Neither connection strings nor plaintext snapshots are public DTOs. */
@Component
public class DataSecrets {
    private final Environment env; private final ObjectMapper json;
    public DataSecrets(Environment env,ObjectMapper json){this.env=env;this.json=json;}
    public JsonNode resolve(String ref){
        if(ref==null||!ref.matches("[A-Z][A-Z0-9_]{0,79}"))throw DataPolicy.bad("invalid secret reference");
        String value=env.getProperty("DATA_SECRET_"+ref);
        if(value==null)throw DataPolicy.bad("data source secret is not configured");
        try{return json.readTree(value);}catch(Exception e){throw DataPolicy.bad("data source secret configuration is invalid");}
    }
    public String seal(String value){
        try{byte[] nonce=new byte[12];new SecureRandom().nextBytes(nonce);Cipher c=Cipher.getInstance("AES/GCM/NoPadding");c.init(Cipher.ENCRYPT_MODE,key(),new GCMParameterSpec(128,nonce));byte[] body=c.doFinal(value.getBytes(StandardCharsets.UTF_8));byte[] all=new byte[nonce.length+body.length];System.arraycopy(nonce,0,all,0,nonce.length);System.arraycopy(body,0,all,nonce.length,body.length);return Base64.getEncoder().encodeToString(all);}catch(Exception e){throw DataPolicy.bad("snapshot encryption is not configured");}
    }
    public String unseal(String value){try{byte[] all=Base64.getDecoder().decode(value);Cipher c=Cipher.getInstance("AES/GCM/NoPadding");c.init(Cipher.DECRYPT_MODE,key(),new GCMParameterSpec(128,Arrays.copyOf(all,12)));return new String(c.doFinal(Arrays.copyOfRange(all,12,all.length)),StandardCharsets.UTF_8);}catch(Exception e){throw DataPolicy.bad("snapshot cannot be decrypted");}}
    private SecretKeySpec key(){String k=env.getProperty("DATA_SNAPSHOT_KEY");if(k==null)throw new IllegalStateException();byte[] raw=Base64.getDecoder().decode(k);if(raw.length!=32)throw new IllegalStateException();return new SecretKeySpec(raw,"AES");}
}
