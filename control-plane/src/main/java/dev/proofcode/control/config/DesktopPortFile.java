package dev.proofcode.control.config;

import java.nio.file.*;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.web.context.WebServerInitializedEvent;
import org.springframework.context.event.EventListener;
import org.springframework.context.annotation.Profile;
import org.springframework.stereotype.Component;

/** A private parent-owned directory carries startup readiness, never credentials. */
@Component
@Profile("desktop")
public class DesktopPortFile {
    private final String portFile;
    public DesktopPortFile(@Value("${PROOFCODE_DESKTOP_PORT_FILE:}") String portFile){this.portFile=portFile;}
    @EventListener
    public void ready(WebServerInitializedEvent event) throws java.io.IOException {
        if(portFile.isBlank())return;
        Path path=Path.of(portFile), temporary=path.resolveSibling(path.getFileName()+".tmp");
        Files.writeString(temporary,Integer.toString(event.getWebServer().getPort()),StandardOpenOption.CREATE_NEW);
        Files.move(temporary,path,StandardCopyOption.ATOMIC_MOVE);
    }
}
