package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

func TestConversationHistoryDoesNotConsumeCurrentTurnSteps(t *testing.T) {
	history := []model.Message{{Role: model.RoleUser, Content: "older request"}, {Role: model.RoleAssistant, Content: "older answer"}}
	provider := &scriptedProvider{responses: []model.Response{{Content: "current answer", Usage: model.Usage{Reported: true}}}}
	engine := Agent{Provider: provider, Tools: tool.NewRegistry(), Approval: AutomaticApproval{}, Events: event.NewSequencedSink(&event.MemorySink{})}
	result, err := engine.Run(context.Background(), RunRequest{SystemPrompt: "system", Prompt: "current request", HistoryMessages: history, MaxSteps: 1})
	if err != nil || result.Content != "current answer" || len(provider.requests) != 1 || !result.Usage.Reported {
		t.Fatalf("new turn exhausted by older history: %+v %v", result, err)
	}
	if len(provider.requests[0].Messages) != 4 || provider.requests[0].Messages[2].Content != "older answer" {
		t.Fatal("new turn omitted its conversation history")
	}
}

func TestResumedStepsExcludePreviousConversationTurns(t *testing.T) {
	initial := []model.Message{{Role: model.RoleSystem, Content: "system"}, {Role: model.RoleUser, Content: "older request"}, {Role: model.RoleAssistant, Content: "older answer"}, {Role: model.RoleUser, Content: "current request"}, {Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "current-call", Name: "echo", Arguments: json.RawMessage(`{}`)}}}, {Role: model.RoleTool, ToolCallID: "current-call", Content: "result"}}
	provider := &scriptedProvider{responses: []model.Response{{Content: "finished"}}}
	engine := Agent{Provider: provider, Tools: tool.NewRegistry(), Approval: AutomaticApproval{}, Events: event.NewSequencedSink(&event.MemorySink{})}
	result, err := engine.Run(context.Background(), RunRequest{InitialMessages: initial, MaxSteps: 2, PrepareMessages: func(_ context.Context, step int, messages []model.Message) ([]model.Message, error) {
		if step != 2 {
			t.Fatalf("resumed current step=%d, want 2", step)
		}
		return messages, nil
	}})
	if err != nil || result.Content != "finished" || len(provider.requests) != 1 {
		t.Fatalf("resume counted previous conversation: %+v %v", result, err)
	}
	engine.Provider = &failingProvider{}
	if _, err = engine.Run(context.Background(), RunRequest{InitialMessages: initial, MaxSteps: 1}); err == nil {
		t.Fatal("resume reset the current task's consumed step budget")
	}
}
