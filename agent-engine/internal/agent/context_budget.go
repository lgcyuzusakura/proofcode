package agent

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/config"
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
	groups, latestUser, err := groupMessages(messages)
	if err != nil {
		return nil, 0, 0, err
	}
	tools := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, map[string]any{"type": "function", "function": definition})
	}
	encodedTools, err := json.Marshal(tools)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("encode tool definitions: %w", err)
	}
	total := config.EstimateTokens(string(encodedTools))
	for index := range groups {
		for _, message := range messages[groups[index].start:groups[index].end] {
			encodedCalls, err := json.Marshal(message.ToolCalls)
			if err != nil {
				return nil, 0, 0, fmt.Errorf("encode tool calls: %w", err)
			}
			groups[index].tokens += config.EstimateTokens(message.Content, string(encodedCalls))
		}
		total += groups[index].tokens
	}
	if total <= maxModelInputTokens {
		return messages, 0, total, nil
	}

	kept := make([]bool, len(groups))
	for index := range kept {
		kept[index] = true
	}
	dropped := 0
	drop := func(start, end int) {
		for index := start; index < end; index++ {
			if !kept[index] || messages[groups[index].start].Role == model.RoleSystem {
				continue
			}
			kept[index] = false
			total -= groups[index].tokens
			dropped += groups[index].end - groups[index].start
		}
	}
	for index := 0; index < latestUser && total > maxModelInputTokens; {
		if messages[groups[index].start].Role == model.RoleSystem {
			index++
			continue
		}
		end := index + 1
		if messages[groups[index].start].Role == model.RoleUser {
			for end < latestUser && messages[groups[end].start].Role != model.RoleUser {
				end++
			}
		}
		drop(index, end)
		index = end
	}
	for index := latestUser + 1; index < len(groups) && total > maxModelInputTokens; index++ {
		drop(index, index+1)
	}
	if total > maxModelInputTokens {
		return nil, 0, total, fmt.Errorf("system prompt, latest user request, and tool definitions need %d estimated input tokens; limit is %d", total, maxModelInputTokens)
	}
	selected := make([]model.Message, 0, len(messages)-dropped)
	for index, group := range groups {
		if kept[index] {
			selected = append(selected, messages[group.start:group.end]...)
		}
	}
	return selected, dropped, total, nil
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
