package dev.proofcode.control.data;

import com.fasterxml.jackson.databind.JsonNode;
import java.util.*;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/projects/{projectId}/data")
public class DataController {
    private final DataOperationService service;
    public DataController(DataOperationService service){this.service=service;}
    @PostMapping("/resources") public DataResource register(@PathVariable UUID projectId,@RequestBody JsonNode input){return service.register(projectId,input);}
    @GetMapping("/resources") public List<DataResource> resources(@PathVariable UUID projectId){return service.resources(projectId);}
    @GetMapping("/plans") public List<DataOperation> plans(@PathVariable UUID projectId){return service.plans(projectId);}
    @GetMapping("/audit") public List<DataAudit> audit(@PathVariable UUID projectId){return service.audit(projectId);}
    @PostMapping("/tasks/{taskId}/schema") public Map<String,Object> schema(@PathVariable UUID projectId,@PathVariable UUID taskId,@RequestBody JsonNode input){DataPolicy.fields(input,"attempt","resourceId");service.requireTaskProject(projectId,taskId,InternalDataController.attempt(input));return service.inspectSchema(taskId,InternalDataController.attempt(input),InternalDataController.uuid(input,"resourceId"));}
    @PostMapping("/tasks/{taskId}/plans") public DataOperation plan(@PathVariable UUID projectId,@PathVariable UUID taskId,@RequestBody JsonNode input){service.requireTaskProject(projectId,taskId,InternalDataController.attempt(input));return service.plan(taskId,input);}
    @PostMapping("/plans/{planId}/approval") public DataOperationService.ApprovalResult approve(@PathVariable UUID projectId,@PathVariable UUID planId,@RequestBody JsonNode input){DataPolicy.fields(input,"digest","approved");if(!input.path("approved").isBoolean())throw DataPolicy.bad("approved must be boolean");return service.approve(projectId,planId,DataPolicy.required(input,"digest"),input.get("approved").booleanValue());}
    @PostMapping("/plans/{planId}/execute") public DataOperationService.ExecutionResult execute(@PathVariable UUID projectId,@PathVariable UUID planId,@RequestBody JsonNode input){DataPolicy.fields(input,"attempt","digest","capability");return service.executeWithCapability(projectId,InternalDataController.attempt(input),planId,DataPolicy.required(input,"digest"),DataPolicy.required(input,"capability"));}
    @PostMapping("/plans/{planId}/compensation") public DataOperation compensate(@PathVariable UUID projectId,@PathVariable UUID planId,@RequestBody JsonNode input){DataPolicy.fields(input,"idempotencyKey");return service.compensationPlan(projectId,planId,DataPolicy.required(input,"idempotencyKey"));}
}
