package dev.proofcode.control.agentplatform;

import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import jakarta.annotation.PreDestroy;
import java.io.*;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.*;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;

/** Fixed-purpose process adapter. Network policy and MCP discovery stay in the Go transport. */
@Service
public class McpInspector implements AutoCloseable {
 private static final int MAX_OUTPUT=1<<20;
 private static final Set<String> CODES=Set.of("ok","configuration","dns","private_host","timeout","network","authentication","forbidden","redirect","rate_limit","http","protocol","catalog","tool_name","catalog_size");
 private final ObjectMapper json;
 private final String executable;
 private final long timeoutMillis;
 private final Semaphore slots=new Semaphore(2);
 private final ExecutorService io=Executors.newFixedThreadPool(4,r->{var thread=new Thread(r,"mcp-inspection-io");thread.setDaemon(true);return thread;});

 public McpInspector(ObjectMapper json,@Value("${proofcode.mcp.inspect-bin:proofcode-mcp-inspect}")String executable,@Value("${proofcode.mcp.inspect-timeout-ms:20000}")long timeoutMillis){
  this.json=json;this.executable=executable;this.timeoutMillis=Math.max(100,Math.min(22000,timeoutMillis));
 }
 public record ToolPreview(String name,String qualifiedName,String description,JsonNode inputSchema){}
 public record Report(boolean ok,String code,String stage,Integer httpStatus,String protocol,Instant checkedAt,long elapsedMs,List<ToolPreview> tools){}

 Report inspect(McpServerService.ProbeConfiguration config){
  long started=System.nanoTime();
  if(!slots.tryAcquire())return failure("busy",started);
  Process process=null;Future<byte[]> reader=null;Future<?> writer=null;
  try{
   if(executable==null||executable.isBlank())return failure("unavailable",started);
   byte[] input=json.writeValueAsBytes(config);
   if(input.length>32*1024)return failure("configuration",started);
   try{process=new ProcessBuilder(executable).redirectError(ProcessBuilder.Redirect.DISCARD).start();}
   catch(IOException unavailable){return failure("unavailable",started);}
   final Process child=process;
   reader=io.submit(()->{
    try(var output=child.getInputStream()){
     byte[] value=output.readNBytes(MAX_OUTPUT+1);
     if(value.length>MAX_OUTPUT){child.destroyForcibly();throw new IOException("inspection output limit");}
     return value;
    }
   });
   writer=io.submit(()->{try(var stdin=child.getOutputStream()){stdin.write(input);}catch(IOException failure){throw new UncheckedIOException(failure);}});
   if(!process.waitFor(timeoutMillis,TimeUnit.MILLISECONDS))return failure("timeout",started);
   if(process.exitValue()!=0)return failure("helper_error",started);
   writer.get(1,TimeUnit.SECONDS);
   var report=json.readerFor(Report.class).with(DeserializationFeature.FAIL_ON_TRAILING_TOKENS).<Report>readValue(reader.get(1,TimeUnit.SECONDS));
   if(!valid(report))return failure("helper_error",started);
   // The helper redacts metadata. Repeat exact-secret redaction at the public boundary.
   if((config.bearer()!=null&&!config.bearer().isEmpty())||(config.headerValue()!=null&&!config.headerValue().isEmpty())){
    var secrets=java.util.stream.Stream.of(config.bearer(),config.headerValue()).filter(value->value!=null&&!value.isEmpty()).toList();
    var tools=report.tools().stream().map(tool->{String name=tool.name(),qualified=tool.qualifiedName(),description=tool.description();JsonNode schema=tool.inputSchema();for(String secret:secrets){name=name.replace(secret,"[redacted]");qualified=qualified.replace(secret,"[redacted]");description=description.replace(secret,"[redacted]");schema=redact(schema,secret);}return new ToolPreview(name,qualified,description,schema);}).toList();
    report=new Report(report.ok(),report.code(),report.stage(),report.httpStatus(),report.protocol(),report.checkedAt(),report.elapsedMs(),tools);
   }
   return new Report(report.ok(),report.code(),report.stage(),report.httpStatus(),report.protocol(),report.checkedAt(),elapsed(started),report.tools());
  }catch(InterruptedException interrupted){Thread.currentThread().interrupt();return failure("cancelled",started);}
  catch(Exception failure){return failure("helper_error",started);}
  finally{
   if(process!=null){
    process.destroyForcibly();
    try{process.getOutputStream().close();process.getInputStream().close();}catch(IOException ignored){}
   }
   if(reader!=null)reader.cancel(true);if(writer!=null)writer.cancel(true);
   slots.release();
  }
 }

 private boolean valid(Report value){
  if(value==null||!CODES.contains(value.code())||!Set.of("initialize","catalog","complete").contains(value.stage())||value.checkedAt()==null||value.elapsedMs()<0||value.tools()==null||value.tools().size()>256||value.ok()!=value.code().equals("ok"))return false;
  if(!value.ok()&&!value.tools().isEmpty())return false;
  return value.tools().stream().allMatch(tool->tool!=null&&tool.name()!=null&&tool.qualifiedName()!=null&&tool.description()!=null&&tool.inputSchema()!=null&&tool.inputSchema().isObject());
 }
 private JsonNode redact(JsonNode value,String secret){
  if(value.isTextual())return json.getNodeFactory().textNode(value.textValue().replace(secret,"[redacted]"));
  if(value.isObject()){
   var clean=json.createObjectNode();value.fields().forEachRemaining(entry->clean.set(entry.getKey().replace(secret,"[redacted]"),redact(entry.getValue(),secret)));return clean;
  }
  if(value.isArray()){var clean=json.createArrayNode();value.forEach(child->clean.add(redact(child,secret)));return clean;}
  return value;
 }
 private static long elapsed(long started){return TimeUnit.NANOSECONDS.toMillis(System.nanoTime()-started);}
 private static Report failure(String code,long started){return new Report(false,code,"initialize",null,null,Instant.now(),elapsed(started),List.of());}
 @PreDestroy public void close(){io.shutdownNow();}
}
