package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesToolProtocolAndCachedUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Error("wrong wire API")
		}
		var payload map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if string(payload["store"]) != "false" || string(payload["max_output_tokens"]) != "256" || payload["previous_response_id"] != nil {
			t.Error("storage or output policy incorrect")
		}
		if !strings.Contains(string(payload["input"]), "function_call_output") || !strings.Contains(string(payload["input"]), "call-1") {
			t.Error("tool history lost")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"type":"response.output_text.delta","delta":"done"}`)
		fmt.Fprintln(w, `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"done"}]},{"type":"function_call","call_id":"call-2","name":"read_file","arguments":"{\"path\":\"main.go\"}"}],"usage":{"input_tokens":100,"output_tokens":10,"input_tokens_details":{"cached_tokens":80}}}}`)
	}))
	defer server.Close()
	stream := ""
	result, err := (&OpenAICompatible{BaseURL: server.URL, WireAPI: "responses"}).Chat(context.Background(), Request{Model: "configured", MaxTokens: 256, Messages: []Message{{Role: RoleUser, Content: "inspect"}, {Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "list_files", Arguments: json.RawMessage(`{}`)}}}, {Role: RoleTool, ToolCallID: "call-1", Content: "[]"}}}, func(s string) { stream += s })
	if err != nil || stream != "done" || result.Content != "done" || len(result.ToolCalls) != 1 || result.Usage.CachedInputTokens != 80 || !result.Usage.CacheReported {
		t.Fatalf("response=%+v err=%v", result, err)
	}
}
func TestResponsesRejectsPartialAndScrubsProviderErrors(t *testing.T) {
	for _, body := range []string{`data: {"type":"response.output_text.delta","delta":"partial"}`, `data: {"type":"response.failed","response":{"error":{"message":"secret-key"}}}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, body) }))
		_, err := (&OpenAICompatible{BaseURL: s.URL, WireAPI: "responses"}).Chat(context.Background(), Request{Model: "test", Messages: []Message{{Role: RoleUser, Content: "test"}}}, nil)
		s.Close()
		if err == nil || strings.Contains(err.Error(), "secret-key") {
			t.Fatal("incomplete/error handling failed")
		}
	}
}
