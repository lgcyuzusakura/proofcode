package dev.proofcode.control.agentplatform;

import com.fasterxml.jackson.databind.ObjectMapper;
import java.nio.file.*;
import java.time.Duration;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.condition.*;
import org.junit.jupiter.api.io.TempDir;
import static org.junit.jupiter.api.Assertions.*;

class McpInspectorTest {
 @TempDir Path temp;
 private McpServerService.ProbeConfiguration config(){return new McpServerService.ProbeConfiguration("docs","https://example.com/mcp","fixture-secret",false);}
 @Test void missingHelperReturnsActionableResultWithoutDisclosingConfiguration(){
  try(var inspector=new McpInspector(new ObjectMapper().findAndRegisterModules(),temp.resolve("missing-helper").toString(),200)){
   var report=inspector.inspect(config());assertEquals("unavailable",report.code());assertFalse(report.ok());assertTrue(report.tools().isEmpty());assertFalse(report.toString().contains("fixture-secret"));
  }
 }
 private Path helper(String script)throws Exception{
  Path file=temp.resolve("helper");Files.writeString(file,"#!/bin/sh\n"+script+"\n");assertTrue(file.toFile().setExecutable(true));return file;
 }
 @Test @EnabledOnOs(OS.LINUX) void hungHelperIsTerminatedWithinDeadline()throws Exception{
  var executable=helper("exec sleep 30");
  try(var inspector=new McpInspector(new ObjectMapper().findAndRegisterModules(),executable.toString(),150)){
   assertTimeoutPreemptively(Duration.ofSeconds(3),()->assertEquals("timeout",inspector.inspect(config()).code()));
  }
 }
 @Test @EnabledOnOs(OS.LINUX) void malformedAndOversizedProcessOutputIsRejected()throws Exception{
  var executable=helper("cat >/dev/null\nprintf 'not-json'");
  try(var inspector=new McpInspector(new ObjectMapper().findAndRegisterModules(),executable.toString(),1000)){assertEquals("helper_error",inspector.inspect(config()).code());}
  helper("cat >/dev/null\nhead -c 1048577 /dev/zero");
  try(var inspector=new McpInspector(new ObjectMapper().findAndRegisterModules(),executable.toString(),1000)){assertTimeoutPreemptively(Duration.ofSeconds(3),()->assertEquals("helper_error",inspector.inspect(config()).code()));}
 }
}
