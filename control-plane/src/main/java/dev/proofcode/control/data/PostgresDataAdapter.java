package dev.proofcode.control.data;

import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.node.*;
import org.springframework.stereotype.Component;
import java.sql.*;
import java.util.*;

/** PostgreSQL only. DDL, stored procedures, arbitrary expressions and cross-store transactions are deliberately absent. */
@Component
public class PostgresDataAdapter implements DataAdapter {
    private final DataSecrets secrets;private final ObjectMapper json;
    public PostgresDataAdapter(DataSecrets secrets,ObjectMapper json){this.secrets=secrets;this.json=json;}
    protected Connection connect(DataResource r)throws SQLException {JsonNode s=secrets.resolve(r.secretRef);String url=DataPolicy.required(s,"url");if(!url.startsWith("jdbc:postgresql:"))throw DataPolicy.bad("only PostgreSQL JDBC endpoints are supported");return DriverManager.getConnection(url,DataPolicy.required(s,"user"),s.path("password").asText());}
    @Override public JsonNode schema(DataResource r){
        try(Connection c=connect(r)){DatabaseMetaData md=c.getMetaData();ArrayNode tables=json.createArrayNode();JsonNode allowed=json.readTree(r.allowedSchemas);List<String> schemaNames=new ArrayList<>();allowed.forEach(n->schemaNames.add(n.asText()));Collections.sort(schemaNames);
            for(String schema:schemaNames){DataPolicy.identifier(schema);try(ResultSet ts=md.getTables(null,schema,null,new String[]{"TABLE"})){List<String> names=new ArrayList<>();while(ts.next())names.add(ts.getString("TABLE_NAME"));Collections.sort(names);for(String table:names){if(!table.matches("[A-Za-z_][A-Za-z0-9_]{0,62}"))continue;ObjectNode t=tables.addObject().put("schema",schema).put("name",table);ArrayNode columns=t.putArray("columns");try(ResultSet cols=md.getColumns(null,schema,table,null)){while(cols.next())columns.addObject().put("name",cols.getString("COLUMN_NAME")).put("type",cols.getString("TYPE_NAME")).put("nullable",cols.getInt("NULLABLE")!=DatabaseMetaData.columnNoNulls);}}}}
            return json.createObjectNode().set("tables",tables);
        }catch(Exception e){throw DataPolicy.bad("schema inspection failed; verify source configuration and permissions");}
    }
    @Override public Compiled compile(DataResource r,JsonNode schema,JsonNode ir){
        DataPolicy.fields(ir,"kind","schema","table","select","filters","limit","operation","values","expectedRows");
        String kind=DataPolicy.required(ir,"kind"),sn=DataPolicy.required(ir,"schema"),table=DataPolicy.required(ir,"table");
        JsonNode tableMeta=null;for(JsonNode t:schema.path("tables")){if(sn.equals(t.path("schema").asText())&&table.equals(t.path("name").asText()))tableMeta=t;}if(tableMeta==null)throw DataPolicy.bad("table is absent from the approved schema snapshot");
        Set<String> columns=new HashSet<>();tableMeta.path("columns").forEach(n->columns.add(n.path("name").asText()));
        String target=DataPolicy.identifier(sn)+"."+DataPolicy.identifier(table);List<Object> params=new ArrayList<>();String sql;
        if(kind.equals("query")){
            if(ir.has("operation")||ir.has("values")||ir.has("expectedRows"))throw DataPolicy.bad("query cannot contain mutation fields");JsonNode select=ir.get("select");if(select==null||!select.isArray()||select.isEmpty()||select.size()>64)throw DataPolicy.bad("select requires 1 to 64 columns");List<String> selected=new ArrayList<>();for(JsonNode col:select){if(!col.isTextual())throw DataPolicy.bad("column must be a string");selected.add(column(columns,col.asText()));}
            sql="SELECT "+String.join(", ",selected)+" FROM "+target+filters(ir,columns,params)+" LIMIT "+DataPolicy.integer(ir,"limit",1,1000,100);
        }else if(kind.equals("mutation")){
            if(ir.has("select")||ir.has("limit"))throw DataPolicy.bad("mutation cannot contain query fields");String op=DataPolicy.required(ir,"operation");DataPolicy.integer(ir,"expectedRows",0,1000,-1);if(!ir.has("expectedRows"))throw DataPolicy.bad("expectedRows is required");
            if(op.equals("delete")){if(ir.has("values"))throw DataPolicy.bad("delete cannot contain values");sql="DELETE FROM "+target+requiredFilters(ir,columns,params);}
            else if(op.equals("insert")||op.equals("update")){JsonNode values=ir.get("values");if(values==null||!values.isObject()||values.isEmpty()||values.size()>64)throw DataPolicy.bad("values must contain 1 to 64 columns");List<String> keys=new ArrayList<>();values.fieldNames().forEachRemaining(keys::add);Collections.sort(keys);List<String> assignments=new ArrayList<>();for(String key:keys){String col=column(columns,key);params.add(scalar(values.get(key)));assignments.add(op.equals("update")?col+" = ?":col);}if(op.equals("insert")){if(ir.has("filters"))throw DataPolicy.bad("insert cannot contain filters");sql="INSERT INTO "+target+" ("+String.join(", ",assignments)+") VALUES ("+String.join(", ",Collections.nCopies(keys.size(),"?"))+")";}else sql="UPDATE "+target+" SET "+String.join(", ",assignments)+requiredFilters(ir,columns,params);}
            else throw DataPolicy.bad("unsupported mutation operation");
        }else throw DataPolicy.bad("unsupported SQL IR kind");
        return new Compiled(kind,sql,sql,Collections.unmodifiableList(new ArrayList<>(params)),ir);
    }
    private String column(Set<String> cols,String name){if(!cols.contains(name))throw DataPolicy.bad("unknown column");return DataPolicy.identifier(name);}
    private String requiredFilters(JsonNode ir,Set<String> cols,List<Object> params){String where=filters(ir,cols,params);if(where.isEmpty())throw DataPolicy.bad("mutation requires a nonempty WHERE");return where;}
    private String filters(JsonNode ir,Set<String> cols,List<Object> params){JsonNode filters=ir.get("filters");if(filters==null)return "";if(!filters.isArray()||filters.size()>32)throw DataPolicy.bad("filters must be an array of at most 32 predicates");List<String> predicates=new ArrayList<>();for(JsonNode f:filters){DataPolicy.fields(f,"column","operator","value");String op=switch(DataPolicy.required(f,"operator")){case "eq"->"=";case "ne"->"<>";case "lt"->"<";case "lte"->"<=";case "gt"->">";case "gte"->">=";default->throw DataPolicy.bad("unsupported filter operator");};if(!f.has("value")||f.get("value").isNull())throw DataPolicy.bad("filter value is required and non-null");predicates.add(column(cols,DataPolicy.required(f,"column"))+" "+op+" ?");params.add(scalar(f.get("value")));}return predicates.isEmpty()?"":" WHERE "+String.join(" AND ",predicates);}
    private Object scalar(JsonNode v){if(v==null||v.isNull())return null;if(v.isTextual()){if(v.asText().length()>65536)throw DataPolicy.bad("value is too large");return v.asText();}if(v.isBoolean())return v.booleanValue();if(v.isIntegralNumber())return v.canConvertToLong()?v.longValue():new java.math.BigDecimal(v.bigIntegerValue());if(v.isFloatingPointNumber())return v.decimalValue();throw DataPolicy.bad("only scalar SQL values are supported");}
    @Override public Outcome execute(DataResource r,Compiled p){
        Connection c=null;boolean commitAttempted=false;
        try{c=connect(r);c.setAutoCommit(false);c.setReadOnly(p.kind().equals("query"));
            if(p.kind().equals("query")){try(PreparedStatement st=c.prepareStatement(p.sql())){bind(st,p.parameters());st.setQueryTimeout(10);st.setMaxRows(1000);List<Map<String,Object>> rows=new ArrayList<>();int bytes=0;try(ResultSet rs=st.executeQuery()){int n=rs.getMetaData().getColumnCount();while(rs.next()){Map<String,Object> row=new LinkedHashMap<>();for(int i=1;i<=n;i++)row.put(rs.getMetaData().getColumnLabel(i),Objects.toString(rs.getObject(i),null));bytes+=json.writeValueAsBytes(row).length;if(bytes>262144)break;rows.add(row);}}c.rollback();return new Outcome("SUCCEEDED",Map.of("rows",rows,"returnedRows",rows.size(),"truncated",bytes>262144),null);}}
            try(PreparedStatement st=c.prepareStatement(p.sql())){bind(st,p.parameters());st.setQueryTimeout(10);int affected=st.executeUpdate();if(affected!=p.ir().path("expectedRows").asInt()){c.rollback();return Outcome.failed("EXPECTED_ROWS_MISMATCH");}commitAttempted=true;c.commit();return new Outcome("COMMITTED",Map.of("affectedRows",affected),null);}
        }catch(Exception e){if(commitAttempted)return new Outcome("COMMIT_UNKNOWN",Map.of("errorCode","COMMIT_RESULT_UNKNOWN"),null);if(c!=null)try{c.rollback();}catch(SQLException rollbackFailure){return new Outcome("COMMIT_UNKNOWN",Map.of("errorCode","ROLLBACK_UNCONFIRMED"),null);}return Outcome.failed("DATABASE_EXECUTION_FAILED");}
        finally{if(c!=null)try{c.close();}catch(SQLException ignored){}}
    }
    public Map<String,Object> explain(DataResource r,Compiled p){if(!p.kind().equals("query"))throw DataPolicy.bad("only read query EXPLAIN is supported");try(Connection c=connect(r)){c.setReadOnly(true);try(PreparedStatement s=c.prepareStatement("EXPLAIN (FORMAT JSON) "+p.sql())){bind(s,p.parameters());s.setQueryTimeout(10);try(ResultSet rows=s.executeQuery()){return Map.of("plan",rows.next()?rows.getString(1):"");}}}catch(Exception e){throw DataPolicy.bad("EXPLAIN failed");}}
    private void bind(PreparedStatement st,List<Object> parameters)throws SQLException{for(int i=0;i<parameters.size();i++)st.setObject(i+1,parameters.get(i));}
}
