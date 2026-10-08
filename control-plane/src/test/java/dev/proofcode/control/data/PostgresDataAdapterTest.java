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
    @BeforeEach void setup()throws Exception{url="jdbc:h2:mem:data"+UUID.randomUUID()+";MODE=PostgreSQL;DB_CLOSE_DELAY=-1;DATABASE_TO_LOWER=TRUE";try(Connection c=DriverManager.getConnection(url)){c.createStatement().execute("CREATE TABLE public.orders (id BIGINT PRIMARY KEY,status VARCHAR(100))");c.createStatement().execute("INSERT INTO public.orders VALUES (1,'paid'),(2,'paid')");}resource=new DataResource(UUID.randomUUID(),"postgres","dev","TEST","[\"public\"]");adapter=new PostgresDataAdapter(new DataSecrets(new MockEnvironment(),json),json){@Override protected Connection connect(DataResource r)throws SQLException{return DriverManager.getConnection(url);}};schema=adapter.schema(resource);}
    private JsonNode ir(String body)throws Exception{return json.readTree(body);}
    @Test void expectedRowsMismatchRollsBackActualSqlTransaction()throws Exception{JsonNode input=ir("{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\"orders\",\"operation\":\"update\",\"values\":{\"status\":\"cancelled\"},\"filters\":[{\"column\":\"status\",\"operator\":\"eq\",\"value\":\"paid\"}],\"expectedRows\":1}");assertEquals("FAILED_ROLLED_BACK",adapter.execute(resource,adapter.compile(resource,schema,input)).status());try(Connection c=DriverManager.getConnection(url);ResultSet r=c.createStatement().executeQuery("SELECT count(*) FROM orders WHERE status='paid'")){r.next();assertEquals(2,r.getInt(1));}}
    @Test void validParameterizedWriteCommitsAndNullValueBinds()throws Exception{JsonNode input=ir("{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\"orders\",\"operation\":\"update\",\"values\":{\"status\":null},\"filters\":[{\"column\":\"id\",\"operator\":\"eq\",\"value\":1}],\"expectedRows\":1}");var compiled=adapter.compile(resource,schema,input);assertTrue(compiled.sql().contains("?"));assertEquals("COMMITTED",adapter.execute(resource,compiled).status());try(Connection c=DriverManager.getConnection(url);ResultSet r=c.createStatement().executeQuery("SELECT status FROM orders WHERE id=1")){r.next();assertNull(r.getString(1));}}
    @Test void preventsRawSqlUnknownColumnsAndUnfilteredDelete()throws Exception{for(String body:List.of("{\"kind\":\"query\",\"rawSql\":\"DROP TABLE orders\"}","{\"kind\":\"query\",\"schema\":\"public\",\"table\":\"orders\",\"select\":[\"unknown\"]}","{\"kind\":\"mutation\",\"schema\":\"public\",\"table\":\"orders\",\"operation\":\"delete\",\"expectedRows\":2}")){assertThrows(ResponseStatusException.class,()->adapter.compile(resource,schema,json.readTree(body)));}}
    @Test void injectedPredicateIsBoundAsLiteralAndQueryIsLimited()throws Exception{JsonNode input=ir("{\"kind\":\"query\",\"schema\":\"public\",\"table\":\"orders\",\"select\":[\"id\"],\"filters\":[{\"column\":\"status\",\"operator\":\"eq\",\"value\":\"paid' OR 1=1 --\"}],\"limit\":1}");var compiled=adapter.compile(resource,schema,input);assertFalse(compiled.sql().contains("OR 1=1"));assertEquals(0,adapter.execute(resource,compiled).result().get("returnedRows"));}
}
