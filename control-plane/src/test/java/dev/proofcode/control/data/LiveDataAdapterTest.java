package dev.proofcode.control.data;

import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.node.*;
import java.sql.*;
import java.time.Duration;
import java.util.*;
import java.util.function.Function;
import org.junit.jupiter.api.*;
import org.junit.jupiter.api.condition.EnabledIfEnvironmentVariable;
import org.springframework.data.redis.connection.lettuce.LettuceConnectionFactory;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.mock.env.MockEnvironment;
import org.springframework.web.server.ResponseStatusException;
import static org.junit.jupiter.api.Assertions.*;

/** Opt-in actual PostgreSQL/Redis checks, exclusively against disposable test services. */
@EnabledIfEnvironmentVariable(named="PROOFCODE_LIVE_DATA_TEST",matches="true")
class LiveDataAdapterTest {
    private final ObjectMapper json=new ObjectMapper();
    private final List<String> tables=new ArrayList<>();
    private final List<DataResource> caches=new ArrayList<>();
    private DataSecrets secrets;
    private String host;
    @BeforeEach void setup(){
        host=System.getenv().getOrDefault("PROOFCODE_LIVE_DATA_HOST","host.docker.internal");
        MockEnvironment env=new MockEnvironment()
            .withProperty("DATA_SNAPSHOT_KEY",Base64.getEncoder().encodeToString(new byte[32]))
            .withProperty("DATA_SECRET_PG","{\"url\":\"jdbc:postgresql://"+host+":15491/data_test\",\"user\":\"proofcode\",\"password\":\"data-test-only\"}")
            .withProperty("DATA_SECRET_CACHE","{\"host\":\""+host+"\",\"port\":16491}");
        secrets=new DataSecrets(env,json);
    }
    private Connection pg()throws SQLException{return DriverManager.getConnection("jdbc:postgresql://"+host+":15491/data_test","proofcode","data-test-only");}
    private String table(String prefix){String name=prefix+"_"+UUID.randomUUID().toString().replace("-","");tables.add(name);return name;}
    private void sql(String statement)throws SQLException{try(Connection c=pg();Statement s=c.createStatement()){s.execute(statement);}}
    private long count(String name)throws SQLException{try(Connection c=pg();Statement s=c.createStatement();ResultSet rs=s.executeQuery("SELECT count(*) FROM public."+name)){rs.next();return rs.getLong(1);}}
    private String status(String name,long id)throws SQLException{try(Connection c=pg();Statement s=c.createStatement();ResultSet rs=s.executeQuery("SELECT status FROM public."+name+" WHERE id="+id)){return rs.next()?rs.getString(1):null;}}
    private DataResource postgres(){return new DataResource(UUID.randomUUID(),"postgres","dev","PG","[\"public\"]");}
    private DataResource cacheResource(UUID project){DataResource r=new DataResource(project,"redis","dev","CACHE","[]");caches.add(r);return r;}
    private <T>T redis(Function<StringRedisTemplate,T> action){LettuceConnectionFactory f=new LettuceConnectionFactory(host,16491);f.afterPropertiesSet();f.start();try{StringRedisTemplate t=new StringRedisTemplate(f);t.afterPropertiesSet();return action.apply(t);}finally{f.destroy();}}
    private String physical(DataResource r,String logical){return "pc:{"+r.projectId+":"+r.id+"}:"+r.environment+":"+logical;}
    private JsonNode input(String body)throws Exception{return json.readTree(body);}
    private JsonNode query(String table){ObjectNode n=json.createObjectNode().put("kind","query").put("schema","public").put("table",table);n.putArray("select").add("id").add("status");return n;}
    private ObjectNode update(String table,long id,String value){ObjectNode n=json.createObjectNode().put("kind","mutation").put("schema","public").put("table",table).put("operation","update").put("expectedRows",1);n.putObject("values").put("status",value);n.putArray("filters").addObject().put("column","id").put("operator","eq").put("value",id);return n;}
    private DataAdapter.Compiled cache(RedisDataAdapter a,DataResource r,String body)throws Exception{return a.compile(r,a.schema(r),input(body));}
    private DataAdapter.Outcome set(RedisDataAdapter a,DataResource r,String key,String value,int ttl){ObjectNode n=json.createObjectNode().put("kind","cache").put("operation","set").put("key",key).put("value",value).put("ttlSeconds",ttl);var p=a.compile(r,a.schema(r),n);return a.execute(r,p,a.prepare(r,p));}
    private Map<String,Object> get(RedisDataAdapter a,DataResource r,String key){ObjectNode n=json.createObjectNode().put("kind","cache").put("operation","get").put("key",key);var out=a.execute(r,a.compile(r,a.schema(r),n),null);assertEquals("SUCCEEDED",out.status());return out.result();}
    @AfterEach void cleanup()throws Exception{
        if(!tables.isEmpty())try(Connection c=pg();Statement s=c.createStatement()){for(String table:tables)s.execute("DROP TABLE IF EXISTS public."+table+" CASCADE");}
        if(!caches.isEmpty())redis(t->{for(DataResource r:caches){Set<String> keys=t.keys(physical(r,"*"));if(keys!=null&&!keys.isEmpty())t.delete(keys);}return null;});
    }

