package dev.proofcode.control.agentplatform;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/account/preferences")
public class PersonalizationController {
 private final PersonalizationService service;
 public PersonalizationController(PersonalizationService service){this.service=service;}
 @GetMapping public PersonalizationService.View get(){return service.get();}
 @PutMapping public PersonalizationService.View save(@RequestBody PersonalizationService.Input input){return service.save(input);}
}
