package dev.proofcode.control.account;

import jakarta.servlet.http.HttpServletRequest;
import java.time.Instant;
import java.util.concurrent.ConcurrentHashMap;
import org.springframework.web.bind.annotation.*;
import org.springframework.http.*;
import org.springframework.web.server.ResponseStatusException;

@RestController @RequestMapping("/api/auth")
public class AccountController {
    private final AccountService service;
    private final ConcurrentHashMap<String,Window> attempts=new ConcurrentHashMap<>();
    public AccountController(AccountService service){this.service=service;}
    @PostMapping("/register") public AccountService.Login register(@RequestBody Credentials c,HttpServletRequest request){limit(request);return service.register(c.username(),c.password());}
    @PostMapping("/login") public AccountService.Login login(@RequestBody Credentials c,HttpServletRequest request){limit(request);return service.login(c.username(),c.password());}
    @GetMapping("/me") public AccountService.User me(){return service.me();}
    @PostMapping("/logout") public ResponseEntity<Void> logout(@RequestHeader(value="Authorization",defaultValue="") String h){service.logout(h.startsWith("Bearer ")?h.substring(7):null);return ResponseEntity.noContent().build();}
    private void limit(HttpServletRequest r){long now=Instant.now().getEpochSecond();if(attempts.size()>10000)attempts.entrySet().removeIf(e->e.getValue().start+60<now);Window w=attempts.compute(r.getRemoteAddr(),(k,v)->v==null||v.start+60<now?new Window(now,1):new Window(v.start,v.count+1));if(w.count>20)throw new ResponseStatusException(HttpStatus.TOO_MANY_REQUESTS,"尝试过于频繁，请稍后重试");}
    record Window(long start,int count){}
    public record Credentials(String username,String password){}
}
