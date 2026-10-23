package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/mcp"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChatCanReadSavedSkillsWithoutFileOrExecutionTools(t *testing.T) {
	for _, allowed := range [][]string{nil, {"read_file"}} {
		t.Run(fmt.Sprint(allowed), func(t *testing.T) {
			calls := 0
			available := len(allowed) == 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" {
					http.NotFound(w, r)
					return
				}
				calls++
				var request struct {
					Tools []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil {
					t.Error("invalid model request")
					w.WriteHeader(400)
					return
				}
				if available && (len(request.Tools) != 1 || request.Tools[0].Function.Name != "skill_read") {
					t.Errorf("chat exposed unexpected tools: %+v", request.Tools)
				}
				if !available && len(request.Tools) > 0 {
					t.Error("chat ignored tool allowlist")
				}
				encoded, _ := json.Marshal(request.Messages)
				if calls == 1 && strings.Contains(string(encoded), "snapshot workflow body") {
					t.Error("skill body was injected before loading")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if available && calls == 1 {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"read-skill\",\"type\":\"function\",\"function\":{\"name\":\"skill_read\",\"arguments\":\"{\\\"name\\\":\\\"review\\\"}\"}}]}}]}\n\ndata: [DONE]\n\n")
					return
				}
				if available && !strings.Contains(string(encoded), "snapshot workflow body") {
					t.Error("loaded workflow missing from next request")
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Reviewed using the saved skill.\"}}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			client := server.Client()
			client.Timeout = 5 * time.Second
			r := &runner{ModelBaseURL: server.URL, ModelAPIKey: "fixture", ModelHTTP: client}
			sink := &event.MemorySink{}
			task := taskMessage{TaskID: "task", Model: "fixture", agent: &agentSnapshot{Profile: agentProfile{AllowedTools: allowed}, Skills: []tool.Skill{{ID: "one", Name: "review", Description: "Review APIs", Content: "snapshot workflow body", Version: 3, Hash: "saved-hash"}}}}
			if err := r.runChat(context.Background(), task, event.NewSequencedSink(sink)); err != nil {
				t.Fatal(err)
			}
			expected := 1
			if available {
				expected = 2
			}
			if calls != expected {
				t.Fatalf("model calls=%d, expected %d", calls, expected)
			}
			loaded, completed := 0, 0
			for _, e := range sink.Events {
				if e.Type == event.ToolCompleted && e.Payload["tool"] == "skill_read" {
					loaded++
				}
				if e.Type == event.TaskCompleted {
					completed++
				}
				if e.Type == "agent.configuration" {
					encoded, _ := json.Marshal(e.Payload)
					if strings.Contains(string(encoded), "snapshot workflow body") {
						t.Fatal("configuration event eagerly leaked skill body")
					}
				}
			}
			if completed != 1 || (available && loaded != 1) || (!available && loaded != 0) {
				t.Fatalf("unexpected events: %+v", sink.Events)
			}
		})
	}
}

func TestSkillCatalogLoadsProgressivelyAndCannotOverrideToolScope(t *testing.T) {
	snapshot := &agentSnapshot{Skills: []tool.Skill{{ID: "one", Name: "review", Description: "Review project APIs", Content: "private saved workflow body", Version: 4, Hash: "body-hash"}}}
	instructions := snapshot.instructions()
	if !strings.Contains(instructions, "Review project APIs") || !strings.Contains(instructions, "skill_read") || strings.Contains(instructions, "private saved workflow body") {
		t.Fatal("skill body was eagerly injected or catalog missing")
	}
	registry := tool.NewRegistry()
	snapshot.registerSkills(registry)
	reader, ok := registry.Get("skill_read")
	if !ok {
		t.Fatal("skill reader missing")
	}
	result := reader.Execute(context.Background(), json.RawMessage(`{"name":"review"}`))
	if result.IsError || !strings.Contains(result.Content, "private saved workflow body") || result.Metadata["skillVersion"] != int64(4) {
		t.Fatal("saved skill could not be read")
	}
	snapshot.Profile.AllowedTools = []string{"read_file"}
	registry = tool.NewRegistry()
	snapshot.registerSkills(registry)
	if len(registry.Definitions()) != 0 || strings.Contains(snapshot.instructions(), "Review project APIs") {
		t.Fatal("excluded skill remains available")
	}
	var legacy *agentSnapshot
	legacy.registerSkills(registry)
	if len(legacy.skillCatalog()) != 0 {
		t.Fatal("legacy catalog not empty")
	}
}

