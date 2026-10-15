package dev.proofcode.control.account;
import org.springframework.data.jpa.repository.JpaRepository;
import java.util.*;
public interface AccountRepository extends JpaRepository<Account,UUID> { Optional<Account> findByUsername(String username); }
