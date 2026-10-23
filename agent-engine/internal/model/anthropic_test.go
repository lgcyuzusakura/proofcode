package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const anthropicStart = `{"type":"message_start","message":{"type":"message","content":[],"usage":{"input_tokens":20,"output_tokens":0,"cache_read_input_tokens":60,"cache_creation_input_tokens":10}}}`

var anthropicToolEvents = []string{
	anthropicStart,
	`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"查看文件"}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool-2","name":"read_file","input":{}}}`,
	`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
	`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"main.go\"}"}}`,
	`{"type":"content_block_stop","index":1}`,
	`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":12}}`,
	`{"type":"message_stop"}`,
}

func TestAnthropicStreamHistoryAndCachePricing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "fixture-key" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
			t.Error("incorrect endpoint or authentication")
		}
		var payload struct {
			System   string             `json:"system"`
			Messages []anthropicMessage `json:"messages"`
			Max      int                `json:"max_tokens"`
			Tools    []map[string]any   `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.System != "project rules" || payload.Max != 32 || len(payload.Tools) != 1 || payload.Tools[0]["input_schema"] == nil {
			t.Errorf("incorrect request: %+v", payload)
		}
		if len(payload.Messages) != 3 || len(payload.Messages[2].Content) != 2 || !payload.Messages[2].Content[0].IsError || payload.Messages[2].Content[1].ToolUseID != "prior-2" {
			t.Errorf("tool history not preserved: %+v", payload.Messages)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range anthropicToolEvents {
			fmt.Fprintf(w, "event: ignored\ndata: %s\n\n", event)
		}
	}))
	defer server.Close()
	settings := DefaultSettings()
	read, write := 0.1, 1.25
	settings.Prices = []PriceTier{{Input: 1, Output: 2, CacheRead: &read, CacheWrite: &write}}
	p := &OpenAICompatible{BaseURL: server.URL + "/v1", APIKey: "fixture-key", WireAPI: "anthropic", Settings: &settings}
	text := ""
	result, err := p.Chat(context.Background(), Request{Model: "fixture", MaxTokens: 32, Tools: []ToolDefinition{{Name: "read_file", Parameters: map[string]any{"type": "object"}}}, Messages: []Message{
		{Role: RoleSystem, Content: "project rules"}, {Role: RoleUser, Content: "inspect"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "prior", Name: "read_file", Arguments: json.RawMessage(`{}`)}, {ID: "prior-2", Name: "read_file", Arguments: json.RawMessage(`{}`)}}},
		{Role: RoleTool, ToolCallID: "prior", Content: `{"isError":true}`}, {Role: RoleTool, ToolCallID: "prior-2", Content: "ok"},
	}}, func(s string) { text += s })
	if err != nil {
		t.Fatal(err)
	}
	if text != "查看文件" || result.Content != text || len(result.ToolCalls) != 1 || string(result.ToolCalls[0].Arguments) != `{"path":"main.go"}` {
		t.Fatalf("incorrect response: %+v", result)
	}
	u := result.Usage
	if !u.Reported || u.InputTokens != 90 || u.OutputTokens != 12 || !u.CacheReported || u.CachedInputTokens != 60 || !u.CacheWriteReported || u.CacheWriteTokens != 10 || u.FirstTokenMs == nil || u.EstimatedCost == nil {
		t.Fatalf("incorrect usage: %+v", u)
	}
	if delta := *u.EstimatedCost - 0.0000625; delta > 1e-12 || delta < -1e-12 {
		t.Fatalf("cost=%v", *u.EstimatedCost)
	}
}

func TestAnthropicRejectsIncompleteAndInvalidTools(t *testing.T) {
	cases := map[string][]string{
		"missing stop":  anthropicToolEvents[:len(anthropicToolEvents)-1],
		"invalid order": {anthropicStart, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`},
		"secret error":  {`{"type":"error","error":{"message":"secret-key"}}`},
		"invalid index": {anthropicStart, `{"type":"content_block_start","index":-1,"content_block":{"type":"text"}}`},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			stream := ""
			for _, event := range events {
				stream += "data: " + event + "\n\n"
			}
			result, err := readAnthropicStream(context.Background(), strings.NewReader(stream), nil)
			if err == nil || strings.Contains(err.Error(), "secret-key") || len(result.ToolCalls) > 0 {
				t.Fatalf("unsafe partial response: %+v %v", result, err)
			}
		})
	}
	for _, body := range []string{
		`{"type":"message","stop_reason":"max_tokens","content":[{"type":"tool_use","id":"a","name":"write","input":{}}]}`,
		`{"type":"message","stop_reason":"tool_use","content":[{"type":"tool_use","id":"a","name":"write","input":"oops"}]}`,
		`{"type":"message","stop_reason":"tool_use","content":[{"type":"tool_use","id":"a","name":"write","input":{}},{"type":"tool_use","id":"a","name":"write","input":{}}]}`,
	} {
		var envelope anthropicEnvelope
		_ = json.Unmarshal([]byte(body), &envelope)
		if _, err := anthropicResult(envelope); err == nil {
			t.Fatal("invalid completion accepted")
		}
	}
}

func TestAnthropicJSONUnknownUsageAndHTTPErrorRedaction(t *testing.T) {
	for _, status := range []int{200, 401, 403, 429, 529} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status == 200 {
					fmt.Fprint(w, `{"type":"message","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`)
				} else {
					fmt.Fprint(w, `{"error":{"message":"secret-key"}}`)
				}
			}))
			defer server.Close()
			result, err := (&OpenAICompatible{BaseURL: server.URL, WireAPI: "anthropic"}).Chat(context.Background(), Request{Model: "fixture", Messages: []Message{{Role: RoleUser, Content: "go"}}}, nil)
			if status == 200 {
				if err != nil || result.Content != "done" || result.Usage.Reported || result.Usage.CacheReported || result.Usage.EstimatedCost != nil {
					t.Fatalf("unknown usage fabricated: %+v %v", result, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "secret-key") || !strings.Contains(err.Error(), fmt.Sprint(status)) {
				t.Fatalf("incorrect error: %v", err)
			}
		})
	}
}

func TestAnthropicCancellationAndRedirect(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", anthropicStart)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		defer cancel()
		_, err := (&OpenAICompatible{BaseURL: server.URL, WireAPI: "anthropic"}).Chat(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "go"}}}, nil)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancellation lost: %v", err)
		}
	})
	t.Run("redirect", func(t *testing.T) {
		hit := false
		destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
		defer destination.Close()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
		}))
		defer server.Close()
		_, err := (&OpenAICompatible{BaseURL: server.URL, WireAPI: "anthropic", APIKey: "secret-key"}).Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "go"}}}, nil)
		if err == nil || hit {
			t.Fatal("redirect followed")
		}
	})
}
