package dev.proofcode.control.source;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.project.ProjectRepository;
import dev.proofcode.control.session.WorkspaceService;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.*;
import org.springframework.stereotype.Service;
import org.springframework.http.HttpStatus;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;

@Service
public class SourceSnapshotService {
    private final SourceSnapshotRepository sources;private final WorkspaceService scopes;private final ProjectRepository projects;private final ObjectMapper json;
    public SourceSnapshotService(SourceSnapshotRepository sources,WorkspaceService scopes,ProjectRepository projects,ObjectMapper json){this.sources=sources;this.scopes=scopes;this.projects=projects;this.json=json;}
    public record FileEntry(String path,String sha256,String content,boolean executable){}
    public record Archive(String manifestHash,List<FileEntry> files){}
    public Optional<SourceSnapshot> latest(UUID projectId,UUID workspaceId){return sources.completedSources(projectId,workspaceId,org.springframework.data.domain.PageRequest.of(0,1)).stream().findFirst();}
    @Transactional public SourceSnapshot create(UUID projectId,UUID workspaceId,Archive archive){
        // The project lock also serializes duplicate immutable snapshot uploads.
        var project=projects.lockById(projectId).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));
        scopes.requireWorkspace(projectId,workspaceId);
        if(!"LOCAL_FOLDER".equals(project.getSourceKind())&&!"SCRATCH".equals(project.getSourceKind()))throw new IllegalArgumentException("source snapshots require a local or scratch project");
        long bytes=validate(archive);
        var existing=sources.findByProjectIdAndWorkspaceIdAndManifestHash(projectId,workspaceId,archive.manifestHash());if(existing.isPresent())return existing.get();
        try{return sources.save(new SourceSnapshot(UUID.randomUUID(),projectId,workspaceId,archive.manifestHash(),archive.files().size(),bytes,json.writeValueAsString(archive)));}catch(com.fasterxml.jackson.core.JsonProcessingException error){throw new IllegalStateException(error);}
    }
    public SourceSnapshot require(UUID projectId,UUID workspaceId,UUID id){
        SourceSnapshot source=sources.findById(id).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND,"source snapshot not found"));
        if(!projectId.equals(source.getProjectId())||(workspaceId!=null&&!workspaceId.equals(source.getWorkspaceId())))throw new ResponseStatusException(HttpStatus.NOT_FOUND,"source snapshot not found in workspace");return source;
    }
    public static long validate(Archive archive){
        if(archive==null||archive.files()==null||archive.files().size()>2000)throw new IllegalArgumentException("source snapshot requires at most 2000 files");
        var paths=new HashSet<String>();var entries=new ArrayList<>(archive.files());long bytes=0;
        for(FileEntry file:entries){
            if(file==null||!safePath(file.path())||!paths.add(file.path().toLowerCase(Locale.ROOT)))throw new IllegalArgumentException("invalid or duplicate snapshot path");
            if(file.content()==null||file.content().length()>1400000)throw new IllegalArgumentException("snapshot file exceeds 1 MiB");
            byte[] data;try{data=Base64.getDecoder().decode(file.content());}catch(IllegalArgumentException error){throw new IllegalArgumentException("snapshot content must be base64");}
            bytes+=data.length;if(data.length>1<<20||bytes>8<<20)throw new IllegalArgumentException("source snapshot exceeds 8 MiB or per-file limit");
            if(!digest(data).equals(file.sha256()))throw new IllegalArgumentException("snapshot file checksum mismatch");
        }
        entries.sort((left,right)->Arrays.compareUnsigned(left.path().getBytes(StandardCharsets.UTF_8),right.path().getBytes(StandardCharsets.UTF_8)));StringBuilder manifest=new StringBuilder();for(FileEntry file:entries)manifest.append(file.path()).append('\0').append(file.sha256()).append('\0').append(file.executable()?"1":"0").append('\n');
        if(!digest(manifest.toString().getBytes(StandardCharsets.UTF_8)).equals(archive.manifestHash()))throw new IllegalArgumentException("snapshot manifest checksum mismatch");
        return bytes;
    }
    public static boolean safePath(String path){
        if(path==null||path.isEmpty()||path.length()>500||path.startsWith("/")||path.contains("\\")||path.matches(".*[\u0000-\u001f:*?\"<>|].*"))return false;
        for(String part:path.split("/",-1)){String lower=part.toLowerCase(Locale.ROOT);if(part.isEmpty()||part.equals(".")||part.equals("..")||part.endsWith(".")||part.endsWith(" ")||Set.of(".git",".proofcode","node_modules",".context-store").contains(lower)||lower.matches("(con|prn|aux|nul|com[1-9]|lpt[1-9])(\\..*)?"))return false;}
        String lower=path.toLowerCase(Locale.ROOT);String name=lower.substring(lower.lastIndexOf('/')+1);
        return !(name.equals(".env")||name.startsWith(".env.")&&!name.equals(".env.example")||Set.of("auth.json","credentials.json","credentials.local.json","secrets.json").contains(name)||name.matches(".*\\.(pem|key|p12|pfx|jks)"));
    }
    private static String digest(byte[] data){try{return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(data));}catch(java.security.NoSuchAlgorithmException error){throw new IllegalStateException(error);}}
}
