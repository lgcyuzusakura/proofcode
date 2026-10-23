package dev.proofcode.control.agentplatform;

import com.fasterxml.jackson.databind.*;
import dev.proofcode.control.data.DataSecrets;
import dev.proofcode.control.session.WorkspaceService;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.time.*;
import java.util.*;
import java.util.concurrent.Semaphore;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.HttpStatus;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.support.TransactionTemplate;
import org.springframework.web.server.ResponseStatusException;

@Service
public class McpOAuthService {
 private final McpOAuthGrantRepository grants; private final AgentResourceRepository resources; private final WorkspaceService scopes; private final DataSecrets secrets; private final ObjectMapper json; private final McpOAuthClient client; private final String redirectUri; private final boolean allowPrivate; private final Semaphore slots=new Semaphore(2); private final TransactionTemplate tx;
 McpOAuthService(McpOAuthGrantRepository grants,AgentResourceRepository resources,WorkspaceService scopes,DataSecrets secrets,ObjectMapper json,McpOAuthClient client,PlatformTransactionManager manager,@Value("${proofcode.mcp.oauth-redirect-uri:http://localhost:8080/api/mcp/oauth/callback}")String redirectUri,@Value("${proofcode.mcp.allow-private-hosts:false}")boolean allowPrivate){this.grants=grants;this.resources=resources;this.scopes=scopes;this.secrets=secrets;this.json=json;this.client=client;this.redirectUri=validateRedirect(redirectUri);this.allowPrivate=allowPrivate;this.tx=new TransactionTemplate(manager);}
 public record Input(String clientId,String clientSecret,String scope,boolean clearClientSecret) { @Override public String toString(){return "[private OAuth configuration]";} }
 public record Status(boolean configured,String clientId,String scope,boolean hasClientSecret,String redirectUri,String state,long serverVersion){}
 public record Authorization(String authorizationUrl){}
 public record Pinned(UUID grantId){}
 public record Connection(String grantId,String accessToken,long expiresAtEpochSecond){@Override public String toString(){return "[private OAuth connection]";}}
 public record ServerConfig(String clientId,String encryptedSecret,String scope){}
 public record StoredConfig(String url,String encryptedBearer,String headerName,String encryptedHeader,boolean enabled,ServerConfig oauth){public StoredConfig withOAuth(ServerConfig value){return new StoredConfig(url,encryptedBearer,headerName,encryptedHeader,enabled,value);}}
 private record Configuration(String clientId,String encryptedClientSecret,String scope,String verifier,String metadataJson,String redirectUri) { @Override public String toString(){return "[private OAuth authorization]";} }
 private record Tokens(String accessToken,String refreshToken,String scope) { @Override public String toString(){return "[private OAuth tokens]";} }

 @Transactional public Status status(UUID project,UUID serverId,long version){var server=require(project,serverId,version);var grant=grants.findFirstByServerIdAndServerVersionOrderByCreatedAtDesc(serverId,version).orElse(null);var config=readOAuth(server.body);String state=grant==null?"NOT_CONNECTED":grant.status;if(grant!=null&&grant.pendingUntil!=null&&!grant.pendingUntil.isAfter(Instant.now())&&Set.of("PENDING","EXCHANGING").contains(state))state="EXPIRED";return new Status(config!=null,config==null?"":config.clientId(),config==null?"":config.scope(),config!=null&&config.encryptedSecret()!=null,redirectUri,state,version);}

