package dev.proofcode.control.account;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

@Entity @Table(name="accounts")
public class Account {
    @Id UUID id;
    @Column(nullable=false,unique=true,length=64) String username;
    @Column(name="password_hash",nullable=false,length=100) String passwordHash;
    @Column(name="created_at",nullable=false) Instant createdAt;
    protected Account() {}
    Account(String username,String hash){id=UUID.randomUUID();this.username=username;passwordHash=hash;createdAt=Instant.now();}
}
