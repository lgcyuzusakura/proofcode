package dev.proofcode.control.data;
import com.fasterxml.jackson.databind.JsonNode;
import java.util.*;
public interface DataAdapter {
    JsonNode schema(DataResource resource);
    Compiled compile(DataResource resource,JsonNode schema,JsonNode ir);
    Outcome execute(DataResource resource,Compiled plan);
    default String prepare(DataResource resource,Compiled plan){return null;}
    default Outcome execute(DataResource resource,Compiled plan,String snapshot){return execute(resource,plan);}
    default Outcome compensate(DataResource resource,String snapshot){throw DataPolicy.bad("compensation is unsupported");}
    record Compiled(String kind,String preview,String sql,List<Object> parameters,JsonNode ir){}
    record Outcome(String status,Map<String,Object> result,String privateSnapshot){
        public static Outcome failed(String code){return new Outcome("FAILED_ROLLED_BACK",Map.of("errorCode",code),null);}
    }
}
