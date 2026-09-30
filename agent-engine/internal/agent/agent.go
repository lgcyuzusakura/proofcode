package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/config"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/decision"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

type Approval interface {
	Approve(context.Context, string, tool.Risk, string, json.RawMessage) (bool, error)
}

// ApprovalRequiredError is a durable pause boundary. Callers must persist the
// task/workspace state and stop the loop; continuing with a synthetic tool
// result would let the model make progress without the user's decision.
type ApprovalRequiredError struct {
	CallID    string
	Tool      string
	Risk      tool.Risk
	Arguments json.RawMessage
	Remaining []model.ToolCall
}

func (e *ApprovalRequiredError) Error() string {
	return fmt.Sprintf("approval required for %s tool call %s", e.Tool, e.CallID)
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
	InitialMessages       []model.Message
	ResumeToolCall        *model.ToolCall
	ResumeApproved        *bool
	InitialUsage          model.Usage
	RemainingCalls        []model.ToolCall
	Pause                 func(RunResult, *ApprovalRequiredError) error
	BeforeTool            func(RunResult, model.ToolCall) error
	Checkpoint            func(RunResult) error
}
type RunResult struct {
	Content        string
	Messages       []model.Message
	Usage          model.Usage
	RemainingCalls []model.ToolCall
}
type Agent struct {
	Provider model.Provider
	Tools    *tool.Registry
	Approval Approval
	Events   *event.SequencedSink
	Router   ToolRouter
	Routing  ToolRoutingPolicy
}

type ToolRouter interface {
	Choose(context.Context, string, []decision.Option) (decision.ChoiceResult, error)
}

type ToolRoutingPolicy struct {
	Mode          string
	MinConfidence float64
}

