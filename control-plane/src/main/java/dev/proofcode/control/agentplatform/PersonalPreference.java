package dev.proofcode.control.agentplatform;

import jakarta.persistence.*;
import java.util.UUID;

@Entity
@Table(name="account_preferences", uniqueConstraints=@UniqueConstraint(columnNames="owner_id"))
public class PersonalPreference {
 @Id @GeneratedValue private UUID id;
 @Column(name="owner_id",nullable=false,unique=true) private UUID ownerId;
 @Column(nullable=false,columnDefinition="text") public String body;
 @Version public long version;
 protected PersonalPreference(){}
 public PersonalPreference(UUID ownerId,String body){this.ownerId=ownerId;this.body=body;}
 public UUID getOwnerId(){return ownerId;}
}
