package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyRequestForwardsAuthenticatedAPIRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/tasks" || request.URL.Query().Get("source") != "desktop" {
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.String())
		}
		if got := request.Header.Get("Authorization"); got != "Bearer desktop-token" {
			t.Errorf("authorization = %q", got)
		}
		if got := request.Header.Get("Idempotency-Key"); got != "request-1" {
			t.Errorf("idempotency key = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusAccepted)
		_, _ = writer.Write([]byte(`{"accepted":true}`))
	}))
	defer server.Close()
	t.Setenv("PROOFCODE_CONTROL_PLANE_URL", server.URL)

	value, err := (&App{}).ProxyRequest("/api/tasks?source=desktop", http.MethodPost, `{"prompt":"Fix"}`, "Bearer desktop-token", "request-1")
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != http.StatusAccepted || value.Body != `{"accepted":true}` {
		t.Fatalf("response = %#v", value)
	}
}

func TestProxyRequestRejectsNonAPIAndUnsafeInputs(t *testing.T) {
	app := &App{}
	for _, test := range []struct {
		name   string
		path   string
		method string
		body   string
		auth   string
	}{
		{name: "internal path", path: "/internal/tasks/1", method: http.MethodGet},
		{name: "path traversal", path: "/api/../internal", method: http.MethodGet},
		{name: "unsupported method", path: "/api/projects", method: http.MethodDelete},
		{name: "header injection", path: "/api/projects", method: http.MethodGet, auth: "Bearer ok\r\nX-Leak: yes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := app.ProxyRequest(test.path, test.method, test.body, test.auth, "")
			if err == nil {
				t.Fatal("expected proxy validation error")
			}
		})
	}
}

func TestProxyRequestSavesOAuthConfigurationUsingPut(t *testing.T) {
	const path = "/api/projects/project/mcp-servers/server/oauth"
	const payload = `{"clientId":"desktop-client","clientSecret":"fixture-secret","scope":"docs.read","clearClientSecret":false}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil || request.Method != http.MethodPut || request.URL.Path != path || request.URL.Query().Get("version") != "7" || string(body) != payload {
			t.Errorf("OAuth configuration request did not survive the desktop bridge")
		}
		if request.Header.Get("Authorization") != "Bearer fixture-session" || request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("OAuth request headers did not survive the desktop bridge")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"configured":true,"hasClientSecret":true,"serverVersion":8}`))
	}))
	defer server.Close()
	t.Setenv("PROOFCODE_CONTROL_PLANE_URL", server.URL)
	app := &App{registryDir: t.TempDir()}
	response, err := app.ProxyRequest(path+"?version=7", "put", payload, "Bearer fixture-session", "")
	if err != nil || response.Status != http.StatusOK || strings.Contains(response.Body, "fixture-secret") || !strings.Contains(response.Body, `"serverVersion":8`) {
		t.Fatalf("OAuth save through desktop proxy failed: status %d, error %v", response.Status, err)
	}
}

func TestProxyRequestRejectsOversizedBody(t *testing.T) {
	t.Setenv("PROOFCODE_CONTROL_PLANE_URL", "http://127.0.0.1:1")
	_, err := (&App{}).ProxyRequest("/api/projects", http.MethodPost, strings.Repeat("x", 20<<20+1), "", "")
	if err == nil {
		t.Fatal("expected body size validation error")
	}
}
