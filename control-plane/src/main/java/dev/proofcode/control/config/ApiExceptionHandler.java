package dev.proofcode.control.config;

import java.util.Map;
import java.util.NoSuchElementException;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

@RestControllerAdvice
public class ApiExceptionHandler {
    @ExceptionHandler(NoSuchElementException.class) ResponseEntity<Map<String,String>> notFound(){return ResponseEntity.status(HttpStatus.NOT_FOUND).body(Map.of("error","resource not found"));}
    @ExceptionHandler(org.springframework.web.server.ResponseStatusException.class) ResponseEntity<Map<String,String>> status(org.springframework.web.server.ResponseStatusException value){return ResponseEntity.status(value.getStatusCode()).body(Map.of("error",value.getReason()==null?"request rejected":value.getReason()));}
    @ExceptionHandler({IllegalArgumentException.class,MethodArgumentNotValidException.class}) ResponseEntity<Map<String,String>> badRequest(Exception value){return ResponseEntity.badRequest().body(Map.of("error",value.getMessage()==null?"invalid request":value.getMessage()));}
}

