package dev.proofcode.control.data;

import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.node.*;
import dev.proofcode.control.project.ProjectRepository;
import dev.proofcode.control.task.*;
import java.time.*;
import java.security.SecureRandom;
import java.util.*;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.support.TransactionTemplate;

/** A durable approval ledger. Lock order is task/session -> operation -> resource. */
@Service
public class DataOperationService {
    private final DataResourceRepository resources;
    private final DataOperationRepository operations;
    private final DataSchemaRepository schemas;
    private final DataAuditRepository audits;
    private final DataSessionRepository sessions;
    private final TaskRepository tasks;
    private final ProjectRepository projects;
    private final ObjectMapper json;
    private final PostgresDataAdapter postgres;
    private final RedisDataAdapter redis;
    private final DataSecrets secrets;
    private final TransactionTemplate tx;
    public DataOperationService(DataResourceRepository resources,DataOperationRepository operations,DataSchemaRepository schemas,DataAuditRepository audits,DataSessionRepository sessions,TaskRepository tasks,ProjectRepository projects,ObjectMapper json,PostgresDataAdapter postgres,RedisDataAdapter redis,DataSecrets secrets,PlatformTransactionManager manager) {
        this.resources=resources;this.operations=operations;this.schemas=schemas;this.audits=audits;this.sessions=sessions;this.tasks=tasks;this.projects=projects;this.json=json;this.postgres=postgres;this.redis=redis;this.secrets=secrets;
        tx=new TransactionTemplate(manager);tx.setPropagationBehavior(org.springframework.transaction.TransactionDefinition.PROPAGATION_REQUIRES_NEW);
    }
    private DataAdapter adapter(DataResource r){return switch(r.provider){case "postgres"->postgres;case "redis"->redis;default->throw DataPolicy.bad("unsupported provider");};}
    private void project(UUID project){if(!projects.existsById(project))throw DataPolicy.forbidden();}
    @Transactional public DataResource register(UUID project,JsonNode request) {
        project(project);DataPolicy.fields(request,"name","provider","environment","secretRef","allowedSchema");
        String provider=DataPolicy.required(request,"provider"),env=DataPolicy.required(request,"environment"),secret=DataPolicy.required(request,"secretRef");
        configuration(provider,env,secret);String schemas=allowedSchemas(provider,request.get("allowedSchema"));
        DataResource r=new DataResource(project,provider,env,secret,schemas);
        if(request.has("name"))r.name=name(request,"name");return resources.save(r);
    }
    @Transactional public DataResource updateResource(UUID project,UUID id,JsonNode request) {
        DataPolicy.fields(request,"expectedVersion","name","active","environment","secretRef","allowedSchema");
        DataResource r=scoped(id,project,true);long expected=version(request,"expectedVersion");if(expected!=r.resourceVersion)throw DataPolicy.conflict("resource version changed");
        if(request.has("name"))r.name=name(request,"name");
        if(request.has("active")){if(!request.get("active").isBoolean())throw DataPolicy.bad("active must be boolean");r.active=request.get("active").booleanValue();}
        if(request.has("environment"))r.environment=DataPolicy.required(request,"environment");
        if(request.has("secretRef"))r.secretRef=DataPolicy.required(request,"secretRef");
        if(request.has("allowedSchema"))r.allowedSchemas=allowedSchemas(r.provider,request.get("allowedSchema"));
        configuration(r.provider,r.environment,r.secretRef);r.resourceVersion=Math.addExact(r.resourceVersion,1);return resources.saveAndFlush(r);
    }
    private void configuration(String provider,String environment,String secret){if(!Set.of("postgres","redis").contains(provider)||!Set.of("dev","staging","production").contains(environment)||!secret.matches("[A-Z][A-Z0-9_]{0,79}"))throw DataPolicy.bad("invalid resource configuration");}
    private String allowedSchemas(String provider,JsonNode value){
        ArrayNode result=json.createArrayNode();
        Set<String> seen=new LinkedHashSet<>();
        if(value==null||value.isNull()){if(provider.equals("postgres"))result.add("public");}
        else if(value.isTextual()){String schema=value.asText().trim();if(!schema.isEmpty())DataPolicy.identifier(schema);if(!schema.isEmpty())result.add(schema);}
        else if(value.isArray()){if(value.size()>32)throw DataPolicy.bad("too many allowed schemas");for(JsonNode item:value){if(!item.isTextual())throw DataPolicy.bad("allowedSchema must contain strings");String schema=item.asText().trim();DataPolicy.identifier(schema);if(seen.add(schema))result.add(schema);}}
        else throw DataPolicy.bad("allowedSchema must be a string or string array");
        if(provider.equals("postgres")&&result.isEmpty())throw DataPolicy.bad("postgres requires at least one allowed schema");
        return result.toString();
    }
    public List<DataResource> resources(UUID project){project(project);return resources.findByProjectId(project);}
    public List<DataResource> resourcesForTask(UUID task,int attempt){return resources(current(task,attempt,false).getProjectId()).stream().filter(r->r.active).toList();}
    public void requireTaskProject(UUID project,UUID task,int attempt){if(!current(task,attempt,false).getProjectId().equals(project))throw DataPolicy.forbidden();}
    private TaskEntity current(UUID id,int attempt,boolean lock){TaskEntity t=(lock?tasks.lockById(id):tasks.findById(id)).orElseThrow(DataPolicy::forbidden);if(t.getAttempt()!=attempt||terminal(t))throw DataPolicy.conflict("task attempt is expired or terminal");return t;}
    private boolean terminal(TaskEntity t){return Set.of(TaskStatus.SUCCEEDED,TaskStatus.FAILED,TaskStatus.CANCELLED).contains(t.getStatus());}
    private DataResource scoped(UUID id,UUID project,boolean lock){DataResource r=(lock?resources.lockById(id):resources.findById(id)).orElseThrow(DataPolicy::forbidden);if(!r.projectId.equals(project))throw DataPolicy.forbidden();return r;}
    private DataResource active(UUID id,UUID project,boolean lock){DataResource r=scoped(id,project,lock);if(!r.active)throw DataPolicy.conflict("resource is disabled");return r;}

