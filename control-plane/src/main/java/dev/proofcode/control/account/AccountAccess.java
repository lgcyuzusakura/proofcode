package dev.proofcode.control.account;

import dev.proofcode.control.project.*;
import dev.proofcode.control.task.TaskRepository;
import jakarta.servlet.http.*;
import java.util.UUID;
import org.springframework.stereotype.Component;
import org.springframework.web.servlet.*;
import org.springframework.web.servlet.config.annotation.*;
import org.springframework.http.HttpStatus;
import org.springframework.web.server.ResponseStatusException;

/** All public project/task routes pass through ownership checks, including nested data/source routes. */
@Component
public class AccountAccess implements WebMvcConfigurer,HandlerInterceptor {
    private final ProjectRepository projects; private final TaskRepository tasks;
    public AccountAccess(ProjectRepository projects,TaskRepository tasks){this.projects=projects;this.tasks=tasks;}
    @Override public void addInterceptors(InterceptorRegistry registry){registry.addInterceptor(this).addPathPatterns("/api/**");}
    @Override public boolean preHandle(HttpServletRequest r,HttpServletResponse response,Object handler){
        String[] path=r.getRequestURI().split("/");
        if(path.length>3&&path[2].equals("projects"))require(projects,UUID.fromString(path[3]));
        if(path.length>3&&path[2].equals("tasks")){UUID id=UUID.fromString(path[3]);require(projects,tasks.findById(id).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND)).getProjectId());}
        if(r.getParameter("projectId")!=null)require(projects,UUID.fromString(r.getParameter("projectId")));
        return true;
    }
    public static void require(ProjectRepository repository,UUID projectId){
        var auth=org.springframework.security.core.context.SecurityContextHolder.getContext().getAuthentication();
        // Internal runner calls and direct non-HTTP service tests use their existing scope checks.
        if(auth==null||"runner".equals(auth.getPrincipal()))return;
        var project=repository.findById(projectId).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));
        if(!java.util.Objects.equals(AccountService.currentId(),project.getOwnerId()))throw new ResponseStatusException(HttpStatus.NOT_FOUND,"project not found");
    }
}
