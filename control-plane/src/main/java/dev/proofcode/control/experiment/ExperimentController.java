package dev.proofcode.control.experiment;

import jakarta.validation.Valid;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Size;
import java.util.List;
import java.util.UUID;
import org.springframework.http.ResponseEntity;
import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/experiments")
public class ExperimentController {
    private final ExperimentService service;
    public ExperimentController(ExperimentService service){this.service=service;}
    @PostMapping public ResponseEntity<ExperimentService.Comparison> create(@RequestHeader(value="Idempotency-Key",required=false) String key,@Valid @RequestBody CreateExperiment request){
        var result=service.create(request.toService(),key);
        return ResponseEntity.status(result.created()?201:200).body(service.compare(request.projectId(),result.experiment().getId()));
    }
    @GetMapping public List<ExperimentEntity> list(@RequestParam UUID projectId){return service.list(projectId);}
    @GetMapping("/{id}") public ExperimentService.Comparison get(@PathVariable UUID id,@RequestParam UUID projectId){return service.compare(projectId,id);}
    @GetMapping("/{id}/compare") public Object compare(@PathVariable UUID id,@RequestParam UUID projectId,@RequestParam(defaultValue="json") String format){
        var value=service.compare(projectId,id);
        if("csv".equalsIgnoreCase(format)) return ResponseEntity.ok().contentType(new MediaType("text","csv",java.nio.charset.StandardCharsets.UTF_8))
            .header(HttpHeaders.CONTENT_DISPOSITION,"attachment; filename=\"experiment-"+id+".csv\"").body(CsvComparison.render(value));
        if(!"json".equalsIgnoreCase(format)) throw new IllegalArgumentException("format must be json or csv");
        return value;
    }
    public record CreateExperiment(@NotNull UUID projectId,UUID workspaceId,UUID conversationId,@NotBlank @Size(max=160) String name,@NotBlank @Size(max=131072) String prompt,@NotBlank @Size(max=200) String model,@NotBlank String baseCommit,@Min(1) @Max(100) int maxSteps,@NotBlank @Size(max=4096) String testCommand,@Min(0) @Max(2) double temperature,@Min(1) @Max(20) int repetitions){
        ExperimentService.CreateRequest toService(){return new ExperimentService.CreateRequest(projectId,workspaceId,conversationId,name,prompt,model,baseCommit,maxSteps,testCommand,temperature,repetitions);}
    }
}
