package dev.proofcode.control.agentplatform;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.data.DataSecrets;
import dev.proofcode.control.session.WorkspaceService;
import java.net.*;
import java.util.*;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;

@Service
public class McpServerService {
 private final AgentResourceRepository resources; private final WorkspaceService scopes; private final DataSecrets secrets; private final ObjectMapper json; private final boolean allowPrivate; private final McpOAuthService oauth;
 public McpServerService(AgentResourceRepository resources,WorkspaceService scopes,DataSecrets secrets,ObjectMapper json,@Value("${proofcode.mcp.allow-private-hosts:false}")boolean allowPrivate,McpOAuthService oauth){this.resources=resources;this.scopes=scopes;this.secrets=secrets;this.json=json;this.allowPrivate=allowPrivate;this.oauth=oauth;}
 public record Input(String name,String url,String bearer,boolean clearBearer,String headerName,String headerValue,boolean clearHeader,boolean enabled,long version){
  public Input(String name,String url,String bearer,boolean clearBearer,boolean enabled,long version){this(name,url,bearer,clearBearer,null,null,false,enabled,version);}
  @Override public String toString(){return "[private MCP input]";}
 }
 public record View(UUID id,String name,String url,boolean hasBearer,String headerName,boolean hasHeader,boolean enabled,long version){
  public View(UUID id,String name,String url,boolean hasBearer,boolean enabled,long version){this(id,name,url,hasBearer,null,false,enabled,version);}
 }
 private record Stored(String url,String encryptedBearer,String headerName,String encryptedHeader,boolean enabled,McpOAuthService.ServerConfig oauth){}
 // Stored only in the private task snapshot; public DTOs never expose this field.
 public record Pinned(UUID id,String name,long version,String url,String encryptedBearer,String headerName,String encryptedHeader,McpOAuthService.Pinned oauth){
  public Pinned(UUID id,String name,long version,String url,String encryptedBearer){this(id,name,version,url,encryptedBearer,null,null,null);}
 }
 public record Connection(String name,String url,String bearer,String headerName,String headerValue,String oauthGrantId,long oauthExpiresAt){
  public Connection(String name,String url,String bearer){this(name,url,bearer,null,null,null,0);}
  @Override public String toString(){return "[private MCP connection]";}
 }
 record ProbeConfiguration(String name,String url,String bearer,String headerName,String headerValue,boolean allowPrivateHosts){
  ProbeConfiguration(String name,String url,String bearer,boolean allowPrivateHosts){this(name,url,bearer,null,null,allowPrivateHosts);}
  @Override public String toString(){return "[private MCP inspection configuration]";}
 }
 ProbeConfiguration inspectConfiguration(UUID project,UUID id,long version){
  var value=verifyVersion(project,id,version);var stored=decode(value.body,Stored.class);
  String bearer=stored.encryptedBearer()==null?"":secrets.unseal(stored.encryptedBearer());
  if(stored.oauth()!=null){var ref=oauth.pin(id,version);if(ref==null)throw conflict("MCP OAuth 尚未连接，请先完成授权");bearer=oauth.resolve(ref,project,id,version,stored.url()).accessToken();}
  return new ProbeConfiguration(value.name,stored.url(),bearer,stored.headerName(),stored.encryptedHeader()==null?"":secrets.unseal(stored.encryptedHeader()),allowPrivate);
 }
 AgentResource verifyVersion(UUID project,UUID id,long version){
  scopes.requireProject(project);
  // Scalar query bypasses an HTTP request's managed-entity cache after network I/O.
  long current=resources.currentVersion(project,id,"MCP").orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND,"MCP 服务不存在，请重新配置智能体"));
  if(current!=version)throw conflict("服务配置已变化，请重新加载后诊断");
  return require(project,id);
 }
 public List<View> list(UUID project){scopes.requireProject(project);return resources.findByProjectIdAndKindOrderByNameAsc(project,"MCP").stream().map(this::view).toList();}
 @Transactional public View save(UUID project,UUID id,Input input){
  scopes.requireProject(project);
  if(input==null||input.name()==null||!input.name().matches("[A-Za-z][A-Za-z0-9_-]{0,31}"))throw new IllegalArgumentException("服务别名需以字母开头，只能包含字母、数字、下划线或连字符，最多 32 字符");
  String url=validateUrl(input.url());
  var value=id==null?new AgentResource(project,"MCP",input.name(),"{}"):require(project,id);
  if(id!=null&&value.version!=input.version())throw conflict("服务配置已变化，请重新打开后保存");
  if(id==null&&resources.findByProjectIdAndKindOrderByNameAsc(project,"MCP").size()>=64)throw conflict("每个项目最多保存 64 个 MCP 服务");
  resources.findByProjectIdAndKindAndName(project,"MCP",input.name()).filter(other->!other.id.equals(value.id)).ifPresent(other->{throw conflict("服务别名已存在");});
  Stored old=id==null?null:decode(value.body,Stored.class);
  String encrypted=old==null?null:old.encryptedBearer();
  if(input.clearBearer())encrypted=null;
  if(input.bearer()!=null&&!input.bearer().isEmpty()){
   if(input.clearBearer()||input.bearer().length()>4096||input.bearer().chars().anyMatch(c->c<33||c>126))throw new IllegalArgumentException("访问令牌格式无效");
   encrypted=secrets.seal(input.bearer());
  }
  if(old!=null&&!old.url().equals(url)&&old.encryptedBearer()!=null&&!input.clearBearer()&&(input.bearer()==null||input.bearer().isEmpty()))throw new IllegalArgumentException("更换地址时请重新输入访问令牌，或明确清除令牌");
  String headerName=old==null?null:old.headerName(),encryptedHeader=old==null?null:old.encryptedHeader();
  if(input.clearHeader()){headerName=null;encryptedHeader=null;}
  String requestedName=input.headerName()==null?"":input.headerName().trim();String requestedValue=input.headerValue();
  if(!requestedName.isEmpty()){
   validateHeaderName(requestedName);
   if(requestedValue!=null&&!requestedValue.isEmpty()){
    if(input.clearHeader()||requestedValue.length()>4096||requestedValue.chars().anyMatch(c->c<32||c>126))throw new IllegalArgumentException("自定义认证头格式无效");
    headerName=requestedName;encryptedHeader=secrets.seal(requestedValue);
   }else if(!Objects.equals(headerName,requestedName))throw new IllegalArgumentException("新增或更换认证头名称时请填写认证值");
  }else if(requestedValue!=null&&!requestedValue.isEmpty())throw new IllegalArgumentException("填写认证值前请先设置认证头名称");
  if(old!=null&&!old.url().equals(url)&&old.encryptedHeader()!=null&&!input.clearHeader()&&(requestedValue==null||requestedValue.isEmpty()))throw new IllegalArgumentException("更换地址时请重新输入自定义认证值，或明确清除认证头");
  value.name=input.name();value.body=encode(new Stored(url,encrypted,headerName,encryptedHeader,input.enabled(),old!=null&&old.url().equals(url)?old.oauth():null));return view(resources.saveAndFlush(value));
 }
 @Transactional public void delete(UUID project,UUID id,long version){scopes.requireProject(project);var value=require(project,id);if(value.version!=version)throw conflict("服务配置已变化，请重新打开后删除");resources.delete(value);}
 public List<Pinned> pin(UUID project,List<UUID> ids){
  scopes.requireProject(project);if(ids==null)return List.of();
  if(ids.size()>16||ids.stream().anyMatch(Objects::isNull)||new HashSet<>(ids).size()!=ids.size())throw new IllegalArgumentException("最多选择 16 个不同的远程 MCP 服务");
  List<Pinned> result=new ArrayList<>();
  for(UUID id:ids){var value=require(project,id);var body=decode(value.body,Stored.class);if(!body.enabled())throw conflict("MCP 服务 "+value.name+" 已停用，请调整智能体配置");validateUrl(body.url());var ref=body.oauth()==null?null:oauth.pin(id,value.version);if(body.oauth()!=null&&ref==null)throw conflict("MCP 服务 "+value.name+" 尚未完成 OAuth 授权");result.add(new Pinned(value.id,value.name,value.version,body.url(),body.encryptedBearer(),body.headerName(),body.encryptedHeader(),ref));}
  return List.copyOf(result);
 }
 public List<Connection> connections(UUID project,List<Pinned> pinned){
  if(pinned==null)return List.of();return pinned.stream().map(value->{String bearer=value.encryptedBearer()==null?"":secrets.unseal(value.encryptedBearer()),grantId=null;long expires=0;if(value.oauth()!=null){var credential=oauth.resolve(value.oauth(),project,value.id(),value.version(),value.url());bearer=credential.accessToken();grantId=credential.grantId();expires=credential.expiresAtEpochSecond();}return new Connection(value.name(),validateUrl(value.url()),bearer,value.headerName(),value.encryptedHeader()==null?"":secrets.unseal(value.encryptedHeader()),grantId,expires);}).toList();
 }
 public List<Connection> connections(List<Pinned> pinned){return connections(null,pinned);}
 private AgentResource require(UUID project,UUID id){return resources.findById(id).filter(v->v.projectId.equals(project)&&v.kind.equals("MCP")).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND,"MCP 服务不存在，请重新配置智能体"));}
 private View view(AgentResource value){var body=decode(value.body,Stored.class);return new View(value.id,value.name,body.url(),body.encryptedBearer()!=null,body.headerName(),body.encryptedHeader()!=null,body.enabled(),value.version);}
 private static void validateHeaderName(String name){String lower=name.toLowerCase(Locale.ROOT);if(name.length()>128||!name.matches("[!#$%&'*+.^_`|~0-9A-Za-z-]+")||Set.of("host","content-length","connection","transfer-encoding","upgrade","proxy-authorization","authorization","content-type","accept","accept-encoding","user-agent","expect","trailer","te","cookie","origin","referer","cache-control","pragma").contains(lower)||lower.startsWith("mcp-")||lower.startsWith("proxy-")||lower.startsWith("sec-"))throw new IllegalArgumentException("自定义认证头名称无效或属于受保护传输头");}
 String validateUrl(String text){
  try{if(text==null||text.length()>2048)throw new Exception();URI uri=URI.create(text.trim());String host=uri.getHost();
   if(!Set.of("http","https").contains(uri.getScheme())||host==null||uri.getUserInfo()!=null||uri.getRawQuery()!=null||uri.getFragment()!=null||uri.getPath().contains("..")||uri.getPort()==0||uri.getPort()>65535||host.equalsIgnoreCase("metadata.google.internal"))throw new Exception();
   if(!allowPrivate)for(InetAddress ip:InetAddress.getAllByName(host)){byte[] b=ip.getAddress();int first=Byte.toUnsignedInt(b[0]);if(ip.isAnyLocalAddress()||ip.isLoopbackAddress()||ip.isLinkLocalAddress()||ip.isSiteLocalAddress()||ip.isMulticastAddress()||(b.length==16&&(first&0xfe)==0xfc)||(b.length==4&&(first==0||first>=224||(first==100&&Byte.toUnsignedInt(b[1])>=64&&Byte.toUnsignedInt(b[1])<=127)||(first==198&&(Byte.toUnsignedInt(b[1])==18||Byte.toUnsignedInt(b[1])==19)))))throw new Exception();}
   return uri.toString();
  }catch(Exception e){throw new IllegalArgumentException("请输入公共 HTTP(S) MCP 地址；内网服务需由管理员启用");}
 }
 private String encode(Object value){try{return json.writeValueAsString(value);}catch(Exception e){throw new IllegalArgumentException("MCP 配置格式无效");}}
 private <T>T decode(String value,Class<T> type){try{return json.readValue(value,type);}catch(Exception e){throw new IllegalStateException("MCP 配置不可用");}}
 private static ResponseStatusException conflict(String text){return new ResponseStatusException(HttpStatus.CONFLICT,text);}
}
