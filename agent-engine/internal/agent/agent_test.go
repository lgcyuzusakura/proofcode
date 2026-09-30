package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

type scriptedProvider struct {
	responses []model.Response
	index     int
}

func (p *scriptedProvider) Chat(_ context.Context, _ model.Request, onDelta func(string)) (model.Response, error) {
	value := p.responses[p.index]
	p.index++
	if value.Content != "" {
		onDelta(value.Content)
	}
	return value, nil
}

type echoTool struct{}

func (echoTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "echo", Description: "echo", Parameters: map[string]any{"type": "object"}}
}
func (echoTool) Risk(json.RawMessage) tool.Risk { return tool.RiskRead }
func (echoTool) Execute(context.Context, json.RawMessage) tool.Result {
	return tool.Result{Content: "ok"}
}

func TestAgentExecutesToolThenCompletes(t *testing.T) {
	provider := &scriptedProvider{responses: []model.Response{{ToolCalls: []model.ToolCall{{ID: "1", Name: "echo", Arguments: json.RawMessage(`{}`)}}}, {Content: "done"}}}
	memory := &event.MemorySink{}
	value := Agent{Provider: provider, Tools: tool.NewRegistry(echoTool{}), Approval: AutomaticApproval{}, Events: event.NewSequencedSink(memory)}
	result, err := value.Run(context.Background(), RunRequest{TaskID: "task", Prompt: "go", Model: "mock", MaxSteps: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "done" {
		t.Fatalf("unexpected result %q", result.Content)
	}
	if provider.index != 2 {
		t.Fatalf("expected 2 model calls, got %d", provider.index)
	}
}
