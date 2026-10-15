package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/config"
)

func TestOpenAICompatibleAssemblesStreamAndPreservesUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected request: %s, Authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var request struct {
			Messages  []json.RawMessage `json:"messages"`
			Tools     []json.RawMessage `json:"tools"`
			MaxTokens int               `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) != 1 || len(request.Tools) != 1 || request.MaxTokens != 512 {
			t.Errorf("request = %+v, err = %v", request, err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Inspecting "}}],"usage":{"prompt_tokens":12,"completion_tokens":4}}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"now."},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer server.Close()

	var deltas []string
	provider := &OpenAICompatible{BaseURL: server.URL + "/v1"}
	response, err := provider.Chat(context.Background(), Request{
		Model: "local", Messages: []Message{{Role: RoleUser, Content: "inspect"}},
		Tools:     []ToolDefinition{{Name: "read_file", Parameters: map[string]any{"type": "object"}}},
		MaxTokens: 512,
	}, func(delta string) { deltas = append(deltas, delta) })
	if err != nil || response.Content != "Inspecting now." || response.Usage.InputTokens != 12 || response.Usage.OutputTokens != 4 || len(deltas) != 2 {
		t.Fatalf("response = %+v, deltas = %v, error = %v", response, deltas, err)
	}
}

func TestOpenAICompatibleCountsToolDefinitionsInBudget(t *testing.T) {
	content := strings.Repeat("x", (config.MaxTotalTokens-config.DefaultMaxOutputTokens-1)*4)
	provider := &OpenAICompatible{BaseURL: "http://127.0.0.1:1/v1"}
	_, err := provider.Chat(context.Background(), Request{
		Model: "test", Messages: []Message{{Role: RoleUser, Content: content}},
		Tools: []ToolDefinition{{Name: "read_file", Description: "Read a file", Parameters: map[string]any{"type": "object"}}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "超过 200000 token 总预算") {
		t.Fatalf("budget error = %v", err)
	}
}

func TestOpenAICompatibleRejectsIncompleteStreams(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"disconnect", "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n", "before a complete response"},
		{"length limit", "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"length\"}]}\n", "finish_reason"},
		{"empty", "data: [DONE]\n", "before a complete response"},
		{"provider error", "data: {\"error\":{\"message\":\"capacity exceeded\"}}\n", "capacity exceeded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, err := (&OpenAICompatible{BaseURL: server.URL}).Chat(context.Background(), Request{Model: "test", Messages: []Message{{Role: RoleUser, Content: "go"}}}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestOpenAICompatibleAssemblesSparseToolIndices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":3,"id":"call-b","function":{"name":"run_","arguments":"{\"program\":\"go\""}},{"index":1,"id":"call-a","function":{"name":"read_file","arguments":"{}"}}]}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":3,"function":{"name":"command","arguments":"}"}}]},"finish_reason":"tool_calls"}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer server.Close()
	response, err := (&OpenAICompatible{BaseURL: server.URL}).Chat(context.Background(), Request{Model: "test", Messages: []Message{{Role: RoleUser, Content: "go"}}}, nil)
	if err != nil || len(response.ToolCalls) != 2 || response.ToolCalls[0].Name != "read_file" || response.ToolCalls[1].Name != "run_command" || string(response.ToolCalls[1].Arguments) != `{"program":"go"}` {
		t.Fatalf("response = %+v, error = %v", response, err)
	}
}

func TestOpenAICompatibleBoundsModelRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer server.Close()
	start := time.Now()
	_, err := (&OpenAICompatible{BaseURL: server.URL, Timeout: 20 * time.Millisecond}).Chat(context.Background(), Request{Model: "test", Messages: []Message{{Role: RoleUser, Content: "go"}}}, nil)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("timeout error = %v, elapsed = %s", err, time.Since(start))
	}
}
