package dev.proofcode.control;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.scheduling.annotation.EnableScheduling;

@SpringBootApplication
@EnableScheduling
public class ProofCodeApplication {
    public static void main(String[] args) {
        SpringApplication.run(ProofCodeApplication.class, args);
    }
}