    @Test void postgresSchemaParameterizedCommitExplainAndAffectedRowRollback()throws Exception{
        String name=table("orders");sql("CREATE TABLE public."+name+" (id BIGINT PRIMARY KEY,status TEXT)");sql("INSERT INTO public."+name+" VALUES (1,'paid'),(2,'paid')");
        DataResource r=postgres();PostgresDataAdapter a=new PostgresDataAdapter(secrets,json);JsonNode schema=a.schema(r);
        ObjectNode mutation=update(name,1,"cancelled");mutation.putArray("filters").addObject().put("column","status").put("operator","eq").put("value","paid");
        assertEquals("FAILED_ROLLED_BACK",a.execute(r,a.compile(r,schema,mutation)).status());assertEquals("paid",status(name,1));
        var read=a.compile(r,schema,query(name));assertEquals(2,a.execute(r,read).result().get("returnedRows"));assertTrue(a.explain(r,read).containsKey("plan"));
        mutation.put("expectedRows",2);var committed=a.execute(r,a.compile(r,schema,mutation));assertEquals("COMMITTED",committed.status());assertNotNull(committed.privateSnapshot());
        assertEquals("COMPENSATED",a.compensate(r,committed.privateSnapshot()).status());assertEquals("paid",status(name,1));assertEquals("paid",status(name,2));
    }

    @Test void postgresPrimaryForeignKeyMetadataAndTypedDeterministicPage()throws Exception{
        String parent=table("parents"),name=table("typed");UUID code=UUID.randomUUID();
        sql("CREATE TABLE public."+parent+" (id INTEGER PRIMARY KEY)");sql("INSERT INTO public."+parent+" VALUES (1)");
        sql("CREATE TABLE public."+name+" (id BIGINT PRIMARY KEY,parent_id INTEGER REFERENCES public."+parent+"(id),status TEXT,enabled BOOLEAN,day DATE,code UUID)");
        sql("INSERT INTO public."+name+" VALUES (1,1,'same',true,DATE '2026-01-01','"+code+"'),(2,1,'same',true,DATE '2026-01-02','"+code+"'),(3,1,NULL,false,DATE '2026-01-03','"+code+"')");
        DataResource r=postgres();PostgresDataAdapter a=new PostgresDataAdapter(secrets,json);JsonNode schema=a.schema(r),meta=null;
        for(JsonNode t:schema.path("tables"))if(t.path("name").asText().equals(name))meta=t;
        assertNotNull(meta);assertEquals("id",meta.path("primaryKey").get(0).asText());assertEquals(parent,meta.path("foreignKeys").get(0).path("referencesTable").asText());
        JsonNode ir=input("{\"kind\":\"query\",\"schema\":\"public\",\"table\":\""+name+"\",\"select\":[\"id\",\"parent_id\",\"status\",\"enabled\",\"day\",\"code\"],\"where\":{\"operator\":\"and\",\"conditions\":[{\"column\":\"enabled\",\"operator\":\"eq\",\"value\":true},{\"column\":\"id\",\"operator\":\"in\",\"value\":[1,2]}]},\"orderBy\":[{\"column\":\"status\",\"direction\":\"asc\"}],\"limit\":1,\"offset\":1,\"includeRowHashes\":true}");
        var out=a.execute(r,a.compile(r,schema,ir));assertEquals("SUCCEEDED",out.status());var row=((List<Map<String,Object>>)out.result().get("rows")).get(0);
        assertEquals(2L,((Number)row.get("id")).longValue());assertEquals(true,row.get("enabled"));assertEquals("2026-01-02",row.get("day"));assertEquals(code.toString(),row.get("code"));assertEquals(1,((List<?>)out.result().get("rowHashes")).size());
    }

