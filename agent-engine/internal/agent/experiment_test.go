package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/experiment"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

type failingProvider struct {
	response model.Response
	err      error
	calls    int
}

func (p *failingProvider) Chat(context.Context, model.Request, func(string)) (model.Response, error) {
	p.calls++
	return p.response, p.err
}

func TestRequiredJevNeverFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name   string
		router ToolRouter
		mode   string
	}{
		{"unavailable", nil, "route"},
		{"router failure", fixedRouter{err: errors.New("service unavailable")}, "route"},
		{"wrong mode", fixedRouter{choice: "echo", confidence: 1}, "observe"},
		{"low confidence", fixedRouter{choice: "echo", confidence: 0.1}, "route"},
		{"defer", fixedRouter{choice: "defer", confidence: 1}, "route"},
		{"unknown tool", fixedRouter{choice: "missing", confidence: 1}, "route"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &failingProvider{response: model.Response{Content: "must not run"}}
			engine := Agent{Provider: p, Tools: tool.NewRegistry(echoTool{}, writeTestTool{}), Approval: AutomaticApproval{}, Events: event.NewSequencedSink(&event.MemorySink{}), Router: tc.router, Routing: ToolRoutingPolicy{Mode: tc.mode, Required: true}}
			result, err := engine.Run(context.Background(), RunRequest{Prompt: "inspect", InitialUsage: model.Usage{InputTokens: 17, Reported: true}})
			if err == nil || p.calls != 0 || len(result.Messages) != 2 || result.Usage.InputTokens != 17 {
				t.Fatalf("required route silently proceeded: %+v err=%v calls=%d", result, err, p.calls)
			}
		})
	}
}

func TestRequiredRouteViolationRetainsUsageAndDoesNotExecute(t *testing.T) {
	p := &scriptedProvider{responses: []model.Response{{Content: "discarded", ToolCalls: []model.ToolCall{{ID: "bad", Name: "write_test", Arguments: json.RawMessage(`{}`)}}, Usage: model.Usage{InputTokens: 101, OutputTokens: 13, Reported: true}}}}
	sink := &event.MemorySink{}
	engine := Agent{Provider: p, Tools: tool.NewRegistry(echoTool{}, writeTestTool{}), Approval: AutomaticApproval{AllowWrite: true}, Events: event.NewSequencedSink(sink), Router: fixedRouter{choice: "echo", confidence: 1}, Routing: ToolRoutingPolicy{Mode: "route", Required: true}}
	result, err := engine.Run(context.Background(), RunRequest{Prompt: "inspect"})
	if err == nil || p.index != 1 || result.Usage.InputTokens != 101 || !result.Usage.Reported || len(result.Messages) != 2 {
		t.Fatalf("route violation lost usage or fell back: %+v err=%v calls=%d", result, err, p.index)
	}
	for _, e := range sink.Events {
		if e.Type == event.ToolStarted || e.Type == event.ToolRouteFallback || e.Type == event.MessageDelta {
			t.Fatalf("discarded model action escaped routing: %+v", e)
		}
	}
}

func TestProviderFailureAndStepExhaustionRetainState(t *testing.T) {
	apiErr := errors.New("API stream interrupted")
	p := &failingProvider{response: model.Response{Usage: model.Usage{InputTokens: 25, OutputTokens: 8, Reported: true}}, err: apiErr}
	engine := Agent{Provider: p, Tools: tool.NewRegistry(echoTool{}), Approval: AutomaticApproval{}, Events: event.NewSequencedSink(&event.MemorySink{})}
	result, err := engine.Run(context.Background(), RunRequest{Prompt: "inspect"})
	if !errors.Is(err, apiErr) || !result.Usage.Reported || result.Usage.InputTokens != 25 || len(result.Messages) != 2 {
		t.Fatalf("failed provider erased durable state: %+v %v", result, err)
	}
	engine.Provider = &scriptedProvider{responses: []model.Response{{ToolCalls: []model.ToolCall{{ID: "e", Name: "echo", Arguments: json.RawMessage(`{}`)}}, Usage: model.Usage{InputTokens: 22, Reported: true}}}}
	result, err = engine.Run(context.Background(), RunRequest{Prompt: "inspect", MaxSteps: 1})
	if err == nil || len(result.Messages) != 4 || result.Usage.InputTokens != 22 {
		t.Fatalf("step exhaustion erased progress: %+v %v", result, err)
	}
}

func TestFallbackAccountsForBothCallsAndMissingUsage(t *testing.T) {
	for _, missing := range []bool{false, true} {
		p := &scriptedProvider{responses: []model.Response{
			{ToolCalls: []model.ToolCall{{ID: "bad", Name: "write_test", Arguments: json.RawMessage(`{}`)}}, Usage: model.Usage{InputTokens: 20, OutputTokens: 4, Reported: !missing}},
			{Content: "done", Usage: model.Usage{InputTokens: 30, OutputTokens: 7, Reported: true}},
		}}
		collector := experiment.NewCollector(&event.MemorySink{}, "revision")
		engine := Agent{Provider: p, Tools: tool.NewRegistry(echoTool{}, writeTestTool{}), Approval: AutomaticApproval{}, Events: event.NewSequencedSink(collector), Router: fixedRouter{choice: "echo", confidence: 1}, Routing: ToolRoutingPolicy{Mode: "route"}}
		result, err := engine.Run(context.Background(), RunRequest{Prompt: "inspect"})
		if err != nil || result.Usage.InputTokens != 50 || result.Usage.OutputTokens != 11 || result.Usage.Reported == missing {
			t.Fatalf("wrong billable fallback totals: %+v err=%v", result, err)
		}
		metrics := collector.Report(true, "")
		_, present := metrics["totalTokens"]
		if present == missing {
			t.Fatalf("missing usage published as a complete total: %+v", metrics)
		}
	}
}

