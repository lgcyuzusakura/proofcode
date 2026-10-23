package dev.proofcode.control.agentplatform;

import java.util.*;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/projects/{project}/skills")
public class SkillController {
 private final SkillService service;
 public SkillController(SkillService service){this.service=service;}
 @GetMapping public List<SkillService.View> list(@PathVariable UUID project){return service.list(project);}
 @PostMapping public SkillService.View create(@PathVariable UUID project,@RequestBody SkillService.Input input){return service.save(project,null,input);}
 @PatchMapping("/{id}") public SkillService.View update(@PathVariable UUID project,@PathVariable UUID id,@RequestBody SkillService.Input input){return service.save(project,id,input);}
 @PostMapping("/import") public SkillService.View importMarkdown(@PathVariable UUID project,@RequestBody SkillService.ImportInput input){return service.importMarkdown(project,input);}
 @PostMapping("/{id}/delete") public ResponseEntity<Void> delete(@PathVariable UUID project,@PathVariable UUID id,@RequestParam long version){service.delete(project,id,version);return ResponseEntity.noContent().build();}
}
