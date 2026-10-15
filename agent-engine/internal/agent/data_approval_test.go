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

type approvalDataGateway struct {
	executeCalls int
}

func (g *approvalDataGateway) Call(_ context.Context, operation string, _ map[string]json.RawMessage) (json.RawMessage, error) {
	if operation != "execute" {
		return nil, errors.New("unexpected gateway operation")
	}
	g.executeCalls++
	return json.RawMessage(`{"status":"COMMITTED","result":{"affectedRows":1}}`), nil
}

func dataApprovalCall(id string) model.ToolCall {
	return model.ToolCall{ID: id, Name: "data_execute", Arguments: json.RawMessage(`{"planId":"11111111-1111-1111-1111-111111111111","digest":"` + strings.Repeat("a", 64) + `"}`)}
}

func TestRealDataExecuteAlwaysPausesWithAutomaticWriteAndExecEnabled(t *testing.T) {
	for _, remaining := range []bool{false, true} {
		name := "new model call"
		if remaining {
			name = "checkpoint remaining call"
		}
		t.Run(name, func(t *testing.T) {
			call := dataApprovalCall("data-1")
			gateway := &approvalDataGateway{}
			provider := &scriptedProvider{responses: []model.Response{{ToolCalls: []model.ToolCall{call}}, {Content: "must not advance"}}}
			memory := &event.MemorySink{}
			engine := Agent{Provider: provider, Tools: tool.NewRegistry(tool.DataTool{Operation: "execute", Gateway: gateway}), Approval: AutomaticApproval{AllowWrite: true, AllowExec: true}, Events: event.NewSequencedSink(memory)}
			request := RunRequest{TaskID: "data-approval", Prompt: "execute this plan"}
			if remaining {
				request.InitialMessages = []model.Message{{Role: model.RoleUser, Content: "execute this plan"}, {Role: model.RoleAssistant, ToolCalls: []model.ToolCall{call}}}
				request.RemainingCalls = []model.ToolCall{call}
			}
			pauseCalls, beforeCalls := 0, 0
			request.Pause = func(_ RunResult, pending *ApprovalRequiredError) error {
				pauseCalls++
				if gateway.executeCalls != 0 || pending.CallID != call.ID || string(pending.Arguments) != string(call.Arguments) {
					t.Fatalf("pause must precede execution and preserve approval binding: %+v", pending)
				}
				return nil
			}
			request.BeforeTool = func(RunResult, model.ToolCall) error { beforeCalls++; return nil }
			_, err := engine.Run(context.Background(), request)
			var pending *ApprovalRequiredError
			if !errors.As(err, &pending) || pending.Tool != "data_execute" || pending.Risk != tool.RiskWrite || pauseCalls != 1 {
				t.Fatalf("data execution bypassed explicit approval: err=%v pauses=%d", err, pauseCalls)
			}
			wantModelCalls := 1
			if remaining {
				wantModelCalls = 0
			}
			if gateway.executeCalls != 0 || beforeCalls != 0 || provider.index != wantModelCalls {
				t.Fatalf("advanced beyond approval boundary: gateway=%d before=%d model=%d", gateway.executeCalls, beforeCalls, provider.index)
			}
			approvals := 0
			for _, e := range memory.Events {
				if e.Type == event.ToolStarted || e.Type == event.ToolCompleted || e.Type == event.ToolFailed {
					t.Fatalf("execution event emitted before approval: %+v", e)
				}
				if e.Type == event.ToolApprovalNeeded {
					approvals++
					arguments := e.Payload["arguments"].(map[string]any)
					if arguments["planId"] != "11111111-1111-1111-1111-111111111111" || arguments["digest"] != strings.Repeat("a", 64) {
						t.Fatalf("durable approval event lost plan binding: %+v", arguments)
					}
				}
			}
			if approvals != 1 {
				t.Fatalf("durable approval events = %d", approvals)
			}
		})
	}
}

