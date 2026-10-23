package dev.proofcode.control.session;

import java.util.UUID;
import org.springframework.http.*;
import org.springframework.web.bind.annotation.*;

@RestController
public class HistorySearchController {
 private final HistorySearchService service;
 public HistorySearchController(HistorySearchService service){this.service=service;}
 @GetMapping("/api/history/search")
 public ResponseEntity<HistorySearchService.Page> search(@RequestParam(defaultValue="")String query,@RequestParam(required=false)UUID projectId,@RequestParam(defaultValue="ALL")HistorySearchService.Kind kind,@RequestParam(required=false)String cursor,@RequestParam(defaultValue="25")int limit){
  return ResponseEntity.ok().cacheControl(CacheControl.noStore()).body(service.search(query,projectId,kind,cursor,limit));
 }
}
