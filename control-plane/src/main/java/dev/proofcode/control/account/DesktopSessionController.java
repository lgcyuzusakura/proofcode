package dev.proofcode.control.account;

import org.springframework.context.annotation.Profile;
import org.springframework.http.ResponseEntity;
import org.springframework.http.CacheControl;
import org.springframework.web.bind.annotation.*;

/** Only the native process holding the per-launch runner credential can open this session. */
@RestController
@Profile("desktop")
public class DesktopSessionController {
    private final AccountService accounts;
    public DesktopSessionController(AccountService accounts){this.accounts=accounts;}
    @PostMapping("/internal/desktop-session")
    public ResponseEntity<AccountService.Login> login(){return ResponseEntity.ok().cacheControl(CacheControl.noStore()).body(accounts.desktopLogin());}
}
