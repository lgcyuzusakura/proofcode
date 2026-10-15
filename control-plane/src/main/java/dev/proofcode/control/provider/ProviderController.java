package dev.proofcode.control.provider;
import org.springframework.web.bind.annotation.*;
import org.springframework.http.*;
import dev.proofcode.control.task.TaskRepository;
import java.util.*;
@RestController
public class ProviderController {
    private final ProviderService service;private final TaskRepository tasks;
    public ProviderController(ProviderService service,TaskRepository tasks){this.service=service;this.tasks=tasks;}
    @GetMapping("/api/model-providers") public List<ProviderService.View> list(){return service.list();}
    @PostMapping("/api/model-providers") public ResponseEntity<ProviderService.View> create(@RequestBody ProviderService.Input input){return ResponseEntity.status(201).body(service.save(null,input));}
    @PatchMapping("/api/model-providers/{id}") public ProviderService.View update(@PathVariable UUID id,@RequestBody ProviderService.Input input){return service.save(id,input);}
    @PostMapping("/api/model-providers/{id}/models/refresh") public ProviderService.View refresh(@PathVariable UUID id){return service.refresh(id);}
    @PostMapping("/api/model-providers/{id}/delete") public ResponseEntity<Void> delete(@PathVariable UUID id){service.delete(id);return ResponseEntity.noContent().build();}
    @GetMapping("/internal/tasks/{taskId}/model-connection") public ResponseEntity<ProviderService.Connection> connection(@PathVariable UUID taskId,@RequestParam UUID runnerId,@RequestParam int attempt){return ResponseEntity.ok().cacheControl(CacheControl.noStore()).body(service.resolve(tasks.findById(taskId).orElseThrow(),runnerId,attempt));}
}
