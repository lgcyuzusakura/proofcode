package dev.proofcode.control.agentplatform;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.task.*;
import java.util.*;
import org.springframework.http.*;
import org.springframework.web.bind.annotation.*;

@RestController
public class McpServerController {
 private final McpServerService service; private final AgentPlatformController agentApi; private final ObjectMapper json; private final McpInspector inspector;
 private final McpOAuthService oauth;
 public McpServerController(McpServerService service,AgentPlatformController agentApi,ObjectMapper json,McpInspector inspector,McpOAuthService oauth){this.service=service;this.agentApi=agentApi;this.json=json;this.inspector=inspector;this.oauth=oauth;}
 public record Diagnostic(long savedVersion,McpInspector.Report result){}
 @GetMapping("/api/projects/{project}/mcp-servers")public List<McpServerService.View> list(@PathVariable UUID project){return service.list(project);}
 @PostMapping("/api/projects/{project}/mcp-servers")public McpServerService.View create(@PathVariable UUID project,@RequestBody McpServerService.Input input){return service.save(project,null,input);}
 @PatchMapping("/api/projects/{project}/mcp-servers/{id}")public McpServerService.View update(@PathVariable UUID project,@PathVariable UUID id,@RequestBody McpServerService.Input input){return service.save(project,id,input);}
 @PostMapping("/api/projects/{project}/mcp-servers/{id}/delete")public ResponseEntity<Void> delete(@PathVariable UUID project,@PathVariable UUID id,@RequestParam long version){service.delete(project,id,version);return ResponseEntity.noContent().build();}
 @PostMapping("/api/projects/{project}/mcp-servers/{id}/probe")public ResponseEntity<Diagnostic> probe(@PathVariable UUID project,@PathVariable UUID id,@RequestParam long version){
  var config=service.inspectConfiguration(project,id,version);
  var result=inspector.inspect(config);
  service.verifyVersion(project,id,version);
  // A remote 401/403 is a diagnostic result, never an account authentication response.
  return ResponseEntity.ok().cacheControl(CacheControl.noStore()).body(new Diagnostic(version,result));
 }
 @GetMapping("/api/projects/{project}/mcp-servers/{id}/oauth")public McpOAuthService.Status oauthStatus(@PathVariable UUID project,@PathVariable UUID id,@RequestParam long version){return oauth.status(project,id,version);}
 @PutMapping("/api/projects/{project}/mcp-servers/{id}/oauth")public McpOAuthService.Status oauthSave(@PathVariable UUID project,@PathVariable UUID id,@RequestParam long version,@RequestBody McpOAuthService.Input input){return oauth.save(project,id,version,input);}
 @PostMapping("/api/projects/{project}/mcp-servers/{id}/oauth/begin")public McpOAuthService.Authorization oauthBegin(@PathVariable UUID project,@PathVariable UUID id,@RequestParam long version){return oauth.begin(project,id,version);}
 @PostMapping("/api/projects/{project}/mcp-servers/{id}/oauth/cancel")public McpOAuthService.Status oauthCancel(@PathVariable UUID project,@PathVariable UUID id,@RequestParam long version){return oauth.cancel(project,id,version);}
 @GetMapping(value="/api/mcp/oauth/callback",produces=MediaType.TEXT_HTML_VALUE)public ResponseEntity<String> oauthCallback(@RequestParam(required=false)String state,@RequestParam(required=false)String code,@RequestParam(required=false)String error){UUID grant=oauth.claimCallback(state,code,error);boolean success=grant!=null&&oauth.exchange(grant,code);String title=success?"MCP 服务授权成功":"MCP 服务授权失败",message=success?"授权已经保存，可以关闭此页面返回 ProofCode。":"授权无效、已过期或未能完成。请返回 ProofCode 后重新连接 MCP 服务。";String html="<!doctype html><html lang=\"zh-CN\"><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>"+title+"</title><body style=\"font:16px system-ui,sans-serif;max-width:560px;margin:12vh auto;padding:24px;color:#25282b\"><h1 style=\"font-size:22px\">"+title+"</h1><p style=\"line-height:1.7\">"+message+"</p></body></html>";return ResponseEntity.status(success?200:400).cacheControl(CacheControl.noStore()).contentType(MediaType.TEXT_HTML).body(html);}
 @GetMapping("/internal/tasks/{task}/mcp-servers")public ResponseEntity<List<McpServerService.Connection>> resolve(@PathVariable UUID task,@RequestParam UUID runnerId,@RequestParam int attempt)throws Exception{
  // Reuse the exact lease checks of the immutable agent snapshot endpoint.
  var raw=agentApi.snapshot(task,runnerId,attempt).getBody();
  var snapshot=raw==null||raw.equals("null")?null:json.readValue(raw,AgentPlatformService.Snapshot.class);
  UUID project=agentApi.taskProject(task);
  return ResponseEntity.ok().cacheControl(CacheControl.noStore()).body(snapshot==null?List.of():service.connections(project,snapshot.remoteMcp()));
 }
 @GetMapping("/internal/tasks/{task}/mcp-servers/{server}/oauth-token")public ResponseEntity<McpOAuthService.Connection> refreshOAuth(@PathVariable UUID task,@PathVariable UUID server,@RequestParam UUID runnerId,@RequestParam int attempt){var raw=agentApi.snapshot(task,runnerId,attempt).getBody();try{var snapshot=raw==null||raw.equals("null")?null:json.readValue(raw,AgentPlatformService.Snapshot.class);var reference=snapshot==null?null:snapshot.remoteMcp().stream().filter(value->value.id().equals(server)).findFirst().orElse(null);if(reference==null||reference.oauth()==null)throw new org.springframework.web.server.ResponseStatusException(HttpStatus.CONFLICT,"MCP OAuth task binding mismatch");var result=oauth.resolve(reference.oauth(),agentApi.taskProject(task),reference.id(),reference.version(),reference.url());return ResponseEntity.ok().cacheControl(CacheControl.noStore()).body(result);}catch(org.springframework.web.server.ResponseStatusException e){throw e;}catch(Exception e){throw new org.springframework.web.server.ResponseStatusException(HttpStatus.CONFLICT,"MCP OAuth task binding is invalid");}}
}