    @Test void postgresRecoverySnapshotPersistenceFailureRollsBackAndLaterWriteConflicts()throws Exception{
        String name=table("recovery");sql("CREATE TABLE public."+name+" (id BIGINT PRIMARY KEY,status TEXT)");sql("INSERT INTO public."+name+" VALUES (1,'paid')");
        DataResource r=postgres();PostgresDataAdapter a=new PostgresDataAdapter(secrets,json);var plan=a.compile(r,a.schema(r),update(name,1,"private-new-status"));List<String> durable=new ArrayList<>();
        var failure=a.execute(r,plan,null,snapshot->{durable.add(snapshot);try{assertEquals("paid",status(name,1));}catch(SQLException e){throw new IllegalStateException(e);}throw new IllegalStateException("snapshot ledger unavailable");});
        assertEquals("FAILED_ROLLED_BACK",failure.status());assertEquals("paid",status(name,1));assertEquals(1,durable.size());assertFalse(durable.get(0).contains("private-new-status"));
        var committed=a.execute(r,plan,null,durable::add);assertEquals("COMMITTED",committed.status());assertEquals(committed.privateSnapshot(),durable.get(1));
        sql("UPDATE public."+name+" SET status='later-external-write' WHERE id=1");assertEquals("COMPENSATION_CONFLICT",a.compensate(r,committed.privateSnapshot()).status());assertEquals("later-external-write",status(name,1));
    }

    @Test void postgresExactBigintAndDecimalStringsSurviveEditingAndRecovery()throws Exception{
        String name=table("precision");sql("CREATE TABLE public."+name+" (id BIGINT PRIMARY KEY,amount NUMERIC(35,12))");sql("INSERT INTO public."+name+" VALUES (9223372036854775807,12345678901234567890.123456789012)");
        DataResource r=postgres();PostgresDataAdapter a=new PostgresDataAdapter(secrets,json);JsonNode schema=a.schema(r);
        JsonNode query=input("{\"kind\":\"query\",\"schema\":\"public\",\"table\":\""+name+"\",\"select\":[\"id\",\"amount\"],\"includeRowHashes\":true}");var read=a.execute(r,a.compile(r,schema,query));var row=((List<Map<String,Object>>)read.result().get("rows")).get(0);
        assertEquals("9223372036854775807",row.get("id"));assertEquals("12345678901234567890.123456789012",row.get("amount"));
        ObjectNode edit=(ObjectNode)input("{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\""+name+"\",\"operation\":\"update\",\"values\":{\"amount\":\"12345678901234567890.000000000001\"},\"filters\":[{\"column\":\"id\",\"operator\":\"eq\",\"value\":\"9223372036854775807\"}],\"expectedRows\":1}");edit.put("expectedBeforeHash",((List<String>)read.result().get("rowHashes")).get(0));
        var committed=a.execute(r,a.compile(r,schema,edit));assertEquals("COMMITTED",committed.status());assertEquals("COMPENSATED",a.compensate(r,committed.privateSnapshot()).status());assertEquals(row,((List<Map<String,Object>>)a.execute(r,a.compile(r,schema,query)).result().get("rows")).get(0));
    }