func TestRealDataExecuteResumeUsesExplicitDecisionAndJournalsBeforeGateway(t *testing.T) {
	for _, approved := range []bool{false, true} {
		name := "denied"
		if approved {
			name = "approved"
		}
		t.Run(name, func(t *testing.T) {
			call := dataApprovalCall("data-1")
			gateway := &approvalDataGateway{}
			provider := &scriptedProvider{responses: []model.Response{{Content: "done"}}}
			memory := &event.MemorySink{}
			engine := Agent{Provider: provider, Tools: tool.NewRegistry(tool.DataTool{Operation: "execute", Gateway: gateway}), Approval: AutomaticApproval{AllowWrite: true, AllowExec: true}, Events: event.NewSequencedSink(memory)}
			journaled, checkpoints := 0, 0
			result, err := engine.Run(context.Background(), RunRequest{
				TaskID: "data-approval", InitialMessages: []model.Message{{Role: model.RoleUser, Content: "execute"}, {Role: model.RoleAssistant, ToolCalls: []model.ToolCall{call}}},
				ResumeToolCall: &call, ResumeApproved: &approved,
				BeforeTool: func(_ RunResult, pending model.ToolCall) error {
					journaled++
					if gateway.executeCalls != 0 || pending.ID != call.ID {
						t.Fatal("gateway executed before intent journal")
					}
					return nil
				},
				Checkpoint: func(state RunResult) error {
					checkpoints++
					last := state.Messages[len(state.Messages)-1]
					if last.Role != model.RoleTool || last.ToolCallID != call.ID {
						t.Fatal("checkpoint did not persist explicit decision result")
					}
					return nil
				},
			})
			if err != nil || result.Content != "done" || checkpoints != 1 || provider.index != 1 {
				t.Fatalf("resume failed: result=%+v err=%v checkpoints=%d", result, err, checkpoints)
			}
			wantExecutions := 0
			if approved {
				wantExecutions = 1
			}
			if gateway.executeCalls != wantExecutions || journaled != wantExecutions {
				t.Fatalf("explicit decision not respected: approved=%t gateway=%d journal=%d", approved, gateway.executeCalls, journaled)
			}
			started, completed := 0, 0
			for _, e := range memory.Events {
				if e.Type == event.ToolStarted {
					started++
				}
				if e.Type == event.ToolCompleted {
					completed++
				}
			}
			if started != wantExecutions || completed != wantExecutions {
				t.Fatalf("execution events disagree with explicit decision: started=%d completed=%d", started, completed)
			}
		})
	}
}

func TestRealDataExecuteApprovalDoesNotAuthorizeRemainingPlans(t *testing.T) {
	first, second := dataApprovalCall("approved-plan"), dataApprovalCall("next-plan")
	gateway := &approvalDataGateway{}
	provider := &scriptedProvider{}
	memory := &event.MemorySink{}
	engine := Agent{Provider: provider, Tools: tool.NewRegistry(tool.DataTool{Operation: "execute", Gateway: gateway}), Approval: AutomaticApproval{AllowWrite: true, AllowExec: true}, Events: event.NewSequencedSink(memory)}
	approved := true
	_, err := engine.Run(context.Background(), RunRequest{
		TaskID: "data-approval", InitialMessages: []model.Message{{Role: model.RoleUser, Content: "execute both"}, {Role: model.RoleAssistant, ToolCalls: []model.ToolCall{first, second}}},
		ResumeToolCall: &first, ResumeApproved: &approved, RemainingCalls: []model.ToolCall{second}, Checkpoint: func(RunResult) error { return nil },
	})
	var pending *ApprovalRequiredError
	if !errors.As(err, &pending) || pending.CallID != second.ID || gateway.executeCalls != 1 || provider.index != 0 {
		t.Fatalf("one approval authorized another call: err=%v gateway=%d model=%d", err, gateway.executeCalls, provider.index)
	}
}
