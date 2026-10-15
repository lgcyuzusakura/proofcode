package dev.proofcode.control.account;

import java.nio.charset.StandardCharsets;
import java.security.*;
import java.time.Instant;
import java.util.*;
import dev.proofcode.control.project.ProjectRepository;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.security.crypto.bcrypt.BCryptPasswordEncoder;
import org.springframework.security.core.context.SecurityContextHolder;
import org.springframework.http.HttpStatus;
import org.springframework.web.server.ResponseStatusException;

@Service
public class AccountService {
    private final AccountRepository accounts; private final AccountSessionRepository sessions; private final ProjectRepository projects;
    private final BCryptPasswordEncoder passwords=new BCryptPasswordEncoder(12);
    private final String dummy=passwords.encode("invalid-account-password");
    public AccountService(AccountRepository accounts,AccountSessionRepository sessions,ProjectRepository projects){this.accounts=accounts;this.sessions=sessions;this.projects=projects;}
    public static UUID currentId(){var a=SecurityContextHolder.getContext().getAuthentication();return a!=null&&a.getPrincipal() instanceof UUID id?id:null;}
    public static UUID requireId(){UUID id=currentId();if(id==null)throw new ResponseStatusException(HttpStatus.UNAUTHORIZED,"请登录账户");return id;}
    @Transactional public Login register(String username,String password){
        username=normalize(username);validatePassword(password);
        if(accounts.findByUsername(username).isPresent())throw new ResponseStatusException(HttpStatus.CONFLICT,"账户名已被使用");
        Account account;
        try{account=accounts.saveAndFlush(new Account(username,passwords.encode(password)));}
        catch(org.springframework.dao.DataIntegrityViolationException e){throw new ResponseStatusException(HttpStatus.CONFLICT,"账户名已被使用");}
        if(accounts.count()==1)projects.claimUnowned(account.id);
        return issue(account);
    }
    @Transactional public Login login(String username,String password){
        username=normalize(username);validatePassword(password);
        var account=accounts.findByUsername(username);
        boolean matches=passwords.matches(password,account.map(a->a.passwordHash).orElse(dummy));
        if(account.isEmpty()||!matches)throw new ResponseStatusException(HttpStatus.UNAUTHORIZED,"账户或密码不正确");
        // Also repairs upgrades where the sole account already existed before legacy ownership was enabled.
        if(accounts.count()==1)projects.claimUnowned(account.get().id);
        return issue(account.get());
    }
    private Login issue(Account a){byte[] bytes=new byte[32];new SecureRandom().nextBytes(bytes);String token=Base64.getUrlEncoder().withoutPadding().encodeToString(bytes);var session=sessions.save(new AccountSession(digest(token),a.id));return new Login(new User(a.id,a.username),token,session.expiresAt);}
    public UUID authenticate(String token){if(token==null||!token.matches("[A-Za-z0-9_-]{43}"))return null;return sessions.findById(digest(token)).filter(s->s.expiresAt.isAfter(Instant.now())).map(s->s.accountId).orElse(null);}
    @Transactional public void logout(String token){if(token!=null)sessions.deleteById(digest(token));}
    @org.springframework.scheduling.annotation.Scheduled(cron="0 17 * * * *")
    @Transactional public void purgeExpired(){sessions.deleteAll(sessions.findAll().stream().filter(s->!s.expiresAt.isAfter(Instant.now())).toList());}
    public User me(){var a=accounts.findById(requireId()).orElseThrow();return new User(a.id,a.username);}
    private static String normalize(String name){if(name==null||!name.matches("[A-Za-z0-9_.-]{3,64}"))throw new IllegalArgumentException("账户名需为 3–64 位字母、数字、点、横线或下划线");return name.toLowerCase(Locale.ROOT);}
    private static void validatePassword(String p){if(p==null||p.length()<8||p.getBytes(StandardCharsets.UTF_8).length>72)throw new IllegalArgumentException("密码需至少 8 位，UTF-8 长度不超过 72 字节");}
    private static String digest(String token){try{return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(token.getBytes(StandardCharsets.UTF_8)));}catch(NoSuchAlgorithmException e){throw new IllegalStateException(e);}}
    public record User(UUID id,String username){}
    public record Login(User user,String token,Instant expiresAt){}
}
