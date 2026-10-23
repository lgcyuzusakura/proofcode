package dev.proofcode.control.agentplatform;

import jakarta.persistence.*;
import java.util.UUID;

@Entity @Table(name="agent_resources",uniqueConstraints=@UniqueConstraint(columnNames={"project_id","kind","name"}))
public class AgentResource {
 @Id public UUID id;
 @Column(name="project_id",nullable=false) public UUID projectId;
 @Column(nullable=false,length=16) public String kind;
 @Column(nullable=false,length=160) public String name;
 @Column(nullable=false,columnDefinition="text") public String body;
 @Version public long version;
 protected AgentResource(){}
 public AgentResource(UUID projectId,String kind,String name,String body){this.id=UUID.randomUUID();this.projectId=projectId;this.kind=kind;this.name=name;this.body=body;}
}
