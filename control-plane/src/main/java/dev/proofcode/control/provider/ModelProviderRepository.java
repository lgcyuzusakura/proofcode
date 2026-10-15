package dev.proofcode.control.provider;
import org.springframework.data.jpa.repository.JpaRepository;
import java.util.*;
public interface ModelProviderRepository extends JpaRepository<ModelProvider,UUID> { List<ModelProvider> findByOwnerIdOrderByCreatedAtAsc(UUID ownerId); }
