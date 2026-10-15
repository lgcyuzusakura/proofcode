package dev.proofcode.control.data;

import com.fasterxml.jackson.databind.*;
import java.sql.*;
import java.util.*;
import org.junit.jupiter.api.*;
import org.junit.jupiter.api.condition.EnabledIfEnvironmentVariable;
import org.springframework.mock.env.MockEnvironment;
import static org.junit.jupiter.api.Assertions.*;

/** Opt-in real provider checks; uses isolated disposable PostgreSQL/Redis containers, never developer resources. */
@EnabledIfEnvironmentVariable(named="PROOFCODE_LIVE_DATA_TEST",matches="true")
class LiveDataAdapterTest {
    private final ObjectMapper json=new ObjectMapper();
    private DataSecrets secrets;
    @BeforeEach void setup(){String host=System.getenv().getOrDefault("PROOFCODE_LIVE_DATA_HOST","host.docker.internal");MockEnvironment env=new MockEnvironment().withProperty("DATA_SNAPSHOT_KEY",Base64.getEncoder().encodeToString(new byte[32])).withProperty("DATA_SECRET_PG","{\"url\":\"jdbc:postgresql://"+host+":15491/data_test\",\"user\":\"proofcode\",\"password\":\"data-test-only\"}").withProperty("DATA_SECRET_CACHE","{\"host\":\""+host+"\",\"port\":16491}");secrets=new DataSecrets(env,json);}
    @Test void postgresSchemaParameterizedCommitExplainAndRollback()throws Exception{
        JsonNode secret=secrets.resolve("PG");String name="orders_"+UUID.randomUUID().toString().replace("-","");
        try(Connection c=DriverManager.getConnection(secret.path("url").asText(),"proofcode","data-test-only")){c.createStatement().execute("CREATE TABLE public."+name+" (id BIGINT PRIMARY KEY,status TEXT)");c.createStatement().execute("INSERT INTO public."+name+" VALUES (1,'paid'),(2,'paid')");}
        try{
            DataResource r=new DataResource(UUID.randomUUID(),"postgres","dev","PG","[\"public\"]");PostgresDataAdapter adapter=new PostgresDataAdapter(secrets,json);JsonNode schema=adapter.schema(r);
            JsonNode mutation=json.readTree("{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\""+name+"\",\"operation\":\"update\",\"values\":{\"status\":\"cancelled\"},\"filters\":[{\"column\":\"status\",\"operator\":\"eq\",\"value\":\"paid\"}],\"expectedRows\":1}");
            assertEquals("FAILED_ROLLED_BACK",adapter.execute(r,adapter.compile(r,schema,mutation)).status());
            JsonNode query=json.readTree("{\"kind\":\"query\",\"schema\":\"public\",\"table\":\""+name+"\",\"select\":[\"id\",\"status\"],\"filters\":[{\"column\":\"status\",\"operator\":\"eq\",\"value\":\"paid\"}]}");
            assertEquals(2,adapter.execute(r,adapter.compile(r,schema,query)).result().get("returnedRows"));assertTrue(adapter.explain(r,adapter.compile(r,schema,query)).containsKey("plan"));
            ((com.fasterxml.jackson.databind.node.ObjectNode)mutation).put("expectedRows",2);assertEquals("COMMITTED",adapter.execute(r,adapter.compile(r,schema,mutation)).status());
        }finally{try(Connection c=DriverManager.getConnection(secret.path("url").asText(),"proofcode","data-test-only")){c.createStatement().execute("DROP TABLE public."+name);}}
    }
    private DataAdapter.Compiled cache(RedisDataAdapter a,DataResource r,String ir)throws Exception{return a.compile(r,a.schema(r),json.readTree(ir));}
    @Test void redisEncryptedSnapshotScanCasAndCompensation()throws Exception{
        RedisDataAdapter a=new RedisDataAdapter(secrets,json);DataResource r=new DataResource(UUID.randomUUID(),"redis","dev","CACHE","[]");
        var set=cache(a,r,"{\"kind\":\"cache\",\"operation\":\"set\",\"key\":\"session:1\",\"value\":\"private-value\",\"ttlSeconds\":120}");String snapshot=a.prepare(r,set);assertFalse(snapshot.contains("private-value"));var write=a.execute(r,set,snapshot);assertEquals("COMMITTED",write.status());assertFalse(write.privateSnapshot().contains("private-value"));
        var get=cache(a,r,"{\"kind\":\"cache\",\"operation\":\"get\",\"key\":\"session:1\"}");assertEquals("private-value",a.execute(r,get,null).result().get("value"));
        var scan=cache(a,r,"{\"kind\":\"cache\",\"operation\":\"scan\",\"count\":100}");var page=a.execute(r,scan,null);assertEquals("SUCCEEDED",page.status());assertTrue(page.result().containsKey("cursor"));
        assertEquals("COMPENSATED",a.compensate(r,write.privateSnapshot()).status());assertNull(a.execute(r,get,null).result().get("value"));
        String stale=a.prepare(r,set);assertEquals("COMMITTED",a.execute(r,set,stale).status());assertEquals("FAILED_ROLLED_BACK",a.execute(r,set,stale).status());
        DataResource other=new DataResource(UUID.randomUUID(),"redis","dev","CACHE","[]");assertNull(a.execute(other,get,null).result().get("value"));
        var delete=cache(a,r,"{\"kind\":\"cache\",\"operation\":\"delete\",\"key\":\"session:1\"}");assertEquals("COMMITTED",a.execute(r,delete).status());
    }
}
