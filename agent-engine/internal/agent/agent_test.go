package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/decision"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
)

type scriptedProvider struct {
	responses []model.Response
	index     int
	requests  []model.Request
	afterChat func()
}

func (p *scriptedProvider) Chat(_ context.Context, request model.Request, onDelta func(string)) (model.Response, error) {
	p.requests = append(p.requests, request)
	value := p.responses[p.index]
	p.index++
	if p.afterChat != nil {
		p.afterChat()
	}
	if value.Content != "" {
		onDelta(value.Content)
	}
	return value, nil
}

type fixedRouter struct {
	choice     string
	confidence float64
	err        error
}

func (r fixedRouter) Choose(_ context.Context, _ string, _ []decision.Option) (decision.ChoiceResult, error) {
	return decision.ChoiceResult{Choice: r.choice, Confidence: r.confidence, Probabilities: map[string]float64{r.choice: 1}, Model: "mock"}, r.err
}

func TestToolRoutingIsOptionalAndFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       string
		confidence float64
		wantTools  int
		wantCalls  int
		responses  []model.Response
	}{
		{name: "observe", mode: "observe", confidence: 0.99, wantTools: 2, wantCalls: 1, responses: []model.Response{{Content: "done"}}},
		{name: "low confidence", mode: "route", confidence: 0.2, wantTools: 2, wantCalls: 1, responses: []model.Response{{Content: "done"}}},
		{name: "route", mode: "route", confidence: 0.99, wantTools: 1, wantCalls: 1, responses: []model.Response{{Content: "done"}}},
		{name: "wrong model tool", mode: "route", confidence: 0.99, wantTools: 1, wantCalls: 2, responses: []model.Response{{Content: "discarded", ToolCalls: []model.ToolCall{{ID: "unexpected", Name: "write_test", Arguments: json.RawMessage(`{}`)}}}, {Content: "done"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &scriptedProvider{responses: tc.responses}
			memory := &event.MemorySink{}
			engine := Agent{Provider: provider, Tools: tool.NewRegistry(echoTool{}, writeTestTool{}), Approval: AutomaticApproval{}, Events: event.NewSequencedSink(memory), Router: fixedRouter{choice: "echo", confidence: tc.confidence}, Routing: ToolRoutingPolicy{Mode: tc.mode, MinConfidence: 0.85}}
			if _, err := engine.Run(context.Background(), RunRequest{TaskID: "routing", Prompt: "inspect", Model: "mock"}); err != nil {
				t.Fatal(err)
			}
			if len(provider.requests) != tc.wantCalls || len(provider.requests[0].Tools) != tc.wantTools {
				t.Fatalf("model calls=%d initial tools=%d", len(provider.requests), len(provider.requests[0].Tools))
			}
			if tc.wantCalls == 2 && len(provider.requests[1].Tools) != 2 {
				t.Fatalf("fallback did not restore full tool set: %+v", provider.requests[1].Tools)
			}
			if tc.name == "wrong model tool" {
				var deltas string
				for _, value := range memory.Events {
					if value.Type == event.MessageDelta {
						deltas += value.Payload["delta"].(string)
					}
				}
				if deltas != "done" {
					t.Fatalf("fallback streamed discarded response: %q", deltas)
				}
			}
		})
	}
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

func TestAgentPausesAndResumesApprovedToolCall(t *testing.T) {
	call := model.ToolCall{ID: "write-1", Name: "write_test", Arguments: json.RawMessage(`{"value":"ok"}`)}
	provider := &scriptedProvider{responses: []model.Response{{ToolCalls: []model.ToolCall{call}}, {Content: "done"}}}
	memory := &event.MemorySink{}
	registry := tool.NewRegistry(writeTestTool{})
	first := Agent{Provider: provider, Tools: registry, Approval: AutomaticApproval{}, Events: event.NewSequencedSink(memory)}
	paused, err := first.Run(context.Background(), RunRequest{TaskID: "task", Prompt: "edit", Model: "mock"})
	var approvalErr *ApprovalRequiredError
	if !errors.As(err, &approvalErr) || approvalErr.CallID != call.ID || len(paused.Messages) != 3 {
		t.Fatalf("expected paused tool call with messages, got result=%+v err=%v", paused, err)
	}
	if provider.index != 1 {
		t.Fatalf("model advanced before approval: %d", provider.index)
	}
	approved := true
	resumed := Agent{Provider: provider, Tools: registry, Approval: AutomaticApproval{AllowWrite: true}, Events: event.NewSequencedSink(memory)}
	result, err := resumed.Run(context.Background(), RunRequest{TaskID: "task", Model: "mock", InitialMessages: paused.Messages, ResumeToolCall: &call, ResumeApproved: &approved, Checkpoint: func(RunResult) error { return nil }})
	if err != nil || result.Content != "done" || provider.index != 2 {
		t.Fatalf("resume failed: result=%+v err=%v", result, err)
	}
	if len(result.Messages) < 5 || result.Messages[3].Role != model.RoleTool {
		t.Fatalf("resumed tool result missing: %+v", result.Messages)
	}
}

type writeTestTool struct{}

func (writeTestTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "write_test", Parameters: map[string]any{"type": "object"}}
}
func (writeTestTool) Risk(json.RawMessage) tool.Risk { return tool.RiskWrite }
func (writeTestTool) Execute(context.Context, json.RawMessage) tool.Result {
	return tool.Result{Content: "written"}
}

