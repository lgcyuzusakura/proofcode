package dev.proofcode.control.provider;

import com.sun.net.httpserver.HttpServer;
import java.net.*;
import java.net.http.*;
import java.util.concurrent.*;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class ProviderProbeTest {
    @Test void oversizedResponseIsCancelledBeforeItCanBeParsed() throws Exception {
        var server=HttpServer.create(new InetSocketAddress("127.0.0.1",0),0);
        server.createContext("/probe",exchange->{
            try{byte[] bytes=new byte[(256<<10)+1];exchange.sendResponseHeaders(200,bytes.length);exchange.getResponseBody().write(bytes);}finally{exchange.close();}
        });server.start();
        try{
            var request=HttpRequest.newBuilder(URI.create("http://127.0.0.1:"+server.getAddress().getPort()+"/probe")).GET().build();
            var error=assertThrows(ExecutionException.class,()->ProviderProbe.send(HttpClient.newHttpClient(),request));
            assertInstanceOf(IllegalArgumentException.class,error.getCause());
        }finally{server.stop(0);}
    }
    @Test void deadlineCoversAResponseWhoseHeadersArriveButBodyStalls() throws Exception {
        var server=HttpServer.create(new InetSocketAddress("127.0.0.1",0),0);
        var release=new CountDownLatch(1);var headers=new CountDownLatch(1);
        server.createContext("/probe",exchange->{
            try{exchange.sendResponseHeaders(200,0);exchange.getResponseBody().flush();headers.countDown();release.await();}catch(InterruptedException e){Thread.currentThread().interrupt();}finally{exchange.close();}
        });server.start();
        try{
            var request=HttpRequest.newBuilder(URI.create("http://127.0.0.1:"+server.getAddress().getPort()+"/probe")).GET().build();
            assertThrows(TimeoutException.class,()->ProviderProbe.send(HttpClient.newHttpClient(),request,300));
            assertTrue(headers.await(1,TimeUnit.SECONDS),"test must reach the stalled body");
        }finally{release.countDown();server.stop(0);}
    }
}
