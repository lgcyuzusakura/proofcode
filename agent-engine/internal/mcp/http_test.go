package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/agent"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPRuntimeJSONAndSSE(t *testing.T) {
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprint(sse), func(t *testing.T) {
			var calls, deleted atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture-private-token" {
					t.Error("missing bearer")
				}
				if r.Header.Get("X-Docs-Key") != "fixture-api-key-secret" {
					t.Error("missing custom authentication header")
				}
				if r.Method == "DELETE" {
					deleted.Add(1)
					w.WriteHeader(204)
					return
				}
				var req struct {
					ID     int64          `json:"id"`
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				var result any
				switch req.Method {
				case "initialize":
					w.Header().Set("Mcp-Session-Id", "session-1")
					result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}}
				case "notifications/initialized":
					w.WriteHeader(202)
					return
				case "tools/list":
					if r.Header.Get("Mcp-Session-Id") != "session-1" || r.Header.Get("MCP-Protocol-Version") != "2025-03-26" {
						t.Error("session/version missing")
					}
					name, cursor := "echo", "next"
					if req.Params["cursor"] == "next" {
						name, cursor = "other", ""
					}
					result = map[string]any{"tools": []any{map[string]any{"name": name, "inputSchema": map[string]any{"type": "object"}}}, "nextCursor": cursor}
				case "tools/call":
					if req.Params["name"] == "other" {
						select {
						case <-r.Context().Done():
						case <-time.After(250 * time.Millisecond):
						}
						return
					}
					calls.Add(1)
					result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "real fixture result fixture-api-key-secret"}}, "isError": false}
				default:
					t.Errorf("unexpected method: %s", req.Method)
				}
				body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
				if sse {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, ": heartbeat\n\nevent: message\ndata: %s\n\n", body)
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(body)
				}
			}))
			defer server.Close()
			registry := tool.NewRegistry()
			runtime, err := Connect(context.Background(), []ServerConfig{{Name: "fixture", URL: server.URL + "/mcp", Bearer: "fixture-private-token", HeaderName: "X-Docs-Key", HeaderValue: "fixture-api-key-secret", AllowPrivateHosts: true}}, t.TempDir(), registry)
			if err != nil {
				t.Fatal(err)
			}
			if len(registry.Definitions()) != 2 {
				t.Fatal("missing paginated tools")
			}
			echo, _ := registry.Get("mcp__fixture__echo")
			if echo.Risk(nil) != tool.RiskExec {
				t.Fatal("remote call bypasses approval")
			}
			result := echo.Execute(context.Background(), json.RawMessage(`{"text":"hello"}`))
			if result.IsError || !strings.Contains(result.Content, "real fixture result") || strings.Contains(result.Content, "fixture-api-key-secret") || calls.Load() != 1 {
				t.Fatalf("bad result %+v", result)
			}
			engine := agent.Agent{Provider: &fixtureProvider{}, Tools: registry, Approval: agent.AutomaticApproval{AllowExec: true, AllowWrite: true}, Events: event.NewSequencedSink(&event.MemorySink{})}
			state, err := engine.Run(context.Background(), agent.RunRequest{TaskID: "http-task", Prompt: "call remote tool", MaxSteps: 3})
			var pause *agent.ApprovalRequiredError
			if !errors.As(err, &pause) || calls.Load() != 1 {
				t.Fatal("HTTP tool executed before explicit approval")
			}
			approved := true
			_, err = engine.Run(context.Background(), agent.RunRequest{TaskID: "http-task", MaxSteps: 3, InitialMessages: state.Messages, InitialUsage: state.Usage, ResumeToolCall: &model.ToolCall{ID: pause.CallID, Name: pause.Tool, Arguments: pause.Arguments}, ResumeApproved: &approved, Checkpoint: func(agent.RunResult) error { return nil }})
			if err != nil || calls.Load() != 2 {
				t.Fatalf("approved HTTP tool did not resume: %v", err)
			}
			callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			other, _ := registry.Get("mcp__fixture__other")
			started := time.Now()
			cancelled := other.Execute(callCtx, json.RawMessage(`{}`))
			cancel()
			if !cancelled.IsError || time.Since(started) > time.Second {
				t.Fatal("remote tool ignored cancellation")
			}
			if followup := echo.Execute(context.Background(), json.RawMessage(`{}`)); followup.IsError {
				t.Fatal("call after cancellation failed")
			}
			runtime.Close()
			if deleted.Load() != 1 {
				t.Fatal("session not terminated")
			}
		})
	}
}

func TestHTTPBoundariesAndCancellation(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetHits.Add(1) }))
	defer target.Close()
	for _, mode := range []string{"redirect", "wrong-id", "rpc-error", "truncated-sse", "oversized", "slow"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				switch mode {
				case "redirect":
					http.Redirect(w, r, target.URL, 307)
				case "wrong-id":
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"jsonrpc":"2.0","id":999,"result":{}}`)
				case "rpc-error":
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"fixture-private-token"}}`)
				case "truncated-sse":
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}")
				case "oversized":
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, strings.Repeat(" ", maxHTTPBody+2))
				case "slow":
					select {
					case <-r.Context().Done():
					case <-time.After(300 * time.Millisecond):
					}
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err := StartHTTP(ctx, ServerConfig{URL: server.URL, Bearer: "fixture-private-token", AllowPrivateHosts: true})
			if err == nil || strings.Contains(err.Error(), "fixture-private-token") {
				t.Fatalf("unsafe response handling: %v", err)
			}
		})
	}
	if targetHits.Load() != 0 {
		t.Fatal("redirect forwarded credential")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("private endpoint contacted without opt-in") }))
	defer server.Close()
	if _, err := StartHTTP(context.Background(), ServerConfig{URL: server.URL}); err == nil {
		t.Fatal("private endpoint accepted")
	}
	for _, address := range []string{"127.0.0.1", "::ffff:127.0.0.1", "10.1.2.3", "100.64.0.1", "169.254.169.254", "fc00::1", "::", "224.0.0.1"} {
		if publicIP(netip.MustParseAddr(address)) {
			t.Errorf("private address permitted: %s", address)
		}
	}
}

func TestHTTPRejectsUnsafeCustomAuthenticationHeaders(t *testing.T) {
	for _, config := range []ServerConfig{
		{URL: "https://example.com/mcp", HeaderName: "Authorization", HeaderValue: "token"},
		{URL: "https://example.com/mcp", HeaderName: "Mcp-Session-Id", HeaderValue: "token"},
		{URL: "https://example.com/mcp", HeaderName: "X-Key", HeaderValue: "bad\r\nInjected: yes"},
		{URL: "https://example.com/mcp", HeaderName: "X-Key", HeaderValue: ""},
	} {
		if _, err := StartHTTP(context.Background(), config); err == nil {
			t.Fatalf("unsafe custom header accepted: %+v", config)
		}
	}
}
