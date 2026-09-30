package dev.proofcode.control.project;

import jakarta.validation.Valid;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Size;
import java.net.URI;
import java.time.Instant;
import java.util.List;
import java.util.UUID;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/projects")
public class ProjectController {
    private final ProjectRepository projects;
    public ProjectController(ProjectRepository projects){this.projects=projects;}

    @PostMapping
    ResponseEntity<ProjectEntity> create(@Valid @RequestBody CreateProject request){
        URI uri=URI.create(request.repositoryUrl());
        if (!("https".equalsIgnoreCase(uri.getScheme()) || "http".equalsIgnoreCase(uri.getScheme()))) {
            throw new IllegalArgumentException("repositoryUrl must use http or https");
        }
        ProjectEntity value=new ProjectEntity(UUID.randomUUID(),request.name(),request.repositoryUrl(),request.defaultBranch(),Instant.now());
        return ResponseEntity.status(201).body(projects.save(value));
    }

    @GetMapping
    List<ProjectEntity> list(){return projects.findAll();}

    public record CreateProject(@NotBlank @Size(max=120) String name,
                                @NotBlank String repositoryUrl,
                                @NotBlank @Size(max=200) String defaultBranch) {}
}