 @Transactional public Status save(UUID project,UUID serverId,long version,Input input){var server=require(project,serverId,version);if(input==null)throw new IllegalArgumentException("OAuth 配置无效");var root=readStored(server.body);var old=root.oauth();String clientId=input.clientId()==null?"":input.clientId().trim(),scope=input.scope()==null?"":input.scope().trim(),secret=old==null?null:old.encryptedSecret();
  if(!clientId.isEmpty()&&(clientId.length()>512||clientId.chars().anyMatch(c->c<33||c>126)))throw new IllegalArgumentException("OAuth 客户端 ID 无效");
  if(scope.length()>1024||scope.chars().anyMatch(c->c<32||c>126))throw new IllegalArgumentException("OAuth 权限范围无效");
  if(input.clearClientSecret())secret=null;if(input.clientSecret()!=null&&!input.clientSecret().isEmpty()){if(input.clientSecret().length()>4096||input.clientSecret().chars().anyMatch(c->c<33||c>126))throw new IllegalArgumentException("OAuth 客户端密钥无效");secret=secrets.seal(input.clientSecret());}
  if(clientId.isEmpty()){root=root.withOAuth(null);}
  else{if(old!=null&&!old.clientId().equals(clientId)&&old.encryptedSecret()!=null&&!input.clearClientSecret()&&(input.clientSecret()==null||input.clientSecret().isEmpty()))throw new IllegalArgumentException("更换客户端 ID 时请重新输入或清除客户端密钥");root=root.withOAuth(new ServerConfig(clientId,secret,scope));}
  server.body=writeStored(root);resources.saveAndFlush(server);
  return status(project,serverId,server.version);
 }

 public Authorization begin(UUID project,UUID serverId,long version){require(project,serverId,version);if(!slots.tryAcquire())throw new ResponseStatusException(HttpStatus.TOO_MANY_REQUESTS,"OAuth 请求繁忙，请稍后重试");try{
  var server=require(project,serverId,version);var body=readStored(server.body);var oauth=body.oauth();if(oauth==null)throw new IllegalArgumentException("请先配置 OAuth 客户端 ID");String plainSecret=oauth.encryptedSecret()==null?"":secrets.unseal(oauth.encryptedSecret());
  JsonNode metadata=client.call("oauth-discover",Map.of("url",body.url(),"allowPrivateHosts",allowPrivate));
  JsonNode begun=client.call("oauth-begin",Map.of("metadata",metadata,"clientId",oauth.clientId(),"clientSecret",plainSecret,"redirectUri",redirectUri,"scope",oauth.scope()));
  String state=text(begun,"state",1024),verifier=text(begun,"codeVerifier",1024),url=text(begun,"url",8192);String stateHash=sha256(state);
  var metadataJson=json.writeValueAsString(metadata);var configuration=new Configuration(oauth.clientId(),oauth.encryptedSecret(),oauth.scope(),verifier,metadataJson,redirectUri);
  McpOAuthGrant grant=new McpOAuthGrant();grant.id=UUID.randomUUID();grant.projectId=project;grant.serverId=serverId;grant.serverVersion=version;grant.url=body.url();grant.stateHash=stateHash;grant.pendingUntil=Instant.now().plus(Duration.ofMinutes(10));grant.status="PENDING";grant.encryptedConfiguration=secrets.seal(encode(configuration));grant.createdAt=Instant.now();tx.executeWithoutResult(status->{if(resources.currentVersion(project,serverId,"MCP").filter(v->v==version).isEmpty())throw new ResponseStatusException(HttpStatus.CONFLICT,"MCP 配置已变化，请重新加载");grants.saveAndFlush(grant);});return new Authorization(url);
 }catch(ResponseStatusException e){throw e;}catch(Exception e){throw new IllegalStateException("无法连接 MCP OAuth 授权服务，请检查地址及 OAuth 配置");}finally{slots.release();}}

 @Transactional public UUID claimCallback(String state,String code,String error){if(state==null||state.length()<32||state.length()>256)return null;
  String hash=sha256(state);var grant=grants.findByStateHash(hash).orElse(null);if(grant==null)return null;
  boolean valid=code!=null&&!code.isBlank()&&code.length()<=4096&&(error==null||error.isBlank())&&resources.currentVersion(grant.projectId,grant.serverId,"MCP").filter(v->v==grant.serverVersion).isPresent();
  UUID id=grant.id;return grants.claim(id,hash,Instant.now(),Instant.now().plusSeconds(60),valid?"EXCHANGING":"CANCELLED")==1&&valid?id:null;
 }

