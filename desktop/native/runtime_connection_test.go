package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeConnectionNormalizesRootURLAndRejectsEmbeddedSecrets(t *testing.T) {
	for _, row := range []struct{ input, want string }{
		{" http://LOCALHOST:8080/ ", "http://localhost:8080"},
		{"https://Example.COM:443/", "https://example.com"},
		{"http://[::1]:8090/", "http://[::1]:8090"},
	} {
		got, err := normalizeControlPlaneURL(row.input)
		if err != nil || got != row.want {
			t.Fatalf("normalize %q: %q %v", row.input, got, err)
		}
	}
	for _, value := range []string{"", "file:///tmp/api", "https://user:private-key@example.com", "https://example.com/api", "https://example.com?secret=private-key", "https://example.com#private-key", "http://example.com?", "http://example.com#", "http://example.com:", "http://[::1]:", "http://example.com:0", "http://example.com:99999", "http://example.com\r\nprivate-key", "http://example.com/\\evil", "http://example.com/%2F"} {
		if _, err := normalizeControlPlaneURL(value); err == nil || strings.Contains(err.Error(), "private-key") {
			t.Fatalf("accepted or leaked invalid URL %q: %v", value, err)
		}
	}
}

func TestRuntimeConnectionSavedAddressIsPersistentAndDoesNotContactServer(t *testing.T) {
	t.Setenv("PROOFCODE_CONTROL_PLANE_URL", "http://127.0.0.1:8877")
	app := &App{registryDir: t.TempDir()}
	config, err := app.GetRuntimeConnection()
	if err != nil || config.Source != "environment" || config.BaseURL != "http://127.0.0.1:8877" {
		t.Fatalf("environment: %+v %v", config, err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = io.WriteString(w, `{}`) }))
	defer server.Close()
	config, err = app.SaveRuntimeConnection(server.URL + "/")
	if err != nil || config.BaseURL != server.URL || config.Source != "saved" || calls != 0 {
		t.Fatalf("save: %+v %v calls=%d", config, err, calls)
	}
	restored := &App{registryDir: app.registryDir}
	config, err = restored.GetRuntimeConnection()
	if err != nil || config.BaseURL != server.URL || config.Source != "saved" || calls != 0 {
		t.Fatalf("restore: %+v %v", config, err)
	}
	if _, err = app.SaveRuntimeConnection("https://user:secret@example.com"); err == nil {
		t.Fatal("bad save accepted")
	}
	config, err = app.GetRuntimeConnection()
	if err != nil || config.BaseURL != server.URL {
		t.Fatal("invalid save changed configuration")
	}
}

func TestRuntimeConnectionDefaultCorruptAndBusyStates(t *testing.T) {
	t.Setenv("PROOFCODE_CONTROL_PLANE_URL", "")
	app := &App{registryDir: t.TempDir()}
	config, err := app.GetRuntimeConnection()
	if err != nil || config.Source != "default" || config.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("default: %+v %v", config, err)
	}
	if err := os.WriteFile(filepath.Join(app.registryDir, "runtime-connection.json"), []byte(`{"baseUrl":"https://secret-key@localhost"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetRuntimeConnection(); err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("corruption not rejected: %v", err)
	}
	ctx, err := app.beginRuntimeOperation("starting", time.Minute)
	if err != nil || ctx == nil {
		t.Fatal(err)
	}
	if _, err := app.SaveRuntimeConnection("http://127.0.0.1:8877"); err == nil {
		t.Fatal("changed address during service operation")
	}
	_, _ = app.finishRuntimeOperation(RuntimeManagement{}, nil)
	if _, err := app.SaveRuntimeConnection("http://127.0.0.1:8877"); err != nil {
		t.Fatal(err)
	}
}

func TestSavedRuntimeConnectionIsUsedByProxyAndDoctorAndRedirectIsNotFollowed(t *testing.T) {
	app := &App{registryDir: t.TempDir()}
	t.Setenv("PROOFCODE_CONTROL_PLANE_URL", "http://127.0.0.1:1")
	t.Setenv("PATH", t.TempDir())
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/actuator/health" {
			if r.Header.Get("Authorization") != "" {
				t.Error("doctor sent credentials")
			}
			_, _ = io.WriteString(w, `{"status":"UP"}`)
			return
		}
		if r.URL.Path != "/api/projects" || r.Header.Get("Authorization") != "Bearer session" {
			t.Errorf("unexpected proxy %s", r.URL)
		}
		_, _ = io.WriteString(w, `[]`)
	}))
	defer server.Close()
	if _, err := app.SaveRuntimeConnection(server.URL); err != nil {
		t.Fatal(err)
	}
	response, err := app.ProxyRequest("/api/projects", "GET", "", "Bearer session", "")
	if err != nil || response.Status != 200 || calls != 1 {
		t.Fatalf("proxy: %+v %v", response, err)
	}
	app.startup(context.Background())
	report := app.CheckRuntime()
	if report.Checks[4].State != "ready" || calls != 2 {
		t.Fatalf("doctor did not use saved connection: %+v", report)
	}
	sinkCalls := 0
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sinkCalls++; w.WriteHeader(200) }))
	defer sink.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.URL+"/api/projects", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	if _, err := app.SaveRuntimeConnection(redirect.URL); err != nil {
		t.Fatal(err)
	}
	response, err = app.ProxyRequest("/api/projects", "POST", `{"private":"value"}`, "Bearer secret", "")
	if err != nil || response.Status != 307 || sinkCalls != 0 {
		t.Fatalf("redirect followed: %+v %v calls=%d", response, err, sinkCalls)
	}
}