func TestPreparedViewCannotMutateDurableTranscriptOrResumeStep(t *testing.T) {
	initial := []model.Message{{Role: model.RoleSystem, Content: "original system"}, {Role: model.RoleUser, Content: "original request"}, {Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "old", Name: "echo", Arguments: json.RawMessage(`{"value":"original"}`)}}}, {Role: model.RoleTool, ToolCallID: "old", Content: "original output"}}
	before, _ := json.Marshal(initial)
	p := &scriptedProvider{responses: []model.Response{{Content: "done"}}}
	engine := Agent{Provider: p, Tools: tool.NewRegistry(echoTool{}), Approval: AutomaticApproval{}, Events: event.NewSequencedSink(&event.MemorySink{})}
	result, err := engine.Run(context.Background(), RunRequest{InitialMessages: initial, MaxSteps: 2, InitialUsage: model.Usage{InputTokens: 12, Reported: true}, PrepareMessages: func(_ context.Context, step int, messages []model.Message) ([]model.Message, error) {
		if step != 2 {
			t.Fatalf("restored step=%d want 2", step)
		}
		messages[0].Content = "injected view"
		messages[2].ToolCalls[0].Name = "view-only-name"
		messages[2].ToolCalls[0].Arguments[2] = 'X'
		messages[2].ToolCalls[0].Arguments = json.RawMessage(`{"view":true}`)
		return messages, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(initial)
	if string(before) != string(after) || !reflect.DeepEqual(result.Messages[:4], initial) || p.requests[0].Messages[0].Content != "injected view" {
		t.Fatalf("model view mutated durable history: %+v", result.Messages)
	}
	if result.Usage.Reported {
		t.Fatal("missing resumed provider usage was represented as complete")
	}
	engine.Provider = &failingProvider{}
	result, err = engine.Run(context.Background(), RunRequest{InitialMessages: initial, MaxSteps: 1, InitialUsage: model.Usage{InputTokens: 12, Reported: true}})
	if err == nil || !reflect.DeepEqual(result.Messages, initial) || result.Usage.InputTokens != 12 {
		t.Fatalf("resume reset step budget: %+v %v", result, err)
	}
}

func TestPureLLMHasNoToolsAndRejectsIllegalCalls(t *testing.T) {
	for _, illegal := range []bool{false, true} {
		response := model.Response{Content: "suggestion"}
		if illegal {
			response.ToolCalls = []model.ToolCall{{ID: "illegal", Name: "echo", Arguments: json.RawMessage(`{}`)}}
		}
		p := &scriptedProvider{responses: []model.Response{response}}
		sink := &event.MemorySink{}
		engine := Agent{Provider: p, Tools: tool.NewRegistry(), Approval: AutomaticApproval{}, Events: event.NewSequencedSink(sink)}
		_, err := engine.Run(context.Background(), RunRequest{Prompt: "suggest code"})
		if len(p.requests[0].Tools) != 0 || (err != nil) != illegal {
			t.Fatalf("A profile did not enforce empty tools: err=%v", err)
		}
		for _, e := range sink.Events {
			if e.Type == event.ToolStarted {
				t.Fatal("pure LLM executed a tool")
			}
		}
	}
}

func TestApprovalResumeFromSerializedStatePreservesUsageAndStep(t *testing.T) {
	count := 0
	call := model.ToolCall{ID: "durable-write", Name: "tracked_write", Arguments: json.RawMessage(`{}`)}
	p := &scriptedProvider{responses: []model.Response{
		{ToolCalls: []model.ToolCall{call}, Usage: model.Usage{InputTokens: 41, OutputTokens: 9, Reported: true}},
		{Content: "done", Usage: model.Usage{InputTokens: 53, OutputTokens: 12, Reported: true}},
	}}
	registry := tool.NewRegistry(trackedWriteTool{calls: &count})
	engine := Agent{Provider: p, Tools: registry, Approval: AutomaticApproval{}, Events: event.NewSequencedSink(&event.MemorySink{})}
	paused, err := engine.Run(context.Background(), RunRequest{Prompt: "write", MaxSteps: 2})
	var approval *ApprovalRequiredError
	if !errors.As(err, &approval) || count != 0 {
		t.Fatalf("did not pause before execution: %+v %v", paused, err)
	}
	serialized, marshalErr := json.Marshal(paused)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	var restored RunResult
	if unmarshalErr := json.Unmarshal(serialized, &restored); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	approved := true
	result, err := engine.Run(context.Background(), RunRequest{InitialMessages: restored.Messages, InitialUsage: restored.Usage, ResumeToolCall: &call, ResumeApproved: &approved, MaxSteps: 2, Checkpoint: func(RunResult) error { return nil }, PrepareMessages: func(_ context.Context, step int, m []model.Message) ([]model.Message, error) {
		if step != 2 {
			t.Fatalf("resume reset steps to %d", step)
		}
		return m, nil
	}})
	if err != nil || result.Content != "done" || count != 1 || p.index != 2 || result.Usage.InputTokens != 94 || result.Usage.OutputTokens != 21 || !result.Usage.Reported {
		t.Fatalf("serialized pause lost execution state: %+v err=%v calls=%d", result, err, count)
	}
}