 public boolean exchange(UUID id,String code){return exchangeGrant(id,code);}

 private boolean exchangeGrant(UUID id,String code){if(!slots.tryAcquire())return false;try{
  var grant=grants.findById(id).orElse(null);if(grant==null)return false;Configuration config=decode(secrets.unseal(grant.encryptedConfiguration),Configuration.class);JsonNode metadata=json.readTree(config.metadataJson());String secret=config.encryptedClientSecret()==null?"":secrets.unseal(config.encryptedClientSecret());
  JsonNode tokens=client.call("oauth-exchange",Map.of("metadata",metadata,"clientId",config.clientId(),"clientSecret",secret,"redirectUri",config.redirectUri(),"scope",config.scope(),"code",code,"codeVerifier",config.verifier(),"allowPrivateHosts",allowPrivate));
  String access=text(tokens,"access_token",4096),refresh=tokens.path("refresh_token").asText("");Tokens value=new Tokens(access,refresh,tokens.path("scope").asText(""));
  return Boolean.TRUE.equals(tx.execute(status->{var current=grants.locked(id).orElse(null);if(current==null||!current.status.equals("EXCHANGING")||current.pendingUntil==null||!current.pendingUntil.isAfter(Instant.now())||resources.currentVersion(current.projectId,current.serverId,"MCP").filter(v->v==current.serverVersion).isEmpty())return false;current.encryptedTokens=secrets.seal(encode(value));current.tokenExpiresAt=expiry(tokens);current.encryptedConfiguration=secrets.seal(encode(new Configuration(config.clientId(),config.encryptedClientSecret(),config.scope(),"",config.metadataJson(),config.redirectUri())));current.pendingUntil=null;current.status="AUTHORIZED";return true;}));
 }catch(Exception ignored){tx.executeWithoutResult(status->grants.locked(id).filter(v->v.status.equals("EXCHANGING")).ifPresent(v->{v.status="ERROR";v.pendingUntil=null;v.encryptedConfiguration="";}));return false;}finally{slots.release();}}

 @Transactional public Connection resolve(Pinned reference,UUID taskProject,UUID expectedServer,long expectedVersion,String expectedUrl){UUID id=reference.grantId();
  var grant=grants.locked(id).orElseThrow(()->new ResponseStatusException(HttpStatus.CONFLICT,"MCP OAuth connection is unavailable"));if(!grant.projectId.equals(taskProject)||!grant.serverId.equals(expectedServer)||grant.serverVersion!=expectedVersion||!grant.url.equals(expectedUrl)||!grant.status.equals("AUTHORIZED")||grant.encryptedTokens==null)throw new ResponseStatusException(HttpStatus.CONFLICT,"MCP OAuth connection changed; reconnect the service");
  if(grant.tokenExpiresAt==null||grant.tokenExpiresAt.isAfter(Instant.now().plusSeconds(30)))return connection(grant);
  Tokens old=decode(secrets.unseal(grant.encryptedTokens),Tokens.class);if(old.refreshToken().isEmpty())throw new ResponseStatusException(HttpStatus.CONFLICT,"MCP OAuth access expired; reconnect the service");
  if(!slots.tryAcquire())throw new ResponseStatusException(HttpStatus.TOO_MANY_REQUESTS,"OAuth 请求繁忙，请稍后重试");try{
   Configuration config=decode(secrets.unseal(grant.encryptedConfiguration),Configuration.class);JsonNode metadata=json.readTree(config.metadataJson());JsonNode rotated=client.call("oauth-refresh",Map.of("metadata",metadata,"clientId",config.clientId(),"clientSecret",config.encryptedClientSecret()==null?"":secrets.unseal(config.encryptedClientSecret()),"scope",config.scope(),"refreshToken",old.refreshToken(),"allowPrivateHosts",allowPrivate));String access=text(rotated,"access_token",4096),refresh=rotated.path("refresh_token").asText("");Tokens next=new Tokens(access,refresh.isEmpty()?old.refreshToken():refresh,rotated.path("scope").asText(old.scope()));grant.encryptedTokens=secrets.seal(encode(next));grant.tokenExpiresAt=expiry(rotated);return connection(grant);
  }catch(Exception e){grant.status="ERROR";throw new ResponseStatusException(HttpStatus.CONFLICT,"MCP OAuth refresh failed; reconnect the service");}finally{slots.release();}
 }
 private Connection connection(McpOAuthGrant grant){Tokens tokens=decode(secrets.unseal(grant.encryptedTokens),Tokens.class);return new Connection(grant.id.toString(),tokens.accessToken(),grant.tokenExpiresAt==null?0:grant.tokenExpiresAt.getEpochSecond());}

