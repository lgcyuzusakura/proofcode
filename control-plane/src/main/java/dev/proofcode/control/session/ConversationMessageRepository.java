package dev.proofcode.control.session;
import java.util.*;
import org.springframework.data.jpa.repository.*;
import org.springframework.data.repository.query.Param;
public interface ConversationMessageRepository extends JpaRepository<ConversationMessage,UUID>{
    List<ConversationMessage> findTop500ByConversationIdAndSequenceGreaterThanOrderBySequenceAsc(UUID conversationId,long after);
    List<ConversationMessage> findByConversationIdAndSequenceLessThanOrderBySequenceAsc(UUID conversationId,long before);
    Optional<ConversationMessage> findByTaskIdAndAttemptAndRole(UUID taskId,int attempt,String role);
    @Query("select coalesce(max(m.sequence),0) from ConversationMessage m where m.conversationId=:conversationId") long maxSequence(@Param("conversationId") UUID conversationId);
}
