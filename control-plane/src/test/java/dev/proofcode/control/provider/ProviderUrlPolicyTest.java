package dev.proofcode.control.provider;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.proofcode.control.data.DataSecrets;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

class ProviderUrlPolicyTest {
    private ProviderService service(boolean allowPrivate) {
        return new ProviderService(mock(ModelProviderRepository.class),mock(DataSecrets.class),new ObjectMapper(),allowPrivate);
    }
    @Test void defaultPolicyRejectsLocalAndSpecialNetworkAddresses() {
        var service=service(false);
        for(String host:new String[]{"127.0.0.1","0.1.2.3","10.0.0.1","172.16.0.1","192.168.1.1","169.254.169.254","100.64.0.1","198.18.0.1","224.0.0.1","[::1]","[fc00::1]","[fd00::1]"}) {
            assertThrows(IllegalArgumentException.class,()->service.validateUrl("http://"+host+"/v1"),host);
        }
        assertEquals("https://8.8.8.8/v1",service.validateUrl("https://8.8.8.8/v1/"));
    }
    @Test void explicitPrivateAccessStillRejectsUnsafeUrlStructure() {
        var service=service(true);
        assertEquals("http://127.0.0.1:8000/v1",service.validateUrl("http://127.0.0.1:8000/v1"));
        for(String url:new String[]{"file:///etc/passwd","http://key@127.0.0.1/v1","http://127.0.0.1/v1?key=x","http://127.0.0.1/v1#fragment","http://127.0.0.1:99999/v1","http://metadata.google.internal/v1"}) {
            assertThrows(IllegalArgumentException.class,()->service.validateUrl(url),url);
        }
    }
}
