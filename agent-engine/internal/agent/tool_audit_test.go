package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

type dataAuditTool struct {
	name string
	risk tool.Risk
}

func (t dataAuditTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: t.name, Parameters: map[string]any{"type": "object"}}
}
func (t dataAuditTool) Risk(json.RawMessage) tool.Risk { return t.risk }
func (t dataAuditTool) Execute(context.Context, json.RawMessage) tool.Result {
	return tool.Result{Content: `{"rows":[{"email":"private-value-sentinel"}]}`, Metadata: map[string]any{"rows": []any{"private-value-sentinel"}, "values": "private-value-sentinel", "rowCount": 1, "errorCode": "NONE", "resourceId": "resource-1", "error": "private-value-sentinel"}}
}

func TestDataToolAuditRedactsValuesAcrossRunAndResume(t *testing.T) {
	call := model.ToolCall{ID: "plan", Name: "data_plan", Arguments: json.RawMessage(`{"resourceId":"resource-1","schemaVersion":"v1","values":{"email":"private-value-sentinel"},"filters":["private-value-sentinel"]}`)}
	for _, mode := range []string{"normal", "resumed", "remaining"} {
		t.Run(mode, func(t *testing.T) {
			sink := &event.MemorySink{}
			p := &scriptedProvider{responses: []model.Response{{Content: "done"}}}
			request := RunRequest{Prompt: "query"}
			if mode == "normal" {
				p.responses = append([]model.Response{{ToolCalls: []model.ToolCall{call}}}, p.responses...)
			} else {
				request.InitialMessages = []model.Message{{Role: model.RoleUser, Content: "query"}, {Role: model.RoleAssistant, ToolCalls: []model.ToolCall{call}}}
				request.Checkpoint = func(RunResult) error { return nil }
				if mode == "resumed" {
					approved := true
					request.ResumeToolCall = &call
					request.ResumeApproved = &approved
				} else {
					request.RemainingCalls = []model.ToolCall{call}
				}
			}
			engine := Agent{Provider: p, Tools: tool.NewRegistry(dataAuditTool{name: call.Name, risk: tool.RiskRead}), Approval: AutomaticApproval{}, Events: event.NewSequencedSink(sink)}
			result, err := engine.Run(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			foundModelValue := false
			for _, m := range result.Messages {
				if m.Role == model.RoleTool && strings.Contains(m.Content, "private-value-sentinel") {
					foundModelValue = true
				}
			}
			if !foundModelValue {
				t.Fatal("full data result was removed from model transcript")
			}
			for _, e := range sink.Events {
				if e.Type != event.ToolRequested && e.Type != event.ToolCompleted && e.Type != event.ToolFailed && e.Type != event.ToolApprovalNeeded {
					continue
				}
				encoded, _ := json.Marshal(e.Payload)
				if strings.Contains(string(encoded), "private-value-sentinel") {
					t.Fatalf("data values leaked into audit event: %s", encoded)
				}
				if e.Type == event.ToolCompleted && e.Payload["metadata"].(map[string]any)["rowCount"] != 1 {
					t.Fatal("summary lost useful counts")
				}
			}
		})
	}
}

func TestDataExecuteApprovalRetainsPlanDigestAndNoValues(t *testing.T) {
	call := model.ToolCall{ID: "execute", Name: "data_execute", Arguments: json.RawMessage(`{"resourceId":"resource-1","planId":"plan-1","digest":"sha256:abc123","values":"private-value-sentinel"}`)}
	for _, remaining := range []bool{false, true} {
		sink := &event.MemorySink{}
		p := &scriptedProvider{responses: []model.Response{{ToolCalls: []model.ToolCall{call}}}}
		request := RunRequest{Prompt: "execute"}
		if remaining {
			request.InitialMessages = []model.Message{{Role: model.RoleUser, Content: "execute"}, {Role: model.RoleAssistant, ToolCalls: []model.ToolCall{call}}}
			request.RemainingCalls = []model.ToolCall{call}
		}
		engine := Agent{Provider: p, Tools: tool.NewRegistry(dataAuditTool{name: call.Name, risk: tool.RiskWrite}), Approval: AutomaticApproval{}, Events: event.NewSequencedSink(sink)}
		_, err := engine.Run(context.Background(), request)
		var approval *ApprovalRequiredError
		if !errors.As(err, &approval) {
			t.Fatalf("data_execute did not pause: %v", err)
		}
		found := false
		for _, e := range sink.Events {
			if e.Type == event.ToolApprovalNeeded {
				found = true
				args := e.Payload["arguments"].(map[string]any)
				if args["planId"] != "plan-1" || args["digest"] != "sha256:abc123" {
					t.Fatalf("approval lost immutable plan binding: %+v", args)
				}
				encoded, _ := json.Marshal(args)
				if strings.Contains(string(encoded), "private-value-sentinel") {
					t.Fatal("approval event leaked row values")
				}
			}
		}
		if !found {
			t.Fatal("no durable approval event")
		}
	}
}
