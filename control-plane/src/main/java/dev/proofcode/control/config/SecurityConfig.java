package dev.proofcode.control.config;

import jakarta.servlet.DispatcherType;
import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import java.io.IOException;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.http.HttpHeaders;
import org.springframework.security.authentication.UsernamePasswordAuthenticationToken;
import org.springframework.security.config.annotation.web.builders.HttpSecurity;
import org.springframework.security.config.http.SessionCreationPolicy;
import org.springframework.security.core.context.SecurityContextHolder;
import org.springframework.security.web.SecurityFilterChain;
import org.springframework.security.web.authentication.UsernamePasswordAuthenticationFilter;
import org.springframework.web.filter.OncePerRequestFilter;

@Configuration
public class SecurityConfig {
    @Bean
    SecurityFilterChain securityFilterChain(HttpSecurity http, TokenFilter filter) throws Exception {
        return http.csrf(csrf -> csrf.disable())
            .sessionManagement(session -> session.sessionCreationPolicy(SessionCreationPolicy.STATELESS))
            .authorizeHttpRequests(auth -> auth
                .dispatcherTypeMatchers(DispatcherType.ERROR).permitAll()
                .requestMatchers("/actuator/health/**", "/ws/**", "/api/auth/login", "/api/auth/register").permitAll()
                .anyRequest().authenticated())
            .addFilterBefore(filter, UsernamePasswordAuthenticationFilter.class)
            .build();
    }

    @Bean
    TokenFilter tokenFilter(@Value("${proofcode.auth-token}") String userToken,
                            @Value("${proofcode.runner-token}") String runnerToken, dev.proofcode.control.account.AccountService accounts) {
        return new TokenFilter(userToken, runnerToken, accounts);
    }

    static final class TokenFilter extends OncePerRequestFilter {
        private final String userToken;
        private final String runnerToken;
        private final dev.proofcode.control.account.AccountService accounts;
        TokenFilter(String userToken, String runnerToken, dev.proofcode.control.account.AccountService accounts) { this.userToken = userToken; this.runnerToken = runnerToken; this.accounts=accounts; }

        @Override
        protected void doFilterInternal(HttpServletRequest request, HttpServletResponse response, FilterChain chain)
                throws ServletException, IOException {
            String supplied = request.getHeader(HttpHeaders.AUTHORIZATION);
            if (supplied != null && supplied.startsWith("Bearer ")) {
                String token = supplied.substring(7);
                boolean internal = request.getRequestURI().startsWith("/internal/");
                java.util.UUID account = internal ? null : accounts.authenticate(token);
                if (account != null || (!internal && !userToken.equals("change-me") && token.equals(userToken)) || (internal && token.equals(runnerToken))) {
                    SecurityContextHolder.getContext().setAuthentication(
                        new UsernamePasswordAuthenticationToken(internal ? "runner" : account != null ? account : "user", null, java.util.List.of()));
                }
            }
            chain.doFilter(request, response);
        }
    }
}
