package dev.proofcode.control.provider;

import com.fasterxml.jackson.databind.*;
import dev.proofcode.control.account.AccountService;
import dev.proofcode.control.data.DataSecrets;
import dev.proofcode.control.task.*;
import java.net.URI;
import java.net.http.*;
import java.net.InetAddress;
import java.time.*;
import java.util.*;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.http.HttpStatus;
import org.springframework.web.server.ResponseStatusException;

@Service
public class ProviderService {
    private final ModelProviderRepository providers; private final DataSecrets secrets; private final ObjectMapper json;
    private final boolean allowPrivateHosts;
    private final HttpClient client=HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(8)).followRedirects(HttpClient.Redirect.NEVER).build();
    public ProviderService(ModelProviderRepository providers,DataSecrets secrets,ObjectMapper json,@Value("${proofcode.providers.allow-private-hosts:false}")boolean allowPrivateHosts){this.providers=providers;this.secrets=secrets;this.json=json;this.allowPrivateHosts=allowPrivateHosts;}
    public List<View> list(){return providers.findByOwnerIdOrderByCreatedAtAsc(AccountService.requireId()).stream().map(this::view).toList();}
    public ModelProvider owned(UUID id){var p=providers.findById(id).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));if(!p.ownerId.equals(AccountService.requireId()))throw new ResponseStatusException(HttpStatus.NOT_FOUND);return p;}
    @Transactional public View save(UUID id,Input in){
        var p=id==null?new ModelProvider(AccountService.requireId()):owned(id);
        if(in.name()==null||in.name().isBlank()||in.name().length()>120)throw new IllegalArgumentException("服务名称需为 1–120 字符");
        String url=validateUrl(in.baseUrl());
        if(in.wireApi()==null||!Set.of("responses","chat").contains(in.wireApi()))throw new IllegalArgumentException("协议仅支持 responses 或 chat");
        if(in.apiKey()!=null&&!in.apiKey().isEmpty()){if(in.apiKey().length()>4096||in.apiKey().contains("\n")||in.apiKey().contains("\r"))throw new IllegalArgumentException("API Key 格式无效");p.encryptedKey=secrets.seal(in.apiKey());}
        if(p.encryptedKey==null)throw new IllegalArgumentException("请输入 API Key");
        // Retaining a key while changing its destination could send it to an unintended service.
        if(p.baseUrl!=null&&!p.baseUrl.equals(url)&&(in.apiKey()==null||in.apiKey().isEmpty()))throw new IllegalArgumentException("更换服务地址时请重新输入 API Key");
        if(!Objects.equals(p.baseUrl,url)||!Objects.equals(p.wireApi,in.wireApi())){p.modelsJson="[]";p.catalogAt=null;}
        p.name=in.name().trim();p.baseUrl=url;p.wireApi=in.wireApi();
        if(in.aliases()!=null){if(in.aliases().size()>1000)throw new IllegalArgumentException("显示名称过多");for(var e:in.aliases().entrySet())if(e.getKey().length()>200||e.getValue()==null||e.getValue().length()>120)throw new IllegalArgumentException("模型显示名称过长");p.aliasesJson=encode(in.aliases());}
        if(in.modelSettings()!=null)p.settingsJson=encode(ModelSettings.validate(in.modelSettings()));
        return view(providers.saveAndFlush(p));
    }
    @Transactional public View refresh(UUID id){
        var p=owned(id);
        try{
            validateUrl(p.baseUrl);
            HttpRequest req=HttpRequest.newBuilder(URI.create(p.baseUrl+"/models")).timeout(Duration.ofSeconds(12)).header("Authorization","Bearer "+secrets.unseal(p.encryptedKey)).header("Accept","application/json").GET().build();
            var response=client.send(req,HttpResponse.BodyHandlers.ofInputStream());
            byte[] body;try(var stream=response.body()){body=stream.readNBytes((1<<20)+1);}
            if(response.statusCode()<200||response.statusCode()>=300)throw new ResponseStatusException(HttpStatus.BAD_GATEWAY,"模型列表读取失败，服务返回 HTTP "+response.statusCode());
            if(body.length>1<<20)throw new IllegalArgumentException("模型列表超过 1 MiB");
            JsonNode node=json.readTree(body);if(node==null||!node.path("data").isArray())throw new IllegalArgumentException("服务未返回 OpenAI 格式的 data 模型列表");
            Set<String> ids=new TreeSet<>();for(var item:node.get("data")){String model=item.path("id").asText("");if(!model.isBlank()&&model.length()<=200&&!model.chars().anyMatch(Character::isISOControl))ids.add(model);if(ids.size()>1000)throw new IllegalArgumentException("模型列表超过 1000 个");}
            if(ids.isEmpty())throw new IllegalArgumentException("服务没有返回可用模型，请确认地址和权限");
            p.modelsJson=encode(ids);p.catalogAt=Instant.now();return view(providers.saveAndFlush(p));
        }catch(ResponseStatusException|IllegalArgumentException e){throw e;}catch(InterruptedException e){Thread.currentThread().interrupt();throw new ResponseStatusException(HttpStatus.BAD_GATEWAY,"模型列表请求被中断");}catch(Exception e){throw new ResponseStatusException(HttpStatus.BAD_GATEWAY,"无法读取模型列表，请检查 API 地址、密钥及网络");}
    }
    @Transactional public void delete(UUID id){providers.delete(owned(id));}
    public void bind(TaskEntity task,UUID id){
        if(AccountService.currentId()==null&&id==null)return; // Existing fixture/runner default configuration.
        if(id==null)throw new IllegalArgumentException("请先配置模型服务并选择模型");
        var p=owned(id);
        validateUrl(p.baseUrl);
        if(!models(p).contains(task.getModel()))throw new IllegalArgumentException("模型不在该服务的可用列表中，请刷新模型列表");
        task.bindProvider(id,secrets.seal(encode(new Connection(p.baseUrl,secrets.unseal(p.encryptedKey),p.wireApi,settings(p).getOrDefault(task.getModel(),ModelSettings.defaults())))));
    }
    public Connection resolve(TaskEntity task,UUID runner,int attempt){
        if(task.getAttempt()!=attempt||!Objects.equals(task.getRunnerId(),runner)||task.getLeaseUntil()==null||!task.getLeaseUntil().isAfter(Instant.now())||!(task.getStatus()==TaskStatus.RUNNING||task.getStatus()==TaskStatus.VERIFYING))throw new ResponseStatusException(HttpStatus.CONFLICT,"active task lease required");
        if(task.getProviderSnapshot()==null)throw new ResponseStatusException(HttpStatus.NOT_FOUND,"task uses runner configuration");
        try{var connection=json.readValue(secrets.unseal(task.getProviderSnapshot()),Connection.class);validateUrl(connection.baseUrl());return connection;}catch(Exception e){throw new ResponseStatusException(HttpStatus.SERVICE_UNAVAILABLE,"模型服务凭据不可用");}
    }
    public View view(ModelProvider p){try{Map<String,String> aliases=json.readValue(p.aliasesJson,new com.fasterxml.jackson.core.type.TypeReference<Map<String,String>>(){});return new View(p.id,p.name,p.baseUrl,p.wireApi,true,models(p).stream().map(m->new Model(m,aliases.getOrDefault(m,"").isBlank()?m:aliases.get(m).trim())).toList(),aliases,p.catalogAt,p.version,settings(p));}catch(Exception e){throw new IllegalStateException("provider metadata invalid");}}
    private Map<String,ModelSettings> settings(ModelProvider p){try{return json.readValue(p.settingsJson,new com.fasterxml.jackson.core.type.TypeReference<Map<String,ModelSettings>>(){});}catch(Exception e){throw new IllegalStateException("model settings invalid");}}
    private List<String> models(ModelProvider p){try{return json.readValue(p.modelsJson,new com.fasterxml.jackson.core.type.TypeReference<List<String>>(){});}catch(Exception e){throw new IllegalStateException("model catalog invalid");}}
    private String encode(Object value){try{return json.writeValueAsString(value);}catch(Exception e){throw new IllegalArgumentException("配置格式无效");}}
    String validateUrl(String url){try{if(url==null||url.length()>2048)throw new Exception();var u=URI.create(url.trim().replaceAll("/+$",""));String host=u.getHost();if(!Set.of("http","https").contains(u.getScheme())||host==null||u.getUserInfo()!=null||u.getRawQuery()!=null||u.getFragment()!=null||host.equalsIgnoreCase("metadata.google.internal")||u.getPath().contains("..")||u.getPort()>65535||u.getPort()==0)throw new Exception();if(!allowPrivateHosts)for(InetAddress address:InetAddress.getAllByName(host))if(!publicAddress(address))throw new Exception();return u.toString();}catch(Exception e){throw new IllegalArgumentException("API 地址需为公共 http(s) 服务，如 https://api.example.com/v1；内网服务需由管理员单独启用");}}
    private static boolean publicAddress(InetAddress address){
        if(address.isAnyLocalAddress()||address.isLoopbackAddress()||address.isLinkLocalAddress()||address.isSiteLocalAddress()||address.isMulticastAddress())return false;
        byte[] bytes=address.getAddress();int first=Byte.toUnsignedInt(bytes[0]);
        if(bytes.length==16)return (first&0xfe)!=0xfc; // IPv6 unique local addresses are not site-local in Java.
        int second=Byte.toUnsignedInt(bytes[1]);
        return first!=0&&first<224&&!(first==100&&second>=64&&second<=127)&&!(first==198&&(second==18||second==19));
    }
    public record Input(String name,String baseUrl,String apiKey,String wireApi,Map<String,String> aliases,Map<String,ModelSettings> modelSettings){}
    public record View(UUID id,String name,String baseUrl,String wireApi,boolean hasKey,List<Model> models,Map<String,String> aliases,Instant catalogAt,long version,Map<String,ModelSettings> modelSettings){}
    public record Model(String id,String name){}
    public record Connection(String baseUrl,String apiKey,String wireApi,ModelSettings settings){}
}