    @Transactional public DataSession createSession(UUID project,JsonNode input){project(project);DataPolicy.fields(input,"name","purpose","expiresInMinutes");String purpose=input.path("purpose").asText("MANAGEMENT");if(!Set.of("MANAGEMENT","RECOVERY").contains(purpose))throw DataPolicy.bad("invalid data session purpose");return sessions.save(new DataSession(project,name(input,"name"),purpose,DataPolicy.integer(input,"expiresInMinutes",5,1440,120)));}
    public List<DataSession> sessions(UUID project){project(project);return sessions.findByProjectIdOrderByCreatedAtDesc(project);}
    public DataSession getSession(UUID project,UUID id){return session(project,id,false,false);}
    @Transactional public DataSession closeSession(UUID project,UUID id){DataSession session=session(project,id,true,false);session.status="CLOSED";return session;}
    private DataSession session(UUID project,UUID id,boolean lock,boolean requireActive){DataSession s=(lock?sessions.lockById(id):sessions.findById(id)).orElseThrow(DataPolicy::forbidden);if(!s.projectId.equals(project))throw DataPolicy.forbidden();if(requireActive&&(!s.status.equals("ACTIVE")||!s.expiresAt.isAfter(Instant.now())))throw DataPolicy.conflict("data session is closed or expired");return s;}
    public Map<String,Object> inspectSchema(UUID task,int attempt,UUID resource){TaskEntity t=current(task,attempt,false);return inspect(active(resource,t.getProjectId(),false));}
    public Map<String,Object> inspectSessionSchema(UUID project,UUID session,UUID resource){session(project,session,false,true);return inspect(active(resource,project,false));}
    private Map<String,Object> inspect(DataResource r){JsonNode metadata=adapter(r).schema(r);String canonical=canonical(metadata);String version=DataPolicy.hash(canonical);DataSchemaSnapshot snap=tx.execute(status->schemas.save(new DataSchemaSnapshot(r,version,canonical)));return Map.of("snapshotId",snap.id,"schemaVersion",version,"resourceVersion",r.resourceVersion,"policyVersion",r.policyVersion,"metadata",metadata);}
    private JsonNode schemaFor(DataResource r,String version){if(r.provider.equals("redis"))return adapter(r).schema(r);DataSchemaSnapshot snapshot=schemas.findFirstByConnectionIdAndSchemaVersionOrderByCreatedAtDesc(r.id,version).orElseThrow(()->DataPolicy.bad("schemaVersion has no project snapshot"));if(!snapshot.projectId.equals(r.projectId))throw DataPolicy.forbidden();return parse(snapshot.metadata);}
    @Transactional public DataOperation plan(UUID task,JsonNode request){DataPolicy.fields(request,"resourceId","attempt","resourceVersion","schemaVersion","ir","idempotencyKey");int attempt=DataPolicy.integer(request,"attempt",1,Integer.MAX_VALUE,-1);TaskEntity t=current(task,attempt,true);return build(t.getProjectId(),task,attempt,null,request);}
    @Transactional public DataOperation planInSession(UUID project,UUID session,JsonNode request){DataPolicy.fields(request,"resourceId","resourceVersion","schemaVersion","ir","idempotencyKey");session(project,session,true,true);return build(project,null,0,session,request);}
    private DataOperation build(UUID project,UUID task,int attempt,UUID session,JsonNode request){
        DataResource r=active(uuid(request,"resourceId"),project,true);if(request.has("resourceVersion")&&version(request,"resourceVersion")!=r.resourceVersion)throw DataPolicy.conflict("resource changed after preview");
        JsonNode ir=request.get("ir");if(ir==null||!ir.isObject()||ir.toString().length()>131072)throw DataPolicy.bad("bounded IR object required");
        String schemaVersion=r.provider.equals("redis")?DataPolicy.hash(canonical(adapter(r).schema(r))):DataPolicy.required(request,"schemaVersion");
        if(request.has("schemaVersion")&&!schemaVersion.equals(request.get("schemaVersion").asText()))throw DataPolicy.conflict("cache schema version differs");
        String key=idempotency(request),canonical=canonical(ir);DataAdapter.Compiled compiled=adapter(r).compile(r,schemaFor(r,schemaVersion),ir);
        String digest=digest(task,project,attempt,session,r,canonical,schemaVersion);
        Optional<DataOperation> old=session==null?operations.findByTaskIdAndAttemptAndIdempotencyKey(task,attempt,key):operations.findByDataSessionIdAndIdempotencyKey(session,key);
        if(old.isPresent()){if(!old.get().digest.equals(digest))throw DataPolicy.conflict("idempotencyKey belongs to different IR");return old.get();}
        DataOperation op=new DataOperation(task,project,attempt,r,canonical,digest,schemaVersion,compiled.preview(),key,compiled.kind());op.dataSessionId=session;operations.save(op);audit(op,"PLAN_CREATED",Map.of("digest",digest,"resourceVersion",r.resourceVersion));return op;
    }
    public List<DataOperation> plans(UUID project){project(project);return operations.findByProjectIdOrderByCreatedAtDesc(project);}
    public List<DataAudit> audit(UUID project){project(project);return audits.findByProjectIdOrderByCreatedAtDesc(project);}
    public DataOperation getForProject(UUID project,UUID plan){DataOperation op=operations.findById(plan).orElseThrow(DataPolicy::forbidden);if(!op.projectId.equals(project))throw DataPolicy.forbidden();return op;}
    public DataOperation getForTask(UUID task,int attempt,UUID plan){DataOperation op=operations.findById(plan).orElseThrow(DataPolicy::forbidden);if(!Objects.equals(op.taskId,task))throw DataPolicy.forbidden();current(task,attempt,false);validate(op,attempt,op.digest,false);return op;}
    /** Called only by the authenticated user path in TaskService, after its task lock. */
    @Transactional public void approveFromTool(UUID taskId,int attempt,String toolName,JsonNode arguments,boolean approved){if(!"data_execute".equals(toolName))return;DataPolicy.fields(arguments,"planId","digest");current(taskId,attempt,true);DataOperation op=operations.lockById(uuid(arguments,"planId")).orElseThrow(DataPolicy::forbidden);if(!Objects.equals(op.taskId,taskId))throw DataPolicy.forbidden();validate(op,attempt,DataPolicy.required(arguments,"digest"),false);decide(op,approved,false);}
    @Transactional public void approveFromTool(UUID taskId,int attempt,String toolName,String arguments,boolean approved){approveFromTool(taskId,attempt,toolName,parse(arguments),approved);}
    @Transactional public ApprovalResult approve(UUID project,UUID plan,String digest,boolean approved){DataOperation read=getForProject(project,plan);lockContext(read,read.attempt);DataOperation op=operations.lockById(plan).orElseThrow(DataPolicy::forbidden);validate(op,op.attempt,digest,true);String capability=decide(op,approved,true);return new ApprovalResult(op.id,op.status,op.digest,capability,op.capabilityExpiresAt);}
    private void lockContext(DataOperation op,int attempt){if(op.dataSessionId!=null){if(attempt!=0)throw DataPolicy.conflict("session plans have no task attempt");session(op.projectId,op.dataSessionId,true,true);}else current(op.taskId,attempt,true);}
    private String decide(DataOperation op,boolean approved,boolean explicit){if(!op.status.equals("PENDING")){if(!explicit&&op.status.equals(approved?"APPROVED":"DENIED"))return null;throw DataPolicy.conflict("plan has already been decided");}op.status=approved?"APPROVED":"DENIED";op.updatedAt=Instant.now();String cap=null;if(approved){op.capabilityExpiresAt=Instant.now().plusSeconds(300);if(explicit){cap=token();op.capabilityHash=DataPolicy.hash(cap);}}audit(op,approved?"USER_APPROVED":"USER_DENIED",Map.of("digest",op.digest));return cap;}
    public record ApprovalResult(UUID planId,String status,String digest,String capability,Instant expiresAt){}
    public record ExecutionResult(UUID planId,String status,Map<String,Object> result){}
    public ExecutionResult executeApproved(UUID task,int attempt,UUID plan,String digest){return execute(task,null,attempt,plan,digest,null,true);}
    public ExecutionResult executeWithCapability(UUID project,int attempt,UUID plan,String digest,String cap){return execute(null,project,attempt,plan,digest,cap,false);}
    private record Started(DataOperation operation,DataResource resource,boolean repeated){}
    private ExecutionResult execute(UUID task,UUID project,int attempt,UUID plan,String digest,String capability,boolean internal){
        Started started=tx.execute(status->{DataOperation read=operations.findById(plan).orElseThrow(DataPolicy::forbidden);if(task!=null&&!Objects.equals(read.taskId,task)||project!=null&&!read.projectId.equals(project))throw DataPolicy.forbidden();lockContext(read,attempt);DataOperation op=operations.lockById(plan).orElseThrow(DataPolicy::forbidden);DataResource r=validate(op,attempt,digest,true);
            if(Set.of("COMMITTED","SUCCEEDED","FAILED_ROLLED_BACK","FAILED_PRECONDITION","COMPENSATED","COMPENSATION_CONFLICT").contains(op.status))return new Started(op,r,true);
            if(!op.status.equals("APPROVED"))throw DataPolicy.conflict("operation is unapproved, in flight, or has an unknown outcome");if(op.capabilityExpiresAt==null||!op.capabilityExpiresAt.isAfter(Instant.now()))throw DataPolicy.conflict("approval capability expired");
            String effective=capability;if(internal){effective=token();op.capabilityHash=DataPolicy.hash(effective);}if(effective==null||op.capabilityHash==null||!java.security.MessageDigest.isEqual(op.capabilityHash.getBytes(java.nio.charset.StandardCharsets.UTF_8),DataPolicy.hash(effective).getBytes(java.nio.charset.StandardCharsets.UTF_8)))throw DataPolicy.forbidden();if(op.capabilityUsedAt!=null)throw DataPolicy.conflict("capability already consumed");op.capabilityUsedAt=Instant.now();op.status="EXECUTING";op.updatedAt=Instant.now();audit(op,"EXECUTION_STARTED",Map.of("digest",op.digest));operations.flush();audits.flush();return new Started(op,r,false);});
        DataOperation op=started.operation();DataResource r=started.resource();if(started.repeated())return new ExecutionResult(op.id,op.status,op.resultSummary==null?Map.of():json.convertValue(parse(op.resultSummary),Map.class));
        DataAdapter.Outcome result=secrets.withPinned(r.secretRef,()->perform(op,r));return finish(op.id,result);
    }
    private DataAdapter.Outcome perform(DataOperation op,DataResource r){
        DataAdapter a=adapter(r);
        if(op.kind.equals("compensation")){try{if(op.privateSnapshot==null||!DataPolicy.hash(op.privateSnapshot).equals(parse(op.canonicalIr).path("snapshotHash").asText()))return new DataAdapter.Outcome("FAILED_PRECONDITION",Map.of("errorCode","COMPENSATION_SNAPSHOT_MISMATCH"),null);return a.compensate(r,op.privateSnapshot);}catch(Exception e){return new DataAdapter.Outcome("COMPENSATION_UNKNOWN",Map.of("errorCode","UNCONFIRMED_COMPENSATION_RESULT"),null);}}
        DataAdapter.Compiled compiled;String snapshot;
        try{JsonNode schema=schemaFor(r,op.schemaVersion);if(r.provider.equals("postgres")&&!op.schemaVersion.equals(DataPolicy.hash(canonical(a.schema(r)))))throw DataPolicy.conflict("database schema changed after preview");compiled=a.compile(r,schema,parse(op.canonicalIr));snapshot=a.prepare(r,compiled);if(snapshot!=null)persistSnapshot(op.id,snapshot);}
        catch(Exception e){return new DataAdapter.Outcome("FAILED_PRECONDITION",Map.of("errorCode","EXECUTION_PRECONDITION_FAILED"),null);}
        try{return r.provider.equals("postgres")?a.execute(r,compiled,snapshot,value->persistSnapshot(op.id,value)):a.execute(r,compiled,snapshot);}catch(Exception e){return new DataAdapter.Outcome("COMMIT_UNKNOWN",Map.of("errorCode","UNCONFIRMED_EXTERNAL_RESULT"),snapshot);}
    }
    private void persistSnapshot(UUID id,String snapshot){tx.executeWithoutResult(s->{DataOperation locked=operations.lockById(id).orElseThrow();locked.privateSnapshot=snapshot;audit(locked,"SNAPSHOT_PREPARED",Map.of("snapshotHash",DataPolicy.hash(snapshot)));operations.flush();audits.flush();});}
    private DataResource validate(DataOperation op,int attempt,String given,boolean lock){if(op.attempt!=attempt)throw DataPolicy.forbidden();if(op.dataSessionId!=null)session(op.projectId,op.dataSessionId,false,true);else {TaskEntity t=current(op.taskId,attempt,false);if(!op.projectId.equals(t.getProjectId()))throw DataPolicy.forbidden();}DataResource r=active(op.connectionId,op.projectId,lock);if(op.resourceVersion!=r.resourceVersion||!op.policyVersion.equals(r.policyVersion))throw DataPolicy.conflict("resource or policy version changed; create a new plan");String actual=digest(op.taskId,op.projectId,op.attempt,op.dataSessionId,r,op.canonicalIr,op.schemaVersion);if(!op.digest.equals(actual)||!actual.equals(given))throw DataPolicy.conflict("immutable operation digest mismatch");return r;}
    private ExecutionResult finish(UUID id,DataAdapter.Outcome result){if(result==null)result=new DataAdapter.Outcome("COMMIT_UNKNOWN",Map.of("errorCode","UNCONFIRMED_EXTERNAL_RESULT"),null);final DataAdapter.Outcome outcome=result;tx.executeWithoutResult(s->{DataOperation op=operations.lockById(id).orElseThrow();op.status=outcome.status();op.updatedAt=Instant.now();if(outcome.privateSnapshot()!=null)op.privateSnapshot=outcome.privateSnapshot();Map<String,Object> summary=new LinkedHashMap<>();for(String k:List.of("affectedRows","affectedKeys","returnedRows","truncated","errorCode","beforeHash","beforeTtlMs","afterHash","schemaChanged","recoveryAvailable"))if(outcome.result().containsKey(k))summary.put(k,outcome.result().get(k));op.resultSummary=string(summary);audit(op,"EXECUTION_FINISHED",summary);});return new ExecutionResult(id,outcome.status(),outcome.result());}
    /** Committed changes are reversed with a new approval, including after a task has ended. */
    @Transactional public DataOperation compensationPlan(UUID project,UUID original,String idempotencyKey){
        if(idempotencyKey==null||idempotencyKey.isBlank()||idempotencyKey.length()>200)throw DataPolicy.bad("bounded idempotencyKey is required");
        DataOperation read=getForProject(project,original);
        // Recovery may outlive its programming task or management session. Lock even a
        // terminal context, without reactivating it, before the original operation. This
        // serializes simultaneous retries and preserves the normal context -> op -> resource order.
        TaskEntity originalTask=null;
        if(read.taskId!=null)originalTask=tasks.lockById(read.taskId).orElseThrow(DataPolicy::forbidden);
        else session(project,read.dataSessionId,true,false);
        DataOperation old=operations.lockById(original).orElseThrow(DataPolicy::forbidden);
        if(!old.projectId.equals(project))throw DataPolicy.forbidden();
        DataResource r=active(old.connectionId,project,true);
        if(!old.status.equals("COMMITTED")||old.privateSnapshot==null||old.kind.equals("migration"))throw DataPolicy.conflict("only confirmed reversible row/cache writes have a recovery snapshot");
        if(r.resourceVersion!=old.resourceVersion||!r.policyVersion.equals(old.policyVersion))throw DataPolicy.conflict("original resource configuration changed; operator reconciliation is required");
        String ir=canonical(json.createObjectNode().put("kind","compensation").put("originalPlanId",old.id.toString()).put("snapshotHash",DataPolicy.hash(old.privateSnapshot)));
        // Existing recovery plans make retries idempotent even when their task has become terminal.
        for(DataOperation candidate:operations.findByProjectIdOrderByCreatedAtDesc(project)){if(candidate.idempotencyKey.equals(idempotencyKey)&&candidate.kind.equals("compensation")&&parse(candidate.canonicalIr).path("originalPlanId").asText().equals(original.toString()))return candidate;}
        UUID task=null,session=null;int attempt=0;
        if(originalTask!=null&&!terminal(originalTask)&&originalTask.getAttempt()==old.attempt){task=originalTask.getId();attempt=old.attempt;}
        if(task==null){DataSession recovery=sessions.save(new DataSession(project,"Recovery "+old.id.toString().substring(0,8),"RECOVERY",120));session=recovery.id;}
        String digest=digest(task,project,attempt,session,r,ir,old.schemaVersion);DataOperation op=new DataOperation(task,project,attempt,r,ir,digest,old.schemaVersion,"Conditional recovery of "+old.id,idempotencyKey,"compensation");op.dataSessionId=session;op.privateSnapshot=old.privateSnapshot;operations.save(op);audit(op,"COMPENSATION_PLANNED",Map.of("originalPlanId",old.id));return op;
    }
    public Map<String,Object> explain(UUID task,int attempt,UUID plan,String digest){DataOperation op=getForTask(task,attempt,plan);return explainOp(op,digest);}
    public Map<String,Object> explainForProject(UUID project,UUID plan,String digest){DataOperation op=getForProject(project,plan);return explainOp(op,digest);}
    private Map<String,Object> explainOp(DataOperation op,String digest){DataResource r=validate(op,op.attempt,digest,false);if(!r.provider.equals("postgres"))throw DataPolicy.bad("EXPLAIN is only available for PostgreSQL read queries");return postgres.explain(r,postgres.compile(r,schemaFor(r,op.schemaVersion),parse(op.canonicalIr)));}
    private void audit(DataOperation op,String action,Map<String,?> details){audits.save(new DataAudit(op,action,string(details)));}
    private String digest(UUID task,UUID project,int attempt,UUID session,DataResource r,String ir,String schema){return DataPolicy.hash("data-ir-v2\n"+task+"\n"+project+"\n"+attempt+"\n"+session+"\n"+r.id+"\n"+r.provider+"\n"+r.environment+"\n"+r.secretRef+"\n"+r.allowedSchemas+"\n"+r.resourceVersion+"\n"+r.policyVersion+"\n"+schema+"\n"+ir);}
    private String name(JsonNode n,String k){String value=DataPolicy.required(n,k).trim();if(value.length()>120||value.chars().anyMatch(ch->ch<32))throw DataPolicy.bad("name must be at most 120 printable characters");return value;}
    private long version(JsonNode n,String k){JsonNode v=n.get(k);if(v==null||!v.isIntegralNumber()||!v.canConvertToLong()||v.longValue()<0)throw DataPolicy.bad(k+" is required as a nonnegative integer");return v.longValue();}
    private String idempotency(JsonNode request){String key=DataPolicy.required(request,"idempotencyKey");if(key.length()>200)throw DataPolicy.bad("idempotencyKey is too long");return key;}
    private String token(){byte[] b=new byte[32];new SecureRandom().nextBytes(b);return Base64.getUrlEncoder().withoutPadding().encodeToString(b);}
    private UUID uuid(JsonNode n,String k){try{return UUID.fromString(DataPolicy.required(n,k));}catch(IllegalArgumentException e){throw DataPolicy.bad("invalid "+k);}}
    private JsonNode parse(String s){try{return json.readTree(s);}catch(Exception e){throw DataPolicy.bad("invalid JSON");}}
    private String string(Object o){try{return json.writeValueAsString(o);}catch(Exception e){throw new IllegalStateException(e);}}
    private String canonical(JsonNode node){return DataPolicy.canonical(node);}
}
