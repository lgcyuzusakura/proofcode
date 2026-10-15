package dev.proofcode.control.project;

import jakarta.validation.Valid;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Size;
import java.net.URI;
import java.util.Locale;
import java.time.Instant;
import java.util.List;
import java.util.UUID;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.http.HttpStatus;
import org.springframework.web.server.ResponseStatusException;

@RestController
@RequestMapping("/api/projects")
public class ProjectController {
    private final ProjectRepository projects;
    public ProjectController(ProjectRepository projects){this.projects=projects;}

    @PostMapping
    public ResponseEntity<ProjectEntity> create(@Valid @RequestBody CreateProject request){
        String source=normalizeSource(request.sourceKind());
        String repository=request.repositoryUrl()==null?null:request.repositoryUrl().trim();
        if (source.equals("REMOTE_REPOSITORY")) {
            if (repository==null||repository.isBlank()) throw new IllegalArgumentException("repositoryUrl is required for a remote project");
            URI uri=URI.create(repository);
            if (!("https".equalsIgnoreCase(uri.getScheme()) || "http".equalsIgnoreCase(uri.getScheme()))
                ||uri.getHost()==null||uri.getUserInfo()!=null||uri.getRawFragment()!=null) {
                throw new IllegalArgumentException("repositoryUrl must be an http or https URL without embedded credentials or fragments");
            }
        } else if (repository!=null&&!repository.isBlank()) {
            throw new IllegalArgumentException("repositoryUrl must be empty for a local or scratch project");
        }
        String bootstrap=request.bootstrapId()==null?null:request.bootstrapId().trim();
        if (bootstrap!=null&&!bootstrap.matches("[A-Za-z0-9._:-]{8,120}")) throw new IllegalArgumentException("bootstrapId is invalid");
        String handle=request.localHandle()==null?null:request.localHandle().trim();
        if (source.equals("LOCAL_FOLDER")&&!isOpaqueHandle(handle)) throw new IllegalArgumentException("localHandle is required for a local project");
        if (!source.equals("LOCAL_FOLDER")&&handle!=null&&!handle.isBlank()) throw new IllegalArgumentException("localHandle is only valid for local projects");
        String normalizedRepository=repository==null||repository.isBlank()?null:repository;
        if(bootstrap!=null){var existing=projects.findByBootstrapId(bootstrap);if(existing.isPresent())return reuse(existing.get(),request,source,normalizedRepository,handle);}
        ProjectEntity value=new ProjectEntity(UUID.randomUUID(),request.name().trim(),normalizedRepository,request.defaultBranch().trim(),source,handle,bootstrap,Instant.now());
        try{return ResponseEntity.status(201).body(projects.saveAndFlush(value));}
        catch(DataIntegrityViolationException conflict){if(bootstrap==null)throw conflict;return reuse(projects.findByBootstrapId(bootstrap).orElseThrow(()->conflict),request,source,normalizedRepository,handle);}
    }

    @GetMapping
    List<ProjectEntity> list(){return projects.findAll();}

    private static String normalizeSource(String value){String source=value==null?"REMOTE_REPOSITORY":value.trim().toUpperCase(Locale.ROOT);if(!java.util.Set.of("REMOTE_REPOSITORY","LOCAL_FOLDER","SCRATCH").contains(source))throw new IllegalArgumentException("sourceKind is invalid");return source;}
    private static boolean isOpaqueHandle(String value){return value!=null&&value.matches("[A-Za-z0-9._:-]{8,200}");}
    private static ResponseEntity<ProjectEntity> reuse(ProjectEntity value,CreateProject request,String source,String repository,String handle){
        if(!value.getName().equals(request.name().trim())||!value.getDefaultBranch().equals(request.defaultBranch().trim())||!value.getSourceKind().equals(source)||!java.util.Objects.equals(value.getRepositoryUrl(),repository)||!java.util.Objects.equals(value.getLocalHandle(),handle))throw new ResponseStatusException(HttpStatus.CONFLICT,"bootstrapId already belongs to another project registration");
        return ResponseEntity.ok(value);
    }
    public record CreateProject(@NotBlank @Size(max=120) String name,
                                @Size(max=2048) String repositoryUrl,
                                @NotBlank @Size(max=200) String defaultBranch,
                                String sourceKind, String localHandle, String bootstrapId) {
        public CreateProject(String name,String repositoryUrl,String defaultBranch){this(name,repositoryUrl,defaultBranch,null,null,null);}
    }
}