 @Transactional public Pinned pin(UUID serverId,long version){return grants.findFirstByServerIdAndServerVersionAndStatusOrderByCreatedAtDesc(serverId,version,"AUTHORIZED").map(g->new Pinned(g.id)).orElse(null);}
 @Transactional public Status cancel(UUID project,UUID serverId,long version){require(project,serverId,version);grants.findFirstByServerIdAndServerVersionOrderByCreatedAtDesc(serverId,version).ifPresent(found->{var grant=grants.locked(found.id).orElseThrow();if(Set.of("PENDING","EXCHANGING").contains(grant.status)){grant.status="CANCELLED";grant.stateHash=null;grant.encryptedConfiguration="";grant.pendingUntil=null;}});return status(project,serverId,version);}
 @Scheduled(fixedDelay=60000) @Transactional public void expirePending(){grants.expire(Instant.now());}
 private AgentResource require(UUID project,UUID serverId,long version){scopes.requireProject(project);return resources.findById(serverId).filter(v->v.projectId.equals(project)&&v.kind.equals("MCP")).filter(v->v.version==version).orElseThrow(()->new ResponseStatusException(HttpStatus.CONFLICT,"MCP 服务配置已变化，请重新加载"));}
 private StoredConfig readStored(String body){try{return json.readValue(body,StoredConfig.class);}catch(Exception e){throw new IllegalStateException("MCP 配置不可用");}}
 private ServerConfig readOAuth(String body){return readStored(body).oauth();}
 private String writeStored(StoredConfig body){try{return json.writeValueAsString(body);}catch(Exception e){throw new IllegalStateException("MCP 配置不可用");}}
 private String encode(Object value){try{return json.writeValueAsString(value);}catch(Exception e){throw new IllegalStateException("MCP OAuth state is unavailable");}}
 private <T>T decode(String value,Class<T> type){try{return json.readValue(value,type);}catch(Exception e){throw new IllegalStateException("MCP OAuth state is unavailable");}}
 private static Instant expiry(JsonNode tokens){long seconds=tokens.path("expires_in").asLong(0);return seconds<=0?null:Instant.now().plusSeconds(seconds);}
 private static String text(JsonNode object,String field,int max){String value=object.path(field).asText("");if(value.isBlank()||value.length()>max||value.chars().anyMatch(c->c<33||c>126))throw new IllegalArgumentException();return value;}
 private static String sha256(String text){try{return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(text.getBytes(StandardCharsets.UTF_8)));}catch(Exception e){throw new IllegalStateException(e);}}
 private static String validateRedirect(String value){try{URI uri=URI.create(value);if(!Set.of("http","https").contains(uri.getScheme())||uri.getHost()==null||uri.getUserInfo()!=null||uri.getFragment()!=null||uri.getQuery()!=null||uri.getPath().isBlank()||uri.getScheme().equals("http")&&!Set.of("localhost","127.0.0.1","::1").contains(uri.getHost()))throw new IllegalArgumentException();return uri.toString();}catch(Exception e){throw new IllegalStateException("proofcode.mcp.oauth-redirect-uri must be a fixed HTTPS callback URL");}}
}