type trackedWriteTool struct{ calls *int }

func (trackedWriteTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "tracked_write", Parameters: map[string]any{"type": "object"}}
}
func (trackedWriteTool) Risk(json.RawMessage) tool.Risk { return tool.RiskWrite }
func (t trackedWriteTool) Execute(context.Context, json.RawMessage) tool.Result {
	*t.calls++
	return tool.Result{Content: "written"}
}

func TestAgentPersistsToolIntentBeforeExecution(t *testing.T) {
	call := model.ToolCall{ID: "write-1", Name: "tracked_write", Arguments: json.RawMessage(`{}`)}
	provider := &scriptedProvider{responses: []model.Response{{ToolCalls: []model.ToolCall{call}}}}
	count := 0
	engine := Agent{Provider: provider, Tools: tool.NewRegistry(trackedWriteTool{calls: &count}), Approval: AutomaticApproval{AllowWrite: true}, Events: event.NewSequencedSink(&event.MemorySink{})}
	journalErr := errors.New("journal unavailable")
	_, err := engine.Run(context.Background(), RunRequest{TaskID: "task", Prompt: "write", Model: "mock", BeforeTool: func(state RunResult, pending model.ToolCall) error {
		if pending.ID != call.ID || len(state.Messages) != 3 {
			t.Fatalf("journal did not receive the pending model call: %+v %+v", state, pending)
		}
		return journalErr
	}})
	if !errors.Is(err, journalErr) || count != 0 || provider.index != 1 {
		t.Fatalf("tool ran without a durable intent: err=%v calls=%d modelCalls=%d", err, count, provider.index)
	}
}

func TestAgentResumesRemainingCallsAfterCheckpoint(t *testing.T) {
	firstCall := model.ToolCall{ID: "write-1", Name: "tracked_write", Arguments: json.RawMessage(`{}`)}
	secondCall := model.ToolCall{ID: "write-2", Name: "tracked_write", Arguments: json.RawMessage(`{}`)}
	provider := &scriptedProvider{responses: []model.Response{{ToolCalls: []model.ToolCall{firstCall, secondCall}}, {Content: "done"}}}
	count := 0
	registry := tool.NewRegistry(trackedWriteTool{calls: &count})
	memory := &event.MemorySink{}
	engine := Agent{Provider: provider, Tools: registry, Approval: AutomaticApproval{AllowWrite: true}, Events: event.NewSequencedSink(memory)}
	interrupted := errors.New("process stopped after checkpoint")
	var persisted RunResult
	_, err := engine.Run(context.Background(), RunRequest{TaskID: "task", Prompt: "write twice", Model: "mock", Checkpoint: func(state RunResult) error {
		persisted = state
		return interrupted
	}})
	if !errors.Is(err, interrupted) || count != 1 || len(persisted.RemainingCalls) != 1 || persisted.RemainingCalls[0].ID != secondCall.ID {
		t.Fatalf("first tool checkpoint lost pending calls: err=%v calls=%d state=%+v", err, count, persisted)
	}
	resumed := Agent{Provider: provider, Tools: registry, Approval: AutomaticApproval{AllowWrite: true}, Events: event.NewSequencedSink(memory)}
	result, err := resumed.Run(context.Background(), RunRequest{TaskID: "task", Model: "mock", InitialMessages: persisted.Messages, RemainingCalls: persisted.RemainingCalls, Checkpoint: func(RunResult) error { return nil }})
	if err != nil || result.Content != "done" || count != 2 || provider.index != 2 {
		t.Fatalf("resume failed: err=%v result=%+v toolCalls=%d modelCalls=%d", err, result, count, provider.index)
	}
	if len(provider.requests[1].Messages) != 5 || provider.requests[1].Messages[4].ToolCallID != secondCall.ID {
		t.Fatalf("model saw incomplete tool-call exchange: %+v", provider.requests[1].Messages)
	}
}

