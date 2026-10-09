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
    @PatchMapping("/resources/{resourceId}") public DataResource updateResource(@PathVariable UUID projectId,@PathVariable UUID resourceId,@RequestBody JsonNode input){return service.updateResource(projectId,resourceId,input);}
    @PostMapping("/sessions") public DataSession createSession(@PathVariable UUID projectId,@RequestBody JsonNode input){return service.createSession(projectId,input);}
    @GetMapping("/sessions") public List<DataSession> sessions(@PathVariable UUID projectId){return service.sessions(projectId);}
    @GetMapping("/sessions/{sessionId}") public DataSession session(@PathVariable UUID projectId,@PathVariable UUID sessionId){return service.getSession(projectId,sessionId);}
    @PostMapping("/sessions/{sessionId}/close") public DataSession closeSession(@PathVariable UUID projectId,@PathVariable UUID sessionId){return service.closeSession(projectId,sessionId);}
    @PostMapping("/sessions/{sessionId}/schema") public Map<String,Object> sessionSchema(@PathVariable UUID projectId,@PathVariable UUID sessionId,@RequestBody JsonNode input){DataPolicy.fields(input,"resourceId");return service.inspectSessionSchema(projectId,sessionId,InternalDataController.uuid(input,"resourceId"));}
    @PostMapping("/sessions/{sessionId}/plans") public DataOperation sessionPlan(@PathVariable UUID projectId,@PathVariable UUID sessionId,@RequestBody JsonNode input){return service.planInSession(projectId,sessionId,input);}
    @GetMapping("/plans") public List<DataOperation> plans(@PathVariable UUID projectId){return service.plans(projectId);}
    @GetMapping("/plans/{planId}") public DataOperation plan(@PathVariable UUID projectId,@PathVariable UUID planId){return service.getForProject(projectId,planId);}
    @PostMapping("/plans/{planId}/explain") public Map<String,Object> explain(@PathVariable UUID projectId,@PathVariable UUID planId,@RequestBody JsonNode input){DataPolicy.fields(input,"digest");return service.explainForProject(projectId,planId,DataPolicy.required(input,"digest"));}
    @GetMapping("/audit") public List<DataAudit> audit(@PathVariable UUID projectId){return service.audit(projectId);}
    @PostMapping("/tasks/{taskId}/schema") public Map<String,Object> schema(@PathVariable UUID projectId,@PathVariable UUID taskId,@RequestBody JsonNode input){DataPolicy.fields(input,"attempt","resourceId");service.requireTaskProject(projectId,taskId,InternalDataController.attempt(input));return service.inspectSchema(taskId,InternalDataController.attempt(input),InternalDataController.uuid(input,"resourceId"));}
    @PostMapping("/tasks/{taskId}/plans") public DataOperation plan(@PathVariable UUID projectId,@PathVariable UUID taskId,@RequestBody JsonNode input){service.requireTaskProject(projectId,taskId,InternalDataController.attempt(input));return service.plan(taskId,input);}
    @PostMapping("/plans/{planId}/approval") public DataOperationService.ApprovalResult approve(@PathVariable UUID projectId,@PathVariable UUID planId,@RequestBody JsonNode input){DataPolicy.fields(input,"digest","approved");if(!input.path("approved").isBoolean())throw DataPolicy.bad("approved must be boolean");return service.approve(projectId,planId,DataPolicy.required(input,"digest"),input.get("approved").booleanValue());}
    @PostMapping("/plans/{planId}/execute") public DataOperationService.ExecutionResult execute(@PathVariable UUID projectId,@PathVariable UUID planId,@RequestBody JsonNode input){DataPolicy.fields(input,"attempt","digest","capability");int attempt=service.getForProject(projectId,planId).getDataSessionId()==null?InternalDataController.attempt(input):DataPolicy.integer(input,"attempt",0,0,0);return service.executeWithCapability(projectId,attempt,planId,DataPolicy.required(input,"digest"),DataPolicy.required(input,"capability"));}
    @PostMapping("/plans/{planId}/compensation") public DataOperation compensate(@PathVariable UUID projectId,@PathVariable UUID planId,@RequestBody JsonNode input){DataPolicy.fields(input,"idempotencyKey");return service.compensationPlan(projectId,planId,DataPolicy.required(input,"idempotencyKey"));}
}
