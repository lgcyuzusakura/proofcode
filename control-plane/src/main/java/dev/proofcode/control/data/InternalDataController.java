package dev.proofcode.control.data;

import com.fasterxml.jackson.databind.JsonNode;
import java.util.*;
import org.springframework.web.bind.annotation.*;

/** Runner credentials can inspect and execute an already user-approved plan, never approve or register sources. */
@RestController
@RequestMapping("/internal/tasks/{taskId}/data")
public class InternalDataController {
    private final DataOperationService service;
    public InternalDataController(DataOperationService service){this.service=service;}
    @GetMapping("/resources") public List<DataResource> resources(@PathVariable UUID taskId,@RequestParam int attempt){return service.resourcesForTask(taskId,attempt);}
    @PostMapping("/schema") public Map<String,Object> schema(@PathVariable UUID taskId,@RequestBody JsonNode input){DataPolicy.fields(input,"attempt","resourceId");return service.inspectSchema(taskId,attempt(input),uuid(input,"resourceId"));}
    @PostMapping("/plans") public DataOperation plan(@PathVariable UUID taskId,@RequestBody JsonNode input){return service.plan(taskId,input);}
    @GetMapping("/plans/{planId}") public DataOperation inspect(@PathVariable UUID taskId,@PathVariable UUID planId,@RequestParam int attempt){return service.getForTask(taskId,attempt,planId);}
    @PostMapping("/plans/{planId}/execute-approved") public DataOperationService.ExecutionResult execute(@PathVariable UUID taskId,@PathVariable UUID planId,@RequestBody JsonNode input){DataPolicy.fields(input,"attempt","digest");return service.executeApproved(taskId,attempt(input),planId,DataPolicy.required(input,"digest"));}
    @PostMapping("/plans/{planId}/explain") public Map<String,Object> explain(@PathVariable UUID taskId,@PathVariable UUID planId,@RequestBody JsonNode input){DataPolicy.fields(input,"attempt","digest");return service.explain(taskId,attempt(input),planId,DataPolicy.required(input,"digest"));}
    static int attempt(JsonNode input){int attempt=DataPolicy.integer(input,"attempt",1,Integer.MAX_VALUE,-1);if(attempt<1)throw DataPolicy.bad("attempt is required");return attempt;}
    static UUID uuid(JsonNode input,String field){try{return UUID.fromString(DataPolicy.required(input,field));}catch(IllegalArgumentException e){throw DataPolicy.bad("invalid "+field);}}
}
