package context

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

type InputBudget struct {
	ContextTokens  int `json:"contextTokens"`
	OutputReserve  int `json:"outputReserve"`
	OverheadTokens int `json:"overheadTokens"`
}

type BudgetReport struct {
	EstimatedInputTokens int    `json:"estimatedInputTokens"`
	ProtectedTokens      int    `json:"protectedTokens"`
	AvailableInputTokens int    `json:"availableInputTokens"`
	DroppedMessages      int    `json:"droppedMessages"`
	ProtectedIndexes     []int  `json:"protectedIndexes"`
	DroppedIndexes       []int  `json:"droppedIndexes"`
	TokenCounting        string `json:"tokenCounting"`
}

type protocolGroup struct {
	start, end, tokens int
	protected          bool
}

func protectedDecision(s string) bool {
	lower := strings.ToLower(s)
	for _, needle := range []string{"approv", "denied", "reject", "refus", "批准", "拒绝", "待执行", "pending"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// Only explicit, complete successes can be removed. Unknown result formats,
// errors, timeouts, plan/approval records, and data operations stay protected.
func successfulResult(m model.Message) bool {
	var r struct {
		IsError  *bool          `json:"isError"`
		Content  string         `json:"content"`
		Metadata map[string]any `json:"metadata"`
	}
	if json.Unmarshal([]byte(m.Content), &r) != nil || r.IsError == nil || *r.IsError || protectedDecision(m.Content) {
		return false
	}
	if exit, ok := r.Metadata["exitCode"]; ok && exit != float64(0) {
		return false
	}
	for _, field := range []string{"timedOut", "truncated", "outputTruncated"} {
		if value, ok := r.Metadata[field]; ok && value != false {
			return false
		}
	}
	return true
}

func protocolGroups(messages []model.Message) ([]protocolGroup, error) {
	groups := []protocolGroup{}
	haveUser := false
	for i := 0; i < len(messages); {
		m := messages[i]
		g := protocolGroup{start: i, end: i + 1}
		switch m.Role {
		case model.RoleSystem, model.RoleUser:
			g.protected = true
			if m.Role == model.RoleUser {
				haveUser = true
			}
		case model.RoleAssistant:
			g.protected = protectedDecision(m.Content)
			if len(m.ToolCalls) > 0 {
				if i+len(m.ToolCalls) >= len(messages) {
					return nil, errors.New("assistant tool calls have missing results")
				}
				expected := map[string]bool{}
				for _, call := range m.ToolCalls {
					if call.ID == "" || expected[call.ID] {
						return nil, errors.New("assistant tool call IDs must be unique and nonempty")
					}
					expected[call.ID] = true
					if strings.HasPrefix(call.Name, "data_") {
						g.protected = true
					}
				}
				for n := 1; n <= len(m.ToolCalls); n++ {
					result := messages[i+n]
					if result.Role != model.RoleTool || !expected[result.ToolCallID] {
						return nil, errors.New("assistant tool calls have missing or mismatched results")
					}
					delete(expected, result.ToolCallID)
					if !successfulResult(result) {
						g.protected = true
					}
				}
				g.end += len(m.ToolCalls)
			}
		case model.RoleTool:
			return nil, errors.New("tool result has no preceding assistant call")
		default:
			return nil, fmt.Errorf("unsupported message role %q", m.Role)
		}
		for _, message := range messages[g.start:g.end] {
			encoded, err := json.Marshal(message)
			if err != nil {
				return nil, fmt.Errorf("encode model message: %w", err)
			}
			g.tokens += tokenEstimate(string(encoded)) + 4 // conservative protocol framing estimate
		}
		groups = append(groups, g)
		i = g.end
	}
	if !haveUser {
		return nil, errors.New("model history has no user request")
	}
	if len(groups) > 0 {
		groups[len(groups)-1].protected = true
	}
	return groups, nil
}

// SelectMessages keeps every user/system requirement and the complete tool
// protocol of protected groups. It fails rather than silently erasing them.
func SelectMessages(messages []model.Message, definitions []model.ToolDefinition, budget InputBudget) ([]model.Message, BudgetReport, error) {
	r := BudgetReport{TokenCounting: "utf8-json-bytes-div-4-estimate", AvailableInputTokens: budget.ContextTokens - budget.OutputReserve, ProtectedIndexes: []int{}, DroppedIndexes: []int{}}
	if budget.ContextTokens < 1 || budget.OutputReserve < 0 || budget.OverheadTokens < 0 || r.AvailableInputTokens < 1 {
		return nil, r, errors.New("invalid model context/output budget")
	}
	groups, err := protocolGroups(messages)
	if err != nil {
		return nil, r, err
	}
	tools := make([]map[string]any, 0, len(definitions))
	for _, d := range definitions {
		tools = append(tools, map[string]any{"type": "function", "function": d})
	}
	b, err := json.Marshal(tools)
	if err != nil {
		return nil, r, err
	}
	overhead := tokenEstimate(string(b)) + budget.OverheadTokens
	r.EstimatedInputTokens, r.ProtectedTokens = overhead, overhead
	for _, g := range groups {
		r.EstimatedInputTokens += g.tokens
		if g.protected {
			r.ProtectedTokens += g.tokens
			for i := g.start; i < g.end; i++ {
				r.ProtectedIndexes = append(r.ProtectedIndexes, i)
			}
		}
	}
	if r.ProtectedTokens > r.AvailableInputTokens {
		return nil, r, fmt.Errorf("protected system/user requirements, approvals, errors and latest evidence need %d estimated tokens; input budget is %d", r.ProtectedTokens, r.AvailableInputTokens)
	}
	kept := make([]bool, len(groups))
	for i := range kept {
		kept[i] = true
	}
	for i, g := range groups {
		if r.EstimatedInputTokens <= r.AvailableInputTokens {
			break
		}
		if g.protected {
			continue
		}
		kept[i] = false
		r.EstimatedInputTokens -= g.tokens
		r.DroppedMessages += g.end - g.start
		for index := g.start; index < g.end; index++ {
			r.DroppedIndexes = append(r.DroppedIndexes, index)
		}
	}
	view := make([]model.Message, 0, len(messages)-r.DroppedMessages)
	for i, g := range groups {
		if kept[i] {
			view = append(view, messages[g.start:g.end]...)
		}
	}
	return view, r, nil
}
