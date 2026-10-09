package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	codecontext "github.com/proofcode-dev/proofcode/agent-engine/internal/context"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/experiment"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
)

func feedbackCall(id, args string, failed bool) []model.Message {
	result, _ := json.Marshal(tool.Result{Content: "FAIL Add regression", IsError: failed, Metadata: map[string]any{"exitCode": 0}})
	return []model.Message{{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: id, Name: "run_command", Arguments: json.RawMessage(args)}}}, {Role: model.RoleTool, ToolCallID: id, Content: string(result)}}
}

func TestFailureFeedbackClearsOnlyMatchingSuccessfulRetry(t *testing.T) {
	failed := feedbackCall("f", `{"program":"go","args":["test"]}`, true)
	if latestFailure(failed) == "" {
		t.Fatal("unresolved failure lost")
	}
	unrelated := append(append([]model.Message{}, failed...), feedbackCall("other", `{"program":"git","args":["status"]}`, false)...)
	if latestFailure(unrelated) == "" {
		t.Fatal("unrelated success cleared failure")
	}
	resolved := append(unrelated, feedbackCall("retry", `{"args":["test"],"program":"go"}`, false)...)
	if latestFailure(resolved) != "" {
		t.Fatal("successful same-tool/arguments retry failed to resolve feedback")
	}
}

func TestExecutionRegistersScopedContextReadAndVersionSearch(t *testing.T) {
	root := t.TempDir()
	ws, e := workspace.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	p, _ := experiment.ForGroup("E")
	task := taskMessage{TaskID: "486c92f4-520f-4c87-9e17-141e34e4f35b", ProjectID: "567892f4-520f-4c87-9e17-141e34e4f35b", WorkspaceID: "567892f4-520f-4c87-9e17-141e34e4f35c", ConversationID: "567892f4-520f-4c87-9e17-141e34e4f35d", Attempt: 1, Prompt: "read", Model: "fixture"}
	r := &runner{WorkspaceRoot: filepath.Join(t.TempDir(), "workspaces")}
	coordinator, _, e := r.execution(task, ws, event.NewSequencedSink(&event.MemorySink{}))
	if e != nil {
		t.Fatal(e)
	}
	for _, registry := range []*tool.Registry{coordinator.MainTools, coordinator.ScoutTools, coordinator.VerifierTools} {
		if _, ok := registry.Get("context_read"); !ok {
			t.Fatal("raw context retrieval missing from execution tools")
		}
		if _, ok := registry.Get("context_search"); !ok {
			t.Fatal("version search missing from execution tools")
		}
	}
	task.ExperimentProfile = &p
	store, e := codecontext.OpenStore(filepath.Join(r.WorkspaceRoot, ".context-store"))
	if e != nil {
		t.Fatal(e)
	}
	ref, e := store.SaveReference(codecontext.Reference{Scope: contextScope(task), Kind: "tool", EndByte: 3}, []byte("raw"))
	if e != nil {
		t.Fatal(e)
	}
	read, _ := coordinator.MainTools.Get("context_read")
	args, _ := json.Marshal(map[string]any{"referenceId": ref.ID, "offset": 0, "maxBytes": 10})
	if result := read.Execute(context.Background(), args); result.IsError {
		t.Fatalf("registered scoped tool unable to read durable original: %+v", result)
	}
}
