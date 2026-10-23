package dev.proofcode.control.agentplatform;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.*;
import java.util.*;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.*;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

/** Runs the network-restricted OAuth implementation through the fixed Go helper. */
@Component
class McpOAuthClient {
 private final ObjectMapper json;
 private final String executable;
 private final Semaphore slots=new Semaphore(2);
 private final ExecutorService io=Executors.newCachedThreadPool(r->{var t=new Thread(r,"mcp-oauth-helper-io");t.setDaemon(true);return t;});
 McpOAuthClient(ObjectMapper json,@Value("${proofcode.mcp.inspect-bin:proofcode-mcp-inspect}")String executable){this.json=json;this.executable=executable;}
 JsonNode call(String operation,Map<String,Object> input){
  if(!slots.tryAcquire())throw new IllegalStateException("MCP OAuth is busy");Process process=null;Future<byte[]> stdout=null,stderr=null;Future<?> stdin=null;
  try{
   Map<String,Object> request=new LinkedHashMap<>(input);request.put("operation",operation);
   byte[] payload=json.writeValueAsBytes(request);if(payload.length>32*1024)throw new IllegalArgumentException();
   process=new ProcessBuilder(executable).start();Process child=process;
   stdout=io.submit(()->read(child.getInputStream(),128<<10));stderr=io.submit(()->read(child.getErrorStream(),8<<10));stdin=io.submit(()->{try(var stream=child.getOutputStream()){stream.write(payload);}return null;});
   if(!process.waitFor(18,TimeUnit.SECONDS)){process.destroyForcibly();throw new IllegalStateException("MCP OAuth operation timed out");}
   if(process.exitValue()!=0)throw new IllegalStateException("MCP OAuth operation failed");
   stdin.get(1,TimeUnit.SECONDS);byte[] output=stdout.get(1,TimeUnit.SECONDS);if(output.length>128<<10)throw new IllegalStateException("MCP OAuth response is too large");
   JsonNode value=json.reader().with(com.fasterxml.jackson.databind.DeserializationFeature.FAIL_ON_TRAILING_TOKENS).readTree(output);
   if(value==null||value.has("error"))throw new IllegalStateException("MCP OAuth operation failed");
   return value;
  }catch(InterruptedException interrupted){Thread.currentThread().interrupt();throw new IllegalStateException("MCP OAuth operation was interrupted");}
  catch(Exception failure){throw new IllegalStateException("MCP OAuth operation failed");}
  finally{if(process!=null){process.destroyForcibly();try{process.getInputStream().close();process.getErrorStream().close();process.getOutputStream().close();}catch(IOException ignored){}}if(stdout!=null)stdout.cancel(true);if(stderr!=null)stderr.cancel(true);if(stdin!=null)stdin.cancel(true);slots.release();}
 }
 private static byte[] read(InputStream stream,int maximum)throws IOException{try(stream){byte[] value=stream.readNBytes(maximum+1);if(value.length>maximum)throw new IOException("output size");return value;}}
}
