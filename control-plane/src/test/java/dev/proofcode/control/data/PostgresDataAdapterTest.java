package dev.proofcode.control.data;

import com.fasterxml.jackson.databind.*;
import java.sql.*;
import java.util.*;
import org.junit.jupiter.api.*;
import org.springframework.mock.env.MockEnvironment;
import org.springframework.web.server.ResponseStatusException;
import static org.junit.jupiter.api.Assertions.*;

class PostgresDataAdapterTest {
    private final ObjectMapper json=new ObjectMapper();
    private String url;
    private DataResource resource;
    private PostgresDataAdapter adapter;
    private JsonNode schema;
    @BeforeEach void setup()throws Exception{url="jdbc:h2:mem:data"+UUID.randomUUID()+";MODE=PostgreSQL;DB_CLOSE_DELAY=-1;DATABASE_TO_LOWER=TRUE";try(Connection c=DriverManager.getConnection(url)){c.createStatement().execute("CREATE TABLE public.orders (id BIGINT PRIMARY KEY,status VARCHAR(100))");c.createStatement().execute("INSERT INTO public.orders VALUES (1,'paid'),(2,'paid')");}resource=new DataResource(UUID.randomUUID(),"postgres","dev","TEST","[\"public\"]");adapter=new PostgresDataAdapter(new DataSecrets(new MockEnvironment().withProperty("DATA_SNAPSHOT_KEY",Base64.getEncoder().encodeToString(new byte[32])),json),json){@Override protected Connection connect(DataResource r)throws SQLException{return DriverManager.getConnection(url);}};schema=adapter.schema(resource);}
    private JsonNode ir(String body)throws Exception{return json.readTree(body);}
    @Test void expectedRowsMismatchRollsBackActualSqlTransaction()throws Exception{JsonNode input=ir("{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\"orders\",\"operation\":\"update\",\"values\":{\"status\":\"cancelled\"},\"filters\":[{\"column\":\"status\",\"operator\":\"eq\",\"value\":\"paid\"}],\"expectedRows\":1}");assertEquals("FAILED_ROLLED_BACK",adapter.execute(resource,adapter.compile(resource,schema,input)).status());try(Connection c=DriverManager.getConnection(url);ResultSet r=c.createStatement().executeQuery("SELECT count(*) FROM orders WHERE status='paid'")){r.next();assertEquals(2,r.getInt(1));}}
    @Test void validParameterizedWriteCommitsAndNullValueBinds()throws Exception{JsonNode input=ir("{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\"orders\",\"operation\":\"update\",\"values\":{\"status\":null},\"filters\":[{\"column\":\"id\",\"operator\":\"eq\",\"value\":1}],\"expectedRows\":1}");var compiled=adapter.compile(resource,schema,input);assertTrue(compiled.sql().contains("?"));assertEquals("COMMITTED",adapter.execute(resource,compiled).status());try(Connection c=DriverManager.getConnection(url);ResultSet r=c.createStatement().executeQuery("SELECT status FROM orders WHERE id=1")){r.next();assertNull(r.getString(1));}}
    @Test void preventsRawSqlUnknownColumnsAndUnfilteredDelete()throws Exception{for(String body:List.of("{\"kind\":\"query\",\"rawSql\":\"DROP TABLE orders\"}","{\"kind\":\"query\",\"schema\":\"public\",\"table\":\"orders\",\"select\":[\"unknown\"]}","{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\"orders\",\"operation\":\"delete\",\"expectedRows\":2}")){assertThrows(ResponseStatusException.class,()->adapter.compile(resource,schema,json.readTree(body)));}}
    @Test void injectedPredicateIsBoundAsLiteralAndQueryIsLimited()throws Exception{JsonNode input=ir("{\"kind\":\"query\",\"schema\":\"public\",\"table\":\"orders\",\"select\":[\"id\"],\"filters\":[{\"column\":\"status\",\"operator\":\"eq\",\"value\":\"paid' OR 1=1 --\"}],\"limit\":1}");var compiled=adapter.compile(resource,schema,input);assertFalse(compiled.sql().contains("OR 1=1"));assertEquals(0,adapter.execute(resource,compiled).result().get("returnedRows"));}
    @Test void metadataIncludesPrimaryKeysAndIndexesAndTypedStablePaging()throws Exception{
        JsonNode table=schema.path("tables").get(0);assertEquals("id",table.path("primaryKey").get(0).asText());assertTrue(table.has("indexes"));assertTrue(table.path("columns").get(0).has("jdbcType"));
        var p=adapter.compile(resource,schema,ir("{\"kind\":\"query\",\"schema\":\"public\",\"table\":\"orders\",\"select\":[\"id\",\"status\"],\"orderBy\":[{\"column\":\"status\",\"direction\":\"asc\"}],\"offset\":1,\"limit\":1}"));
        assertTrue(p.sql().contains("\"id\" ASC"));var outcome=adapter.execute(resource,p);var rows=(List<Map<String,Object>>)outcome.result().get("rows");assertEquals(2,((Number)rows.get(0).get("id")).intValue());
        assertThrows(ResponseStatusException.class,()->adapter.compile(resource,schema,ir("{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\"orders\",\"operation\":\"update\",\"values\":{\"status\":123},\"filters\":[{\"column\":\"id\",\"operator\":\"eq\",\"value\":1}],\"expectedRows\":1}")));
    }
    @Test void nestedAndOrInAndNullPredicatesRemainBoundedAndParameterized()throws Exception{
        var p=adapter.compile(resource,schema,ir("{\"kind\":\"query\",\"schema\":\"public\",\"table\":\"orders\",\"select\":[\"id\"],\"where\":{\"operator\":\"or\",\"conditions\":[{\"column\":\"id\",\"operator\":\"in\",\"value\":[1]},{\"column\":\"status\",\"operator\":\"is_null\"}]}}"));
        assertEquals(1,adapter.execute(resource,p).result().get("returnedRows"));assertTrue(p.sql().contains("IN (?)"));assertTrue(p.sql().contains("IS NULL"));
    }
    @Test void encryptedRowRecoveryIsDurableBeforeCommitAndRefusesLaterWrites()throws Exception{
        var p=adapter.compile(resource,schema,ir("{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\"orders\",\"operation\":\"update\",\"values\":{\"status\":\"private-new-status\"},\"filters\":[{\"column\":\"id\",\"operator\":\"eq\",\"value\":1}],\"expectedRows\":1}"));
        List<String> stored=new ArrayList<>();var out=adapter.execute(resource,p,null,snapshot->{stored.add(snapshot);try(Connection c=DriverManager.getConnection(url);ResultSet rs=c.createStatement().executeQuery("SELECT status FROM orders WHERE id=1")){rs.next();assertEquals("paid",rs.getString(1));}catch(SQLException e){throw new IllegalStateException(e);}});
        assertEquals("COMMITTED",out.status());assertEquals(1,stored.size());assertFalse(stored.get(0).contains("private-new-status"));assertEquals("COMPENSATED",adapter.compensate(resource,out.privateSnapshot()).status());
        var second=adapter.execute(resource,p);try(Connection c=DriverManager.getConnection(url)){c.createStatement().execute("UPDATE orders SET status='later-write' WHERE id=1");}assertEquals("COMPENSATION_CONFLICT",adapter.compensate(resource,second.privateSnapshot()).status());
        try(Connection c=DriverManager.getConnection(url);ResultSet rs=c.createStatement().executeQuery("SELECT status FROM orders WHERE id=1")){rs.next();assertEquals("later-write",rs.getString(1));}
    }
    @Test void snapshotPersistenceFailureRollsBackTargetAndStaleRowHashRefusesUpdate()throws Exception{
        var query=adapter.compile(resource,schema,ir("{\"kind\":\"query\",\"schema\":\"public\",\"table\":\"orders\",\"select\":[\"id\",\"status\"],\"includeRowHashes\":true,\"filters\":[{\"column\":\"id\",\"operator\":\"eq\",\"value\":1}]}"));String hash=((List<String>)adapter.execute(resource,query).result().get("rowHashes")).get(0);
        var input=(com.fasterxml.jackson.databind.node.ObjectNode)ir("{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\"orders\",\"operation\":\"update\",\"values\":{\"status\":\"cancelled\"},\"filters\":[{\"column\":\"id\",\"operator\":\"eq\",\"value\":1}],\"expectedRows\":1}");input.put("expectedBeforeHash",hash);var p=adapter.compile(resource,schema,input);
        assertEquals("FAILED_ROLLED_BACK",adapter.execute(resource,p,null,s->{throw new IllegalStateException("storage unavailable");}).status());
        try(Connection c=DriverManager.getConnection(url)){c.createStatement().execute("UPDATE orders SET status='external' WHERE id=1");}assertEquals("FAILED_PRECONDITION",adapter.execute(resource,p).status());
        try(Connection c=DriverManager.getConnection(url);ResultSet rs=c.createStatement().executeQuery("SELECT status FROM orders WHERE id=1")){rs.next();assertEquals("external",rs.getString(1));}
    }
    @Test void insertAndDeleteRecoverOnlyTheConfirmedPrimaryKey()throws Exception{
        var insert=adapter.compile(resource,schema,ir("{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\"orders\",\"operation\":\"insert\",\"values\":{\"id\":3,\"status\":\"new\"},\"expectedRows\":1}"));var inserted=adapter.execute(resource,insert);assertEquals("COMMITTED",inserted.status());assertEquals("COMPENSATED",adapter.compensate(resource,inserted.privateSnapshot()).status());
        var delete=adapter.compile(resource,schema,ir("{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\"orders\",\"operation\":\"delete\",\"filters\":[{\"column\":\"id\",\"operator\":\"eq\",\"value\":1}],\"expectedRows\":1}"));var deleted=adapter.execute(resource,delete);assertEquals("COMMITTED",deleted.status());assertEquals("COMPENSATED",adapter.compensate(resource,deleted.privateSnapshot()).status());assertEquals(2,adapter.execute(resource,adapter.compile(resource,schema,ir("{\"kind\":\"query\",\"schema\":\"public\",\"table\":\"orders\",\"select\":[\"id\"]}"))).result().get("returnedRows"));
    }
    @Test void migrationCompilerAllowsOnlyReviewedBoundedTransactionalDdl()throws Exception{
        var p=adapter.compile(resource,schema,ir("{\"kind\":\"migration\",\"schema\":\"public\",\"steps\":[{\"operation\":\"add_column\",\"table\":\"orders\",\"column\":{\"name\":\"note\",\"type\":\"text\",\"nullable\":true}},{\"operation\":\"create_index\",\"table\":\"orders\",\"name\":\"ix_status\",\"columns\":[\"status\"]}]}"));assertEquals("migration",p.kind());assertTrue(p.preview().contains("ADD COLUMN"));
        for(String forbidden:List.of("{\"kind\":\"migration\",\"schema\":\"public\",\"steps\":[{\"operation\":\"drop_table\",\"table\":\"orders\"}]}","{\"kind\":\"migration\",\"schema\":\"public\",\"steps\":[{\"operation\":\"add_column\",\"table\":\"orders\",\"column\":{\"name\":\"note\",\"type\":\"text);DROP TABLE orders;--\"}}]}"))assertThrows(ResponseStatusException.class,()->adapter.compile(resource,schema,ir(forbidden)));
    }
}
