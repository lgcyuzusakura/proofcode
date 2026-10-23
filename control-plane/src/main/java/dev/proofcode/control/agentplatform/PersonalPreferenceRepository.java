package dev.proofcode.control.agentplatform;

import java.util.*;
import org.springframework.data.jpa.repository.JpaRepository;

public interface PersonalPreferenceRepository extends JpaRepository<PersonalPreference,UUID> {
 Optional<PersonalPreference> findByOwnerId(UUID ownerId);
}
