package dev.proofcode.control.provider;
import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;
@Entity @Table(name="model_providers")
public class ModelProvider {
    @Id UUID id;
    @Column(name="owner_id",nullable=false) UUID ownerId;
    @Column(nullable=false,length=120) String name;
    @Column(name="base_url",nullable=false,length=2048) String baseUrl;
    @Column(name="wire_api",nullable=false,length=16) String wireApi;
    @Column(name="encrypted_key",nullable=false,columnDefinition="text") String encryptedKey;
    @Column(name="models_json",nullable=false,columnDefinition="text") String modelsJson="[]";
    @Column(name="aliases_json",nullable=false,columnDefinition="text") String aliasesJson="{}";
    @Column(name="settings_json",nullable=false,columnDefinition="text") String settingsJson="{}";
    @Column(name="catalog_at") Instant catalogAt;
    @Column(name="created_at",nullable=false) Instant createdAt;
    @Version long version;
    protected ModelProvider(){}
    ModelProvider(UUID owner){id=UUID.randomUUID();ownerId=owner;createdAt=Instant.now();}
}
