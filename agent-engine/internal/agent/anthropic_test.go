package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

func TestAnthropicAgentToolRoundTrip(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var input struct {
			Messages []struct {
				Role    string `json:"role"`
				Content []struct {
					Type    string `json:"type"`
					ID      string `json:"tool_use_id"`
					Content string `json:"content"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			fmt.Fprint(w, `{"type":"message","stop_reason":"tool_use","content":[{"type":"tool_use","id":"call-native","name":"echo","input":{}}],"usage":{"input_tokens":10,"output_tokens":4}}`)
		} else {
			last := input.Messages[len(input.Messages)-1]
			if last.Role != "user" || len(last.Content) != 1 || last.Content[0].Type != "tool_result" || last.Content[0].ID != "call-native" || last.Content[0].Content != `{"content":"ok","isError":false}` {
				t.Errorf("tool result protocol lost: %+v", last)
			}
			fmt.Fprint(w, `{"type":"message","stop_reason":"end_turn","content":[{"type":"text","text":"已核对"}],"usage":{"input_tokens":30,"output_tokens":5}}`)
		}
	}))
	defer server.Close()
	sink := &event.MemorySink{}
	engine := Agent{Provider: &model.OpenAICompatible{BaseURL: server.URL, WireAPI: "anthropic"}, Tools: tool.NewRegistry(echoTool{}), Approval: AutomaticApproval{}, Events: event.NewSequencedSink(sink)}
	result, err := engine.Run(context.Background(), RunRequest{TaskID: "native-protocol", SystemPrompt: "inspect", Prompt: "go", Model: "fixture", MaxSteps: 3})
	if err != nil || calls != 2 || result.Content != "已核对" || result.Usage.InputTokens != 40 || result.Usage.OutputTokens != 9 || !result.Usage.Reported {
		t.Fatalf("round trip failed: %+v %v calls=%d", result, err, calls)
	}
	completed := 0
	for _, e := range sink.Events {
		if e.Type == event.ToolCompleted {
			completed++
		}
	}
	if completed != 1 {
		t.Fatalf("tool audit missing: %d", completed)
	}
}

func TestAnthropicPartialToolNeverExecutes(t *testing.T) {
	executed := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"type":"message","stop_reason":"max_tokens","content":[{"type":"tool_use","id":"partial","name":"tracked_write","input":{}}]}`)
	}))
	defer server.Close()
	engine := Agent{Provider: &model.OpenAICompatible{BaseURL: server.URL, WireAPI: "anthropic"}, Tools: tool.NewRegistry(trackedWriteTool{calls: &executed}), Approval: AutomaticApproval{AllowWrite: true, AllowExec: true}, Events: event.NewSequencedSink(&event.MemorySink{})}
	_, err := engine.Run(context.Background(), RunRequest{TaskID: "partial-native", Prompt: "go", Model: "fixture"})
	if err == nil || executed != 0 {
		t.Fatalf("partial tool executed: err=%v count=%d", err, executed)
	}
}