func (a *Agent) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	if a.Provider == nil || a.Tools == nil || a.Approval == nil || a.Events == nil {
		return RunResult{}, errors.New("agent dependencies are incomplete")
	}
	if request.MaxSteps <= 0 {
		request.MaxSteps = 24
	}
	messages := request.InitialMessages
	if len(messages) == 0 {
		messages = []model.Message{{Role: model.RoleSystem, Content: request.SystemPrompt}, {Role: model.RoleUser, Content: request.Prompt}}
	} else {
		messages = append([]model.Message(nil), messages...)
	}
	if !request.SuppressTaskLifecycle {
		if err := a.Events.Emit(ctx, request.TaskID, event.TaskStarted, map[string]any{"model": request.Model}); err != nil {
			return RunResult{Messages: messages}, err
		}
	}
	total := request.InitialUsage
	if request.ResumeToolCall != nil {
		if request.ResumeApproved == nil {
			return RunResult{Messages: messages}, errors.New("resume approval decision is missing")
		}
		selected, ok := a.Tools.Get(request.ResumeToolCall.Name)
		if !ok {
			return RunResult{Messages: messages}, fmt.Errorf("model requested unknown tool %q", request.ResumeToolCall.Name)
		}
		if request.Checkpoint == nil {
			return RunResult{Messages: messages, Usage: total}, errors.New("resume checkpoint callback is required")
		}
		if *request.ResumeApproved {
			if err := ctx.Err(); err != nil {
				return RunResult{Messages: messages, Usage: total}, err
			}
			if request.BeforeTool != nil {
				if err := request.BeforeTool(RunResult{Messages: messages, Usage: total}, *request.ResumeToolCall); err != nil {
					return RunResult{Messages: messages, Usage: total}, err
				}
			}
			if err := a.Events.Emit(ctx, request.TaskID, event.ToolStarted, map[string]any{"tool": request.ResumeToolCall.Name, "callId": request.ResumeToolCall.ID, "resumed": true}); err != nil {
				return RunResult{Messages: messages, Usage: total}, err
			}
			if err := ctx.Err(); err != nil {
				return RunResult{Messages: messages, Usage: total}, err
			}
			result := selected.Execute(ctx, request.ResumeToolCall.Arguments)
			kind := event.ToolCompleted
			if result.IsError {
				kind = event.ToolFailed
			}
			if err := a.Events.Emit(ctx, request.TaskID, kind, map[string]any{"tool": request.ResumeToolCall.Name, "callId": request.ResumeToolCall.ID, "result": result.Content, "metadata": result.Metadata, "resumed": true}); err != nil {
				return RunResult{Messages: messages, Usage: total}, err
			}
			encoded, _ := json.Marshal(result)
			messages = append(messages, model.Message{Role: model.RoleTool, ToolCallID: request.ResumeToolCall.ID, Content: string(encoded)})
			if request.Checkpoint != nil {
				if err := request.Checkpoint(RunResult{Messages: messages, Usage: total, RemainingCalls: request.RemainingCalls}); err != nil {
					return RunResult{Messages: messages, Usage: total}, err
				}
			}
		} else {
			messages = append(messages, model.Message{Role: model.RoleTool, ToolCallID: request.ResumeToolCall.ID, Content: `{"content":"Tool execution was denied by the user.","isError":true}`})
			if request.Checkpoint != nil {
				if err := request.Checkpoint(RunResult{Messages: messages, Usage: total, RemainingCalls: request.RemainingCalls}); err != nil {
					return RunResult{Messages: messages, Usage: total}, err
				}
			}
		}
	}
	for index, call := range request.RemainingCalls {
		selected, ok := a.Tools.Get(call.Name)
		if !ok {
			return RunResult{Messages: messages, Usage: total}, fmt.Errorf("model requested unknown tool %q", call.Name)
		}
		risk := selected.Risk(call.Arguments)
		if err := a.Events.Emit(ctx, request.TaskID, event.ToolRequested, map[string]any{"tool": call.Name, "callId": call.ID, "risk": risk, "arguments": json.RawMessage(call.Arguments), "resumed": true}); err != nil {
			return RunResult{Messages: messages, Usage: total}, err
		}
		approved, approvalErr := a.Approval.Approve(ctx, request.TaskID, risk, call.Name, call.Arguments)
		if approvalErr != nil {
			return RunResult{Messages: messages, Usage: total}, approvalErr
		}
		if !approved {
			pause := &ApprovalRequiredError{CallID: call.ID, Tool: call.Name, Risk: risk, Arguments: call.Arguments, Remaining: append([]model.ToolCall(nil), request.RemainingCalls[index+1:]...)}
			state := RunResult{Messages: messages, Usage: total}
			if request.Pause != nil {
				if err := request.Pause(state, pause); err != nil {
					return state, err
				}
			}
			if err := a.Events.Emit(ctx, request.TaskID, event.ToolApprovalNeeded, map[string]any{"tool": call.Name, "callId": call.ID, "risk": risk, "arguments": json.RawMessage(call.Arguments)}); err != nil {
				return state, err
			}
			return state, pause
		}
		if err := ctx.Err(); err != nil {
			return RunResult{Messages: messages, Usage: total}, err
		}
		if request.BeforeTool != nil {
			if err := request.BeforeTool(RunResult{Messages: messages, Usage: total}, call); err != nil {
				return RunResult{Messages: messages, Usage: total}, err
			}
		}
		if err := a.Events.Emit(ctx, request.TaskID, event.ToolStarted, map[string]any{"tool": call.Name, "callId": call.ID, "resumed": true}); err != nil {
			return RunResult{Messages: messages, Usage: total}, err
		}
		if err := ctx.Err(); err != nil {
			return RunResult{Messages: messages, Usage: total}, err
		}
		result := selected.Execute(ctx, call.Arguments)
		kind := event.ToolCompleted
		if result.IsError {
			kind = event.ToolFailed
		}
		if err := a.Events.Emit(ctx, request.TaskID, kind, map[string]any{"tool": call.Name, "callId": call.ID, "result": result.Content, "metadata": result.Metadata, "resumed": true}); err != nil {
			return RunResult{Messages: messages, Usage: total}, err
		}
		encoded, _ := json.Marshal(result)
		messages = append(messages, model.Message{Role: model.RoleTool, ToolCallID: call.ID, Content: string(encoded)})
		if request.Checkpoint != nil {
			if err := request.Checkpoint(RunResult{Messages: messages, Usage: total, RemainingCalls: request.RemainingCalls[index+1:]}); err != nil {
				return RunResult{Messages: messages, Usage: total}, err
			}
		}
	}
	completedSteps := 0
	for _, message := range messages {
		if message.Role == model.RoleAssistant {
			completedSteps++
		}
	}
	for step := completedSteps + 1; step <= request.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			if !request.SuppressTaskLifecycle {
				_ = a.Events.Emit(context.Background(), request.TaskID, event.TaskCancelled, map[string]any{"reason": err.Error()})
			}
			return RunResult{}, err
		}
		definitions, err := a.routeTools(ctx, request.TaskID, step, messages)
		if err != nil {
			return RunResult{Messages: messages, Usage: total}, err
		}
		allDefinitions := a.Tools.Definitions()
		routed := len(definitions) < len(allDefinitions)
		buffered := ""
		var streamErr error
		emitDelta := func(delta string) {
			if streamErr == nil {
				streamErr = a.Events.Emit(ctx, request.TaskID, event.MessageDelta, map[string]any{"delta": delta, "step": step})
			}
		}
		stream := emitDelta
		if routed {
			stream = func(delta string) { buffered += delta }
		}
		chatRequest := model.Request{Model: request.Model, Messages: messages, Tools: definitions, Temperature: 0.1, MaxTokens: config.DefaultMaxOutputTokens}
		response, err := a.Provider.Chat(ctx, chatRequest, stream)
		if routed && (err != nil || !callsUseDefinitions(response.ToolCalls, definitions)) && ctx.Err() == nil {
			if emitErr := a.Events.Emit(ctx, request.TaskID, event.ToolRouteFallback, map[string]any{"step": step, "reason": "selected tool was unavailable in model response"}); emitErr != nil {
				return RunResult{Messages: messages, Usage: total}, emitErr
			}
			chatRequest.Tools = allDefinitions
			response, err = a.Provider.Chat(ctx, chatRequest, emitDelta)
		} else if routed && buffered != "" {
			emitDelta(buffered)
		}
		if streamErr != nil {
			return RunResult{Messages: messages, Usage: total}, streamErr
		}
		if err != nil {
			if !request.SuppressTaskLifecycle {
				_ = a.Events.Emit(ctx, request.TaskID, event.TaskFailed, map[string]any{"error": err.Error()})
			}
			return RunResult{}, err
		}
		total.InputTokens += response.Usage.InputTokens
		total.OutputTokens += response.Usage.OutputTokens
		messages = append(messages, model.Message{Role: model.RoleAssistant, Content: response.Content, ToolCalls: response.ToolCalls})
		if err := a.Events.Emit(ctx, request.TaskID, event.UsageUpdated, map[string]any{"inputTokens": total.InputTokens, "outputTokens": total.OutputTokens, "totalTokens": total.InputTokens + total.OutputTokens, "budget": config.MaxTotalTokens, "budgetRemaining": config.MaxTotalTokens - total.InputTokens - total.OutputTokens}); err != nil {
			return RunResult{Messages: messages, Usage: total}, err
		}
		if len(response.ToolCalls) == 0 {
			if err := a.Events.Emit(ctx, request.TaskID, event.MessageCompleted, map[string]any{"content": response.Content}); err != nil {
				return RunResult{Messages: messages, Usage: total}, err
			}
			if !request.SuppressTaskLifecycle {
				if err := a.Events.Emit(ctx, request.TaskID, event.TaskCompleted, map[string]any{"steps": step}); err != nil {
					return RunResult{Messages: messages, Usage: total}, err
				}
			}
			return RunResult{Content: response.Content, Messages: messages, Usage: total}, nil
		}
		for callIndex, call := range response.ToolCalls {
			selected, ok := a.Tools.Get(call.Name)
			if !ok {
				return RunResult{}, fmt.Errorf("model requested unknown tool %q", call.Name)
			}
			risk := selected.Risk(call.Arguments)
			if err := a.Events.Emit(ctx, request.TaskID, event.ToolRequested, map[string]any{"tool": call.Name, "callId": call.ID, "risk": risk, "arguments": json.RawMessage(call.Arguments)}); err != nil {
				return RunResult{Messages: messages, Usage: total}, err
			}
			approved, approvalErr := a.Approval.Approve(ctx, request.TaskID, risk, call.Name, call.Arguments)
			if approvalErr != nil {
				return RunResult{}, approvalErr
			}
			if !approved {
				pause := &ApprovalRequiredError{CallID: call.ID, Tool: call.Name, Risk: risk, Arguments: call.Arguments, Remaining: append([]model.ToolCall(nil), response.ToolCalls[callIndex+1:]...)}
				state := RunResult{Messages: messages, Usage: total}
				if request.Pause != nil {
					if err := request.Pause(state, pause); err != nil {
						return state, err
					}
				}
				if err := a.Events.Emit(ctx, request.TaskID, event.ToolApprovalNeeded, map[string]any{"tool": call.Name, "callId": call.ID, "risk": risk, "arguments": json.RawMessage(call.Arguments)}); err != nil {
					return RunResult{Messages: messages, Usage: total}, err
				}
				return state, pause
			}
			if err := ctx.Err(); err != nil {
				return RunResult{Messages: messages, Usage: total}, err
			}
			if request.BeforeTool != nil {
				if err := request.BeforeTool(RunResult{Messages: messages, Usage: total}, call); err != nil {
					return RunResult{Messages: messages, Usage: total}, err
				}
			}
			if err := a.Events.Emit(ctx, request.TaskID, event.ToolStarted, map[string]any{"tool": call.Name, "callId": call.ID}); err != nil {
				return RunResult{Messages: messages, Usage: total}, err
			}
			if err := ctx.Err(); err != nil {
				return RunResult{Messages: messages, Usage: total}, err
			}
			result := selected.Execute(ctx, call.Arguments)
			kind := event.ToolCompleted
			if result.IsError {
				kind = event.ToolFailed
			}
			if err := a.Events.Emit(ctx, request.TaskID, kind, map[string]any{"tool": call.Name, "callId": call.ID, "result": result.Content, "metadata": result.Metadata}); err != nil {
				return RunResult{Messages: messages, Usage: total}, err
			}
			encoded, _ := json.Marshal(result)
			messages = append(messages, model.Message{Role: model.RoleTool, ToolCallID: call.ID, Content: string(encoded)})
			if request.Checkpoint != nil {
				if err := request.Checkpoint(RunResult{Messages: messages, Usage: total, RemainingCalls: response.ToolCalls[callIndex+1:]}); err != nil {
					return RunResult{Messages: messages, Usage: total}, err
				}
			}
		}
	}
	err := fmt.Errorf("agent exceeded maximum of %d steps", request.MaxSteps)
	if !request.SuppressTaskLifecycle {
		_ = a.Events.Emit(ctx, request.TaskID, event.TaskFailed, map[string]any{"error": err.Error()})
	}
	return RunResult{}, err
}