    @Test void postgresDeletionRecoveryPreservesIdentityPrimaryKeyAndComputedColumns()throws Exception{
        String name=table("identity");sql("CREATE TABLE public."+name+" (id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,status TEXT,total INTEGER GENERATED ALWAYS AS (length(status)) STORED)");sql("INSERT INTO public."+name+" (status) VALUES ('paid')");
        DataResource r=postgres();PostgresDataAdapter a=new PostgresDataAdapter(secrets,json);JsonNode schema=a.schema(r);
        JsonNode deletion=input("{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\""+name+"\",\"operation\":\"delete\",\"filters\":[{\"column\":\"id\",\"operator\":\"eq\",\"value\":1}],\"expectedRows\":1}");
        var committed=a.execute(r,a.compile(r,schema,deletion));assertEquals("COMMITTED",committed.status());assertEquals(0,count(name));
        assertEquals("COMPENSATED",a.compensate(r,committed.privateSnapshot()).status());assertEquals(1,count(name));assertEquals("paid",status(name,1));
        try(Connection c=pg();ResultSet rs=c.createStatement().executeQuery("SELECT id,total FROM public."+name)){rs.next();assertEquals(1,rs.getLong("id"));assertEquals(4,rs.getInt("total"));}
    }

    @Test void postgresMigrationStepsCommitTogetherAndLaterFailureRollsBackEarlierDdl()throws Exception{
        String name=table("migration"),created=table("created");sql("CREATE TABLE public."+name+" (id BIGINT PRIMARY KEY,status TEXT)");
        DataResource r=postgres();PostgresDataAdapter a=new PostgresDataAdapter(secrets,json);JsonNode schema=a.schema(r);
        JsonNode good=input("{\"kind\":\"migration\",\"schema\":\"public\",\"steps\":[{\"operation\":\"add_column\",\"table\":\""+name+"\",\"column\":{\"name\":\"note\",\"type\":\"text\"}},{\"operation\":\"create_index\",\"table\":\""+name+"\",\"name\":\"ix_"+name+"\",\"columns\":[\"status\"]},{\"operation\":\"create_table\",\"table\":\""+created+"\",\"columns\":[{\"name\":\"id\",\"type\":\"bigint\",\"nullable\":false}],\"primaryKey\":[\"id\"]}]}");
        assertEquals("COMMITTED",a.execute(r,a.compile(r,schema,good)).status());assertEquals(0,count(created));schema=a.schema(r);
        JsonNode bad=input("{\"kind\":\"migration\",\"schema\":\"public\",\"steps\":[{\"operation\":\"add_column\",\"table\":\""+name+"\",\"column\":{\"name\":\"must_rollback\",\"type\":\"text\"}},{\"operation\":\"create_index\",\"table\":\""+name+"\",\"name\":\"ix_"+name+"\",\"columns\":[\"status\"]}]}");
        assertEquals("FAILED_ROLLED_BACK",a.execute(r,a.compile(r,schema,bad)).status());
        try(Connection c=pg();PreparedStatement s=c.prepareStatement("SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name=? AND column_name='must_rollback'")){s.setString(1,name);try(ResultSet rs=s.executeQuery()){rs.next();assertEquals(0,rs.getLong(1));}}
    }

