package agent

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/config"
	codecontext "github.com/proofcode-dev/proofcode/agent-engine/internal/context"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

const maxModelInputTokens = config.MaxTotalTokens - config.DefaultMaxOutputTokens

type messageGroup struct {
	start  int
	end    int
	tokens int
}

// prepareModelMessages retains the durable transcript while selecting a
// budgeted, tool-call-consistent view for the next model request.
func prepareModelMessages(messages []model.Message, definitions []model.ToolDefinition) ([]model.Message, int, int, error) {
	budget, err := ModelInputBudget()
	if err != nil {
		return nil, 0, 0, err
	}
	view, report, err := codecontext.SelectMessages(messages, definitions, budget)
	return view, report.DroppedMessages, report.EstimatedInputTokens, err
}

// ModelInputBudget uses explicit model capacity/output reserve configuration.
// Counts remain conservative byte estimates until a real tokenizer is wired.
func ModelInputBudget() (codecontext.InputBudget, error) {
	budget := codecontext.InputBudget{ContextTokens: config.MaxTotalTokens, OutputReserve: config.DefaultMaxOutputTokens, OverheadTokens: 256}
	for _, option := range []struct {
		name   string
		target *int
	}{
		{"PROOFCODE_MODEL_CONTEXT_TOKENS", &budget.ContextTokens},
		{"PROOFCODE_OUTPUT_RESERVE_TOKENS", &budget.OutputReserve},
	} {
		if value, present := os.LookupEnv(option.name); present {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > config.MaxTotalTokens {
				return budget, fmt.Errorf("invalid %s", option.name)
			}
			*option.target = n
		}
	}
	if budget.OutputReserve < config.DefaultMaxOutputTokens || budget.ContextTokens <= budget.OutputReserve+budget.OverheadTokens {
		return budget, errors.New("model context must exceed output reserve and overhead; output reserve must cover configured max output tokens")
	}
	return budget, nil
}

func groupMessages(messages []model.Message) ([]messageGroup, int, error) {
	groups := make([]messageGroup, 0, len(messages))
	latestUser := -1
	for index := 0; index < len(messages); {
		message := messages[index]
		group := messageGroup{start: index, end: index + 1}
		switch message.Role {
		case model.RoleSystem:
		case model.RoleUser:
			latestUser = len(groups)
		case model.RoleAssistant:
			if len(message.ToolCalls) > 0 {
				if index+len(message.ToolCalls) >= len(messages) {
					return nil, 0, errors.New("assistant tool calls have missing results")
				}
				expected := make(map[string]bool, len(message.ToolCalls))
				for _, call := range message.ToolCalls {
					if call.ID == "" || expected[call.ID] {
						return nil, 0, errors.New("assistant tool call IDs must be unique and nonempty")
					}
					expected[call.ID] = true
				}
				for offset := 1; offset <= len(message.ToolCalls); offset++ {
					result := messages[index+offset]
					if result.Role != model.RoleTool || !expected[result.ToolCallID] {
						return nil, 0, errors.New("assistant tool calls have missing or mismatched results")
					}
					delete(expected, result.ToolCallID)
				}
				group.end += len(message.ToolCalls)
			}
		case model.RoleTool:
			return nil, 0, errors.New("tool result has no preceding assistant call")
		default:
			return nil, 0, fmt.Errorf("unsupported message role %q", message.Role)
		}
		groups = append(groups, group)
		index = group.end
	}
	if latestUser < 0 {
		return nil, 0, errors.New("model history has no user request")
	}
	return groups, latestUser, nil
}