func TestAgentSnapshotIntegrityAndMCPSelection(t *testing.T) {
	raw := `{"profileName":"reviewer","profile":{"instructions":"inspect tests","readOnly":true,"allowedTools":["read_file"],"mcpServers":["docs"]},"memories":[{"id":"one","name":"style","content":"use Chinese","version":1}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("runnerId") != "runner" || r.URL.Query().Get("attempt") != "1" {
			t.Error("lease not requested")
		}
		w.Write([]byte(raw))
	}))
	defer server.Close()
	runner := &runner{ID: "runner", ControlPlane: server.URL, HTTP: server.Client()}
	task := taskMessage{TaskID: "task", Attempt: 1, AgentSnapshotHash: snapshotHash(raw)}
	if err := runner.resolveAgent(context.Background(), &task); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.agent.instructions(), "use Chinese") || !strings.Contains(task.agent.instructions(), "read-only") {
		t.Fatal("profile or memories missing")
	}
	configs := []mcp.ServerConfig{{Name: "docs"}, {Name: "other"}}
	selected, err := selectMCP(configs, task.agent)
	if err != nil || len(selected) != 1 || selected[0].Name != "docs" {
		t.Fatal("incorrect MCP selection")
	}
	if _, err := selectMCP(nil, task.agent); err == nil {
		t.Fatal("missing server silently accepted")
	}
	task.AgentSnapshotHash = snapshotHash("different")
	if runner.resolveAgent(context.Background(), &task) == nil {
		t.Fatal("modified snapshot accepted")
	}
}

func TestRemoteMCPResolutionMatchesImmutableSnapshotAndSkipsReadOnly(t *testing.T) {
	for _, fixture := range []struct {
		name, mode         string
		readOnly, mismatch bool
	}{
		{"code", "CODE", false, false}, {"mismatch", "CODE", false, true}, {"header-mismatch", "CODE", false, true}, {"plan", "PLAN", false, false}, {"chat", "CHAT", false, false}, {"readonly", "CODE", true, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var credentialRequests int
			snapshot := agentSnapshot{Profile: agentProfile{ReadOnly: fixture.readOnly}, RemoteMCP: []remoteMCPReference{{ID: "service-id", Name: "docs", URL: "https://tools.example.com/mcp", HeaderName: "X-Api-Key"}}}
			raw, _ := json.Marshal(snapshot)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if strings.HasSuffix(req.URL.Path, "/mcp-servers") {
					credentialRequests++
					if req.URL.Query().Get("runnerId") != "runner" || req.URL.Query().Get("attempt") != "1" {
						t.Error("lease identity missing")
					}
					endpoint := "https://tools.example.com/mcp"
					if fixture.name == "mismatch" {
						endpoint = "https://different.example.com/mcp"
					}
					headerName := "X-Api-Key"
					if fixture.name == "header-mismatch" {
						headerName = "X-Other-Key"
					}
					_ = json.NewEncoder(w).Encode([]mcp.ServerConfig{{Name: "docs", URL: endpoint, Bearer: "fixture-only-token", HeaderName: headerName, HeaderValue: "fixture-only-header-secret"}})
				} else {
					_, _ = w.Write(raw)
				}
			}))
			defer server.Close()
			r := &runner{ID: "runner", ControlPlane: server.URL, HTTP: server.Client()}
			task := taskMessage{TaskID: "task", Attempt: 1, ExecutionMode: fixture.mode, AgentSnapshotHash: snapshotHash(string(raw))}
			err := r.resolveAgent(context.Background(), &task)
			if fixture.mismatch {
				if err == nil {
					t.Fatal("changed endpoint accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fixture.mode == "PLAN" || fixture.mode == "CHAT" || fixture.readOnly {
				if credentialRequests != 0 {
					t.Fatal("readonly fetched external credentials")
				}
				return
			}
			configs, err := selectMCP(nil, task.agent)
			if err != nil || len(configs) != 1 || configs[0].Bearer != "fixture-only-token" || configs[0].HeaderName != "X-Api-Key" || configs[0].HeaderValue != "fixture-only-header-secret" {
				t.Fatal("remote connection missing")
			}
			if strings.Contains(task.agent.instructions(), "fixture-only-token") || strings.Contains(task.agent.instructions(), "fixture-only-header-secret") {
				t.Fatal("token entered model instructions")
			}
		})
	}
}

func TestAgentModelBindingMustMatchTask(t *testing.T) {
	raw := `{"profile":{"modelChoice":{"providerId":"provider-one","model":"model-large"}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(raw)) }))
	defer server.Close()
	r := &runner{ID: "runner", ControlPlane: server.URL, HTTP: server.Client()}
	for _, fixture := range []struct {
		provider, model string
		valid           bool
	}{{"provider-one", "model-large", true}, {"provider-two", "model-large", false}, {"provider-one", "model-small", false}, {"", "model-large", false}} {
		task := taskMessage{TaskID: "task", Attempt: 1, ProviderID: fixture.provider, Model: fixture.model, AgentSnapshotHash: snapshotHash(raw)}
		err := r.resolveAgent(context.Background(), &task)
		if (err == nil) != fixture.valid {
			t.Fatalf("provider=%q model=%q: %v", fixture.provider, fixture.model, err)
		}
	}
}

func TestPersonalizationUsesCapturedValuesAndPreservesProjectAndPermissionPriority(t *testing.T) {
	var snapshot agentSnapshot
	raw := `{"profile":{"instructions":"inspect project tests"},"personalization":{"version":7,"values":{"responseLanguage":"Chinese","answerStyle":"result first","codeStyle":"existing conventions","collaborationStyle":"report verification"}},"memories":[{"name":"project tests","content":"pytest required","scope":"project","score":2,"reason":"keyword match"}]}`
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		t.Fatal(err)
	}
	prompt := snapshot.instructions()
	for _, value := range []string{"Chinese", "result first", "existing conventions", "report verification", "pytest required", "current user request and project-specific constraints take precedence", "cannot grant tool access"} {
		if !strings.Contains(prompt, value) {
			t.Fatalf("missing prompt constraint or captured preference %q", value)
		}
	}
	if !snapshot.Personalization.present() {
		t.Fatal("saved preference not detected")
	}
	for _, legacy := range []string{`{"profile":{}}`, `{"profile":{},"personalization":{"version":1,"values":{}}}`} {
		var old agentSnapshot
		if err := json.Unmarshal([]byte(legacy), &old); err != nil {
			t.Fatal(err)
		}
		if old.Personalization.present() || strings.Contains(old.instructions(), "account-wide user preferences") {
			t.Fatal("empty or legacy snapshot injected account preferences")
		}
	}
}