    @Test void redisEncryptedSnapshotScanScopeStaleCasAndCompensation()throws Exception{
        RedisDataAdapter a=new RedisDataAdapter(secrets,json);UUID project=UUID.randomUUID();DataResource r=cacheResource(project),sameProjectOtherResource=cacheResource(project),otherProject=cacheResource(UUID.randomUUID());
        var write=set(a,r,"session:1","private-value",120);assertEquals("COMMITTED",write.status());assertFalse(write.privateSnapshot().contains("private-value"));
        Map<String,Object> result=get(a,r,"session:1");assertEquals("private-value",result.get("value"));assertEquals("string",result.get("type"));assertEquals(13,result.get("sizeBytes"));assertTrue(((Number)result.get("generation")).longValue()>0);
        assertNull(get(a,sameProjectOtherResource,"session:1").get("value"));assertNull(get(a,otherProject,"session:1").get("value"));
        var scan=cache(a,r,"{\"kind\":\"cache\",\"operation\":\"scan\",\"count\":100}");var page=a.execute(r,scan,null);assertEquals("SUCCEEDED",page.status());assertTrue(page.result().containsKey("cursor"));assertTrue(page.result().containsKey("entries"));assertFalse(page.result().toString().contains("::pcmeta"));
        assertEquals("COMPENSATED",a.compensate(r,write.privateSnapshot()).status());assertNull(get(a,r,"session:1").get("value"));
        var plan=cache(a,r,"{\"kind\":\"cache\",\"operation\":\"set\",\"key\":\"session:1\",\"value\":\"again\",\"ttlSeconds\":120}");String stale=a.prepare(r,plan);assertEquals("COMMITTED",a.execute(r,plan,stale).status());assertEquals("FAILED_ROLLED_BACK",a.execute(r,plan,stale).status());
        assertEquals("COMMITTED",a.execute(r,cache(a,r,"{\"kind\":\"cache\",\"operation\":\"delete\",\"key\":\"session:1\"}")).status());
    }

    @Test void redisManagedAbaAndPermanentTombstoneRejectOldRecovery()throws Exception{
        RedisDataAdapter a=new RedisDataAdapter(secrets,json);DataResource r=cacheResource(UUID.randomUUID());var first=set(a,r,"aba","A",120);assertEquals("COMMITTED",first.status());
        assertEquals("COMMITTED",set(a,r,"aba","B",120).status());assertEquals("COMMITTED",set(a,r,"aba","A",120).status());
        assertEquals("COMPENSATION_CONFLICT",a.compensate(r,first.privateSnapshot()).status());assertEquals("A",get(a,r,"aba").get("value"));
        var deletion=a.execute(r,cache(a,r,"{\"kind\":\"cache\",\"operation\":\"delete\",\"key\":\"aba\"}"));assertEquals("COMMITTED",deletion.status());
        redis(t->{assertFalse(Boolean.TRUE.equals(t.hasKey(physical(r,"aba"))));assertTrue(Boolean.TRUE.equals(t.hasKey(physical(r,"aba")+"::pcmeta")));return null;});
        assertEquals("COMMITTED",set(a,r,"aba","A",120).status());assertEquals("COMPENSATION_CONFLICT",a.compensate(r,deletion.privateSnapshot()).status());
    }

    @Test void redisMissingGenerationAndChangedExpiryRejectUnsafeWritesAndRecovery()throws Exception{
        RedisDataAdapter a=new RedisDataAdapter(secrets,json);DataResource r=cacheResource(UUID.randomUUID());
        var unmanaged=cache(a,r,"{\"kind\":\"cache\",\"operation\":\"set\",\"key\":\"unmanaged\",\"value\":\"new\",\"ttlSeconds\":120}");redis(t->{t.opsForValue().set(physical(r,"unmanaged"),"external",Duration.ofSeconds(120));return null;});
        assertThrows(ResponseStatusException.class,()->a.prepare(r,unmanaged));redis(t->{assertEquals("external",t.opsForValue().get(physical(r,"unmanaged")));return null;});
        var first=set(a,r,"ttl","A",120);assertEquals("COMMITTED",first.status());redis(t->{t.expire(physical(r,"ttl"),Duration.ofSeconds(30));return null;});
        assertEquals("COMPENSATION_CONFLICT",a.compensate(r,first.privateSnapshot()).status());assertEquals("A",get(a,r,"ttl").get("value"));
        var plan=cache(a,r,"{\"kind\":\"cache\",\"operation\":\"set\",\"key\":\"ttl\",\"value\":\"B\",\"ttlSeconds\":120}");String snap=a.prepare(r,plan);redis(t->{t.opsForValue().set(physical(r,"ttl")+"::pcmeta","{\"generation\":999}");return null;});
        assertEquals("FAILED_ROLLED_BACK",a.execute(r,plan,snap).status());redis(t->{assertEquals("A",t.opsForValue().get(physical(r,"ttl")));return null;});
    }
}
