package dev.proofcode.control.account;
import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;
@Entity @Table(name="account_sessions")
public class AccountSession {
    @Id @Column(length=64) String digest;
    @Column(name="account_id",nullable=false) UUID accountId;
    @Column(name="expires_at",nullable=false) Instant expiresAt;
    protected AccountSession() {}
    AccountSession(String digest,UUID accountId){this.digest=digest;this.accountId=accountId;expiresAt=Instant.now().plusSeconds(43200);}
}