type rejectingSink struct{ reject event.Type }

func (s rejectingSink) Publish(_ context.Context, value event.Event) error {
	if value.Type == s.reject {
		return errors.New("audit store unavailable")
	}
	return nil
}

func TestAgentDoesNotExecuteToolWhenAuditFails(t *testing.T) {
	for _, rejected := range []event.Type{event.ToolRequested, event.ToolStarted} {
		t.Run(string(rejected), func(t *testing.T) {
			call := model.ToolCall{ID: "write-1", Name: "tracked_write", Arguments: json.RawMessage(`{}`)}
			provider := &scriptedProvider{responses: []model.Response{{ToolCalls: []model.ToolCall{call}}}}
			count := 0
			engine := Agent{Provider: provider, Tools: tool.NewRegistry(trackedWriteTool{calls: &count}), Approval: AutomaticApproval{AllowWrite: true}, Events: event.NewSequencedSink(rejectingSink{reject: rejected})}
			_, err := engine.Run(context.Background(), RunRequest{TaskID: "task", Prompt: "write", Model: "mock"})
			if err == nil || count != 0 {
				t.Fatalf("tool executed without durable %s event: err=%v calls=%d", rejected, err, count)
			}
		})
	}
}

func TestAgentDoesNotExecuteToolAfterCancellation(t *testing.T) {
	call := model.ToolCall{ID: "write-1", Name: "tracked_write", Arguments: json.RawMessage(`{}`)}
	count := 0
	ctx, cancel := context.WithCancel(context.Background())
	provider := &scriptedProvider{responses: []model.Response{{ToolCalls: []model.ToolCall{call}}}, afterChat: cancel}
	engine := Agent{Provider: provider, Tools: tool.NewRegistry(trackedWriteTool{calls: &count}), Approval: AutomaticApproval{AllowWrite: true}, Events: event.NewSequencedSink(&event.MemorySink{})}
	_, err := engine.Run(ctx, RunRequest{TaskID: "task", Prompt: "write", Model: "mock"})
	if !errors.Is(err, context.Canceled) || count != 0 {
		t.Fatalf("tool ran after model response was cancelled: err=%v calls=%d", err, count)
	}

	resumeCtx, cancelResume := context.WithCancel(context.Background())
	cancelResume()
	approved := true
	_, err = engine.Run(resumeCtx, RunRequest{TaskID: "task", Model: "mock", InitialMessages: []model.Message{{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{call}}}, ResumeToolCall: &call, ResumeApproved: &approved, Checkpoint: func(RunResult) error { return nil }})
	if !errors.Is(err, context.Canceled) || count != 0 {
		t.Fatalf("approved tool ran after cancellation: err=%v calls=%d", err, count)
	}
}

func TestAgentChangesCodeAndRunsFocusedTest(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain is unavailable")
	}
	root := t.TempDir()
	files := map[string]string{
		"go.mod":       "module example.com/calc\n\ngo 1.24.0\n",
		"calc.go":      "package calc\n\nfunc Add(a, b int) int { return 0 }\n",
		"calc_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"wrong sum\") } }\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{responses: []model.Response{
		{ToolCalls: []model.ToolCall{{ID: "read", Name: "read_file", Arguments: json.RawMessage(`{"path":"calc.go"}`)}}},
		{ToolCalls: []model.ToolCall{{ID: "edit", Name: "apply_patch", Arguments: json.RawMessage(`{"edits":[{"path":"calc.go","old_text":"return 0","new_text":"return a + b"}]}`)}}},
		{ToolCalls: []model.ToolCall{{ID: "test", Name: "run_command", Arguments: json.RawMessage(`{"program":"go","args":["test","./..."]}`)}}},
		{Content: "Changed Add and verified go test ./..."},
	}}
	engine := Agent{Provider: provider, Tools: tool.NewRegistry(tool.ReadFile{Workspace: ws}, tool.ApplyPatch{Workspace: ws}, tool.RunCommand{Workspace: ws}), Approval: AutomaticApproval{AllowWrite: true, AllowExec: true}, Events: event.NewSequencedSink(&event.MemorySink{})}
	result, err := engine.Run(context.Background(), RunRequest{TaskID: "code-task", Prompt: "Fix Add", Model: "mock"})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := os.ReadFile(filepath.Join(root, "calc.go"))
	if err != nil || !strings.Contains(string(changed), "return a + b") {
		t.Fatalf("source was not changed: %s, %v", changed, err)
	}
	if len(result.Messages) < 8 || !strings.Contains(result.Messages[7].Content, `"exitCode":0`) {
		t.Fatalf("focused test did not pass: %+v", result.Messages)
	}
}
