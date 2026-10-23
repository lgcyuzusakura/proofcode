package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInspectHTTPDiscoversPagesWithoutExecutingAndRedactsCredentials(t *testing.T) {
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprint(sse), func(t *testing.T) {
			var calls, closed, pages atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture-secret" {
					t.Error("saved credential missing")
				}
				if r.Header.Get("X-Api-Key") != "fixture-key-secret" {
					t.Error("saved custom header missing")
				}
				if r.Method == "DELETE" {
					closed.Add(1)
					w.WriteHeader(204)
					return
				}
				var request struct {
					ID     int64          `json:"id"`
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				_ = json.NewDecoder(r.Body).Decode(&request)
				var result any
				switch request.Method {
				case "initialize":
					w.Header().Set("Mcp-Session-Id", "inspection-session")
					result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}}
				case "notifications/initialized":
					w.WriteHeader(202)
					return
				case "tools/list":
					if r.Header.Get("Mcp-Session-Id") != "inspection-session" {
						t.Error("missing session")
					}
					pages.Add(1)
					name, cursor := "search", "next"
					if request.Params["cursor"] == "next" {
						name, cursor = "read", ""
					}
					result = map[string]any{"tools": []any{map[string]any{"name": name, "description": "fixture-secret / fixture-key-secret / Bearer other-secret / <script>untrusted</script>", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"fixture-secret": map[string]any{"description": "fixture-secret"}, "fixture-key-secret": map[string]any{"enum": []any{"fixture-key-secret"}}}}}}, "nextCursor": cursor}
				default:
					calls.Add(1)
					t.Errorf("unexpected tool execution: %s", request.Method)
				}
				body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
				if sse {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: %s\n\n", body)
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(body)
				}
			}))
			defer server.Close()
			report := InspectHTTP(context.Background(), ServerConfig{Name: "docs", URL: server.URL, Bearer: "fixture-secret", HeaderName: "X-Api-Key", HeaderValue: "fixture-key-secret", AllowPrivateHosts: true})
			raw, _ := json.Marshal(report)
			if !report.OK || report.Code != "ok" || report.Protocol != "2025-03-26" || len(report.Tools) != 2 || report.Tools[0].QualifiedName != "mcp__docs__search" {
				t.Fatalf("bad report: %s", raw)
			}
			if pages.Load() != 2 || closed.Load() != 1 || calls.Load() != 0 {
				t.Fatalf("unexpected lifecycle: %d/%d/%d", pages.Load(), closed.Load(), calls.Load())
			}
			if strings.Contains(string(raw), "fixture-secret") || strings.Contains(string(raw), "fixture-key-secret") || strings.Contains(string(raw), "other-secret") {
				t.Fatal("credential leaked in metadata")
			}
		})
	}
}

func TestInspectHTTPClassifiesRemoteFailuresWithoutReturningBodies(t *testing.T) {
	for _, test := range []struct {
		status int
		code   string
	}{{401, "authentication"}, {403, "forbidden"}, {302, "redirect"}, {429, "rate_limit"}, {500, "http"}} {
		t.Run(test.code, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.Header().Set("Location", "/should-not-follow")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("private response body and token"))
			}))
			defer server.Close()
			report := InspectHTTP(context.Background(), ServerConfig{Name: "docs", URL: server.URL, AllowPrivateHosts: true})
			if report.OK || report.Code != test.code || report.HTTPStatus != test.status || len(report.Tools) != 0 || hits.Load() != 1 {
				t.Fatalf("unexpected: %+v hits=%d", report, hits.Load())
			}
			raw, _ := json.Marshal(report)
			if strings.Contains(string(raw), "private response") {
				t.Fatal("remote response leaked")
			}
		})
	}
}

func TestInspectHTTPRejectsPrivateHostAndBoundsTimeout(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer server.Close()
	report := InspectHTTP(context.Background(), ServerConfig{Name: "docs", URL: server.URL})
	if report.Code != "private_host" || hits.Load() != 0 {
		t.Fatalf("private host reached: %+v", report)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	report = InspectHTTP(ctx, ServerConfig{Name: "docs", URL: server.URL, AllowPrivateHosts: true})
	if report.Code != "timeout" || report.ElapsedMS > 1000 {
		t.Fatalf("deadline not applied: %+v", report)
	}
}

func TestInspectHTTPRejectsCatalogWithoutPartialMetadata(t *testing.T) {
	for _, test := range []struct {
		name, code string
		tools      []ToolDefinition
	}{
		{"long_name", "tool_name", []ToolDefinition{{Name: "safe", InputSchema: map[string]any{"type": "object"}}, {Name: strings.Repeat("x", 60), InputSchema: map[string]any{"type": "object"}}}},
		{"duplicate", "catalog", []ToolDefinition{{Name: "same", InputSchema: map[string]any{"type": "object"}}, {Name: "same", InputSchema: map[string]any{"type": "object"}}}},
		{"oversize", "catalog_size", []ToolDefinition{{Name: "large", Description: strings.Repeat("x", 513<<10), InputSchema: map[string]any{"type": "object"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ID     int64  `json:"id"`
					Method string `json:"method"`
				}
				_ = json.NewDecoder(r.Body).Decode(&request)
				result := map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}}
				if request.Method == "notifications/initialized" {
					w.WriteHeader(202)
					return
				}
				if request.Method == "tools/list" {
					result = map[string]any{"tools": test.tools}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
			}))
			defer server.Close()
			report := InspectHTTP(context.Background(), ServerConfig{Name: "docs", URL: server.URL, AllowPrivateHosts: true})
			if report.OK || report.Code != test.code || len(report.Tools) != 0 {
				t.Fatalf("unexpected: %+v", report)
			}
		})
	}
}
