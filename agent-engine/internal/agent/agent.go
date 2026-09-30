package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/config"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

type Approval interface {
	Approve(context.Context, string, tool.Risk, string, json.RawMessage) (bool, error)
}
type AutomaticApproval struct {
	AllowWrite bool
	AllowExec  bool
}

func (a AutomaticApproval) Approve(_ context.Context, _ string, risk tool.Risk, _ string, _ json.RawMessage) (bool, error) {
	switch risk {
	case tool.RiskRead:
		return true, nil
	case tool.RiskWrite:
		return a.AllowWrite, nil
	case tool.RiskExec:
		return a.AllowExec, nil
	default:
		return false, nil
	}
}

type RunRequest struct {
	TaskID                string
	SystemPrompt          string
	Prompt                string
	Model                 string
	MaxSteps              int
	SuppressTaskLifecycle bool
}
type RunResult struct {
	Content  string
	Messages []model.Message
	Usage    model.Usage
}
type Agent struct {
	Provider model.Provider
	Tools    *tool.Registry
	Approval Approval
	Events   *event.SequencedSink
}

func (a *Agent) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	if a.Provider == nil || a.Tools == nil || a.Approval == nil || a.Events == nil {
		return RunResult{}, errors.New("agent dependencies are incomplete")
	}
	if request.MaxSteps <= 0 {
		request.MaxSteps = 24
	}
	messages := []model.Message{{Role: model.RoleSystem, Content: request.SystemPrompt}, {Role: model.RoleUser, Content: request.Prompt}}
	if !request.SuppressTaskLifecycle {
		_ = a.Events.Emit(ctx, request.TaskID, event.TaskStarted, map[string]any{"model": request.Model})
	}
	var total model.Usage
	for step := 1; step <= request.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			if !request.SuppressTaskLifecycle {
				_ = a.Events.Emit(context.Background(), request.TaskID, event.TaskCancelled, map[string]any{"reason": err.Error()})
			}
			return RunResult{}, err
		}
		response, err := a.Provider.Chat(ctx, model.Request{Model: request.Model, Messages: messages, Tools: a.Tools.Definitions(), Temperature: 0.1, MaxTokens: config.DefaultMaxOutputTokens}, func(delta string) {
			_ = a.Events.Emit(ctx, request.TaskID, event.MessageDelta, map[string]any{"delta": delta, "step": step})
		})
		if err != nil {
			if !request.SuppressTaskLifecycle {
				_ = a.Events.Emit(ctx, request.TaskID, event.TaskFailed, map[string]any{"error": err.Error()})
			}
			return RunResult{}, err
		}
		total.InputTokens += response.Usage.InputTokens
		total.OutputTokens += response.Usage.OutputTokens
		messages = append(messages, model.Message{Role: model.RoleAssistant, Content: response.Content, ToolCalls: response.ToolCalls})
		_ = a.Events.Emit(ctx, request.TaskID, event.UsageUpdated, map[string]any{"inputTokens": total.InputTokens, "outputTokens": total.OutputTokens, "totalTokens": total.InputTokens + total.OutputTokens, "budget": config.MaxTotalTokens, "budgetRemaining": config.MaxTotalTokens - total.InputTokens - total.OutputTokens})
		if len(response.ToolCalls) == 0 {
			_ = a.Events.Emit(ctx, request.TaskID, event.MessageCompleted, map[string]any{"content": response.Content})
			if !request.SuppressTaskLifecycle {
				_ = a.Events.Emit(ctx, request.TaskID, event.TaskCompleted, map[string]any{"steps": step})
			}
			return RunResult{Content: response.Content, Messages: messages, Usage: total}, nil
		}
		for _, call := range response.ToolCalls {
			selected, ok := a.Tools.Get(call.Name)
			if !ok {
				return RunResult{}, fmt.Errorf("model requested unknown tool %q", call.Name)
			}
			risk := selected.Risk(call.Arguments)
			_ = a.Events.Emit(ctx, request.TaskID, event.ToolRequested, map[string]any{"tool": call.Name, "callId": call.ID, "risk": risk, "arguments": json.RawMessage(call.Arguments)})
			approved, approvalErr := a.Approval.Approve(ctx, request.TaskID, risk, call.Name, call.Arguments)
			if approvalErr != nil {
				return RunResult{}, approvalErr
			}
			if !approved {
				_ = a.Events.Emit(ctx, request.TaskID, event.ToolApprovalNeeded, map[string]any{"tool": call.Name, "callId": call.ID, "risk": risk})
				messages = append(messages, model.Message{Role: model.RoleTool, ToolCallID: call.ID, Content: "Tool execution was not approved."})
				continue
			}
			_ = a.Events.Emit(ctx, request.TaskID, event.ToolStarted, map[string]any{"tool": call.Name, "callId": call.ID})
			result := selected.Execute(ctx, call.Arguments)
			kind := event.ToolCompleted
			if result.IsError {
				kind = event.ToolFailed
			}
			_ = a.Events.Emit(ctx, request.TaskID, kind, map[string]any{"tool": call.Name, "callId": call.ID, "result": result.Content, "metadata": result.Metadata})
			encoded, _ := json.Marshal(result)
			messages = append(messages, model.Message{Role: model.RoleTool, ToolCallID: call.ID, Content: string(encoded)})
		}
	}
	err := fmt.Errorf("agent exceeded maximum of %d steps", request.MaxSteps)
	if !request.SuppressTaskLifecycle {
		_ = a.Events.Emit(ctx, request.TaskID, event.TaskFailed, map[string]any{"error": err.Error()})
	}
	return RunResult{}, err
}
