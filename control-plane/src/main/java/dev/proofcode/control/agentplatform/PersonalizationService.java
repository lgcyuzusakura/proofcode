package dev.proofcode.control.agentplatform;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.account.AccountService;
import java.util.UUID;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;

@Service
public class PersonalizationService {
 private final PersonalPreferenceRepository preferences; private final ObjectMapper json;
 public PersonalizationService(PersonalPreferenceRepository preferences,ObjectMapper json){this.preferences=preferences;this.json=json;}
 public record Values(String responseLanguage,String answerStyle,String codeStyle,String collaborationStyle){
  public Values { responseLanguage=clean(responseLanguage,80);answerStyle=clean(answerStyle,2000);codeStyle=clean(codeStyle,2000);collaborationStyle=clean(collaborationStyle,2000); }
  private static String clean(String value,int max){if(value==null)value="";value=value.trim();if(value.length()>max)throw new IllegalArgumentException("个人偏好内容过长");return value;}
  public boolean empty(){return responseLanguage.isBlank()&&answerStyle.isBlank()&&codeStyle.isBlank()&&collaborationStyle.isBlank();}
 }
 public record Input(long version,Values values){}
 public record View(long version,Values values){}
 public record Snapshot(long version,Values values){
  public boolean present(){return values!=null&&!values.empty();}
 }
 public View get(){UUID owner=AccountService.requireId();return preferences.findByOwnerId(owner).map(this::view).orElse(new View(0,new Values("","","","")));}
 @Transactional public View save(Input input){
  UUID owner=AccountService.requireId();if(input==null||input.values()==null)throw new IllegalArgumentException("个人偏好无效");
  var value=preferences.findByOwnerId(owner).orElse(null);
  if(value==null){if(input.version()!=0)throw conflict();value=new PersonalPreference(owner,encode(input.values()));}
  else{if(value.version+1!=input.version())throw conflict();value.body=encode(input.values());}
  try{return view(preferences.saveAndFlush(value));}
  catch(org.springframework.dao.DataIntegrityViolationException|org.springframework.dao.OptimisticLockingFailureException e){throw conflict();}
 }
 public Snapshot capture(){
  UUID owner=AccountService.currentId();if(owner==null)return new Snapshot(0,new Values("","","",""));
  return preferences.findByOwnerId(owner).map(value->{View view=view(value);return new Snapshot(view.version(),view.values());}).orElse(new Snapshot(0,new Values("","","","")));
 }
 // Zero denotes an absent row; the first persisted version must not share that revision.
 private View view(PersonalPreference value){return new View(value.version+1,decode(value.body));}
 private String encode(Values value){try{return json.writeValueAsString(value);}catch(Exception e){throw new IllegalStateException("personal preferences could not be encoded",e);}}
 private Values decode(String value){try{return json.readValue(value,Values.class);}catch(Exception e){throw new IllegalStateException("saved personal preferences are invalid",e);}}
 private static ResponseStatusException conflict(){return new ResponseStatusException(HttpStatus.CONFLICT,"个人偏好已被其他请求修改，请重新加载后保存");}
}
