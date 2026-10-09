package dev.proofcode.control.config;

import dev.proofcode.control.websocket.TaskSocketHandler;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.context.TestConfiguration;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Import;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpMethod;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.jms.core.JmsTemplate;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.server.ResponseStatusException;

import static org.junit.jupiter.api.Assertions.assertEquals;

@SpringBootTest(
    webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT,
    properties = "spring.task.scheduling.enabled=false")
@Import(SecurityErrorDispatchIntegrationTest.EndpointConfiguration.class)
class SecurityErrorDispatchIntegrationTest {
    @MockBean JmsTemplate jmsTemplate;
    @MockBean StringRedisTemplate redisTemplate;
    @MockBean TaskSocketHandler sockets;

    @Autowired TestRestTemplate rest;
    @LocalServerPort int port;

    @Test
    void authenticatedErrorsKeepTheirStatusAcrossErrorDispatch() {
        ResponseEntity<String> internal = rest.exchange(
            url("/internal/error-dispatch/conflict"),
            HttpMethod.POST,
            authorized("runner-token"),
            String.class);
        ResponseEntity<String> external = rest.exchange(
            url("/api/error-dispatch/not-found"),
            HttpMethod.GET,
            authorized("test-token"),
            String.class);

        assertEquals(HttpStatus.CONFLICT, internal.getStatusCode());
        assertEquals(HttpStatus.NOT_FOUND, external.getStatusCode());
    }

    @Test
    void errorDispatchPermitDoesNotAuthorizeRequestsToInternalEndpointsOrErrorPath() {
        assertEquals(HttpStatus.FORBIDDEN, rest.postForEntity(
            url("/internal/error-dispatch/conflict"), null, String.class).getStatusCode());
        assertEquals(HttpStatus.FORBIDDEN, postInternalWithToken("wrong-token").getStatusCode());
        assertEquals(HttpStatus.FORBIDDEN, postInternalWithToken("test-token").getStatusCode());
        assertEquals(HttpStatus.FORBIDDEN, rest.getForEntity(url("/error"), String.class).getStatusCode());
    }

    private ResponseEntity<String> postInternalWithToken(String token) {
        return rest.exchange(
            url("/internal/error-dispatch/conflict"),
            HttpMethod.POST,
            authorized(token),
            String.class);
    }

    @Test
    void editorSourceFilesRequireUserAuthentication() {
        String path = "/api/projects/" + java.util.UUID.randomUUID()
            + "/workspaces/" + java.util.UUID.randomUUID()
            + "/sources/" + java.util.UUID.randomUUID() + "/files";
        assertEquals(HttpStatus.FORBIDDEN, rest.getForEntity(url(path), String.class).getStatusCode());
        assertEquals(HttpStatus.FORBIDDEN, rest.exchange(url(path), HttpMethod.GET, authorized("runner-token"), String.class).getStatusCode());
        assertEquals(HttpStatus.NOT_FOUND, rest.exchange(url(path), HttpMethod.GET, authorized("test-token"), String.class).getStatusCode());
    }

    private HttpEntity<Void> authorized(String token) {
        HttpHeaders headers = new HttpHeaders();
        headers.setBearerAuth(token);
        return new HttpEntity<>(headers);
    }

    private String url(String path) {
        return "http://localhost:" + port + path;
    }

    @TestConfiguration(proxyBeanMethods = false)
    static class EndpointConfiguration {
        @Bean
        ErrorDispatchFixtureController errorDispatchFixtureController() {
            return new ErrorDispatchFixtureController();
        }
    }

    @RestController
    static class ErrorDispatchFixtureController {
        @PostMapping("/internal/error-dispatch/conflict")
        void conflict() {
            throw new ResponseStatusException(HttpStatus.CONFLICT, "fixture conflict");
        }

        @GetMapping("/api/error-dispatch/not-found")
        void notFound() {
            throw new ResponseStatusException(HttpStatus.NOT_FOUND, "fixture not found");
        }
    }
}
