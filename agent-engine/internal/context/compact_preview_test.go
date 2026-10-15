package context

import (
	"context"
	"encoding/json"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"strings"
	"testing"
)

func TestLatestLargeSuccessHasExactRecoveryAndStableSystemPrefix(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{ProjectID: "p", WorkspaceID: "w", ConversationID: "c", TaskID: "t", AttemptID: "1"}
	original := `{"isError":false,"content":"` + strings.Repeat("long successful evidence ", 5000) + `"}`
	messages := []model.Message{{Role: model.RoleSystem, Content: "stable system"}, {Role: model.RoleUser, Content: "inspect"}, {Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "call", Name: "read_file", Arguments: json.RawMessage(`{}`)}}}, {Role: model.RoleTool, ToolCallID: "call", Content: original}}
	view, report, err := CompressDurable(context.Background(), store, scope, messages, nil, InputBudget{ContextTokens: 18000, OutputReserve: 2048, OverheadTokens: 256})
	if err != nil {
		t.Fatal(err)
	}
	if len(view) != 4 || view[0].Content != "stable system" || len(view[3].Content) > 13000 || len(report.SourceReferences) == 0 || report.CompressedMessages != 1 {
		t.Fatal("unbounded or invalid preview")
	}
	ref := report.SourceReferences[0]
	offset := 0
	recovered := ""
	for {
		page, err := store.Read(context.Background(), scope, ref, offset, 65536)
		if err != nil {
			t.Fatal(err)
		}
		recovered += page.Content
		offset = page.NextOffset
		if page.EOF {
			break
		}
	}
	if recovered != original {
		t.Fatal("exact original recovery failed")
	}
}

func TestLargeDataAndDecisionResultsStayExact(t *testing.T) {
	for _, name := range []string{"data_schema", "data_execute", "read_file"} {
		t.Run(name, func(t *testing.T) {
			store, err := OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			scope := Scope{ProjectID: "p", WorkspaceID: "w", ConversationID: "c", TaskID: "t", AttemptID: "1"}
			content := strings.Repeat("evidence ", 2000)
			if name == "read_file" {
				content += " pending approval"
			}
			original, _ := json.Marshal(map[string]any{"isError": false, "content": content})
			messages := []model.Message{{Role: model.RoleSystem, Content: "stable"}, {Role: model.RoleUser, Content: "inspect"}}
			for _, id := range []string{"first", "second"} {
				messages = append(messages, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: id, Name: name, Arguments: json.RawMessage(`{}`)}}}, model.Message{Role: model.RoleTool, ToolCallID: id, Content: string(original)})
			}
			view, report, err := CompressDurable(context.Background(), store, scope, messages, nil, InputBudget{ContextTokens: 30000, OutputReserve: 2048, OverheadTokens: 256})
			if err != nil || len(view) != 6 || view[3].Content != string(original) || view[5].Content != string(original) || report.CompressedMessages != 0 {
				t.Fatal("protected data/decision content changed", err)
			}
			_, _, err = CompressDurable(context.Background(), store, scope, messages, nil, InputBudget{ContextTokens: 1000, OutputReserve: 256})
			if err == nil {
				t.Fatal("protected content must fail closed when over budget")
			}
		})
	}
}
