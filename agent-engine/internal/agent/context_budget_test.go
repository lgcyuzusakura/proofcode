package agent

import (
	"strings"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/config"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

func TestPrepareModelMessagesDropsWholeOldToolGroups(t *testing.T) {
	callOne := model.ToolCall{ID: "old-call", Name: "read_file", Arguments: []byte(`{"path":"old.go"}`)}
	callTwo := model.ToolCall{ID: "new-call", Name: "read_file", Arguments: []byte(`{"path":"new.go"}`)}
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system"},
		{Role: model.RoleUser, Content: "old request"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{callOne}},
		{Role: model.RoleTool, ToolCallID: callOne.ID, Content: strings.Repeat("old output ", (maxModelInputTokens*4+100)/len("old output "))},
		{Role: model.RoleUser, Content: "latest request"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{callTwo}},
		{Role: model.RoleTool, ToolCallID: callTwo.ID, Content: "latest output"},
	}
	selected, dropped, _, err := prepareModelMessages(messages, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 3 || len(selected) != 4 {
		t.Fatalf("unexpected pruning: dropped=%d selected=%d", dropped, len(selected))
	}
	if selected[0].Role != model.RoleSystem || selected[1].Content != "latest request" || selected[2].ToolCalls[0].ID != callTwo.ID || selected[3].ToolCallID != callTwo.ID {
		t.Fatalf("pruning broke current transcript: %+v", selected)
	}
	if _, _, err := groupMessages(selected); err != nil {
		t.Fatalf("pruned transcript is not tool-call consistent: %v", err)
	}
}

func TestPrepareModelMessagesRejectsOversizedLatestRequest(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system"},
		{Role: model.RoleUser, Content: strings.Repeat("x", (config.MaxTotalTokens-config.DefaultMaxOutputTokens)*4+4)},
	}
	if _, _, _, err := prepareModelMessages(messages, nil); err == nil {
		t.Fatal("expected oversized latest request to be rejected")
	}
}

func TestPrepareModelMessagesRejectsOrphanToolResult(t *testing.T) {
	messages := []model.Message{{Role: model.RoleSystem, Content: "system"}, {Role: model.RoleUser, Content: "request"}, {Role: model.RoleTool, ToolCallID: "missing", Content: "result"}}
	if _, _, _, err := prepareModelMessages(messages, nil); err == nil {
		t.Fatal("expected orphan tool result to be rejected")
	}
}