func callsUseDefinitions(calls []model.ToolCall, definitions []model.ToolDefinition) bool {
	allowed := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		allowed[definition.Name] = true
	}
	for _, call := range calls {
		if !allowed[call.Name] {
			return false
		}
	}
	return true
}

func (a *Agent) routeTools(ctx context.Context, taskID string, step int, messages []model.Message) ([]model.ToolDefinition, error) {
	definitions := a.Tools.Definitions()
	if a.Router == nil || a.Routing.Mode == "off" || len(definitions) < 2 {
		return definitions, nil
	}
	options := make([]decision.Option, 0, len(definitions))
	for _, definition := range definitions {
		options = append(options, decision.Option{Name: definition.Name, Description: definition.Description})
	}
	choice, err := a.Router.Choose(ctx, routingState(messages), options)
	if err != nil {
		if emitErr := a.Events.Emit(ctx, taskID, event.ToolRouted, map[string]any{"step": step, "mode": a.Routing.Mode, "applied": false, "error": err.Error()}); emitErr != nil {
			return nil, emitErr
		}
		return definitions, nil
	}
	minimum := a.Routing.MinConfidence
	if minimum <= 0 || minimum > 1 {
		minimum = 0.85
	}
	apply := a.Routing.Mode == "route" && choice.Choice != "defer" && choice.Confidence >= minimum
	if apply {
		found := false
		for _, definition := range definitions {
			if definition.Name == choice.Choice {
				definitions = []model.ToolDefinition{definition}
				found = true
				break
			}
		}
		apply = found
	}
	if err := a.Events.Emit(ctx, taskID, event.ToolRouted, map[string]any{"step": step, "mode": a.Routing.Mode, "choice": choice.Choice, "confidence": choice.Confidence, "probabilities": choice.Probabilities, "model": choice.Model, "latencyMs": choice.LatencyMS, "threshold": minimum, "applied": apply}); err != nil {
		return nil, err
	}
	return definitions, nil
}

func routingState(messages []model.Message) string {
	var state strings.Builder
	for _, message := range messages {
		if message.Role == model.RoleUser {
			state.WriteString("User request: ")
			state.WriteString(truncateRunes(message.Content, 3000))
			state.WriteByte('\n')
			break
		}
	}
	start := len(messages) - 4
	if start < 0 {
		start = 0
	}
	for _, message := range messages[start:] {
		if message.Role != model.RoleTool && message.Role != model.RoleAssistant {
			continue
		}
		state.WriteString(string(message.Role))
		state.WriteString(": ")
		state.WriteString(truncateRunes(message.Content, 750))
		if len(message.ToolCalls) > 0 {
			for _, call := range message.ToolCalls {
				state.WriteString(" requested=")
				state.WriteString(call.Name)
			}
		}
		state.WriteByte('\n')
	}
	return state.String()
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
