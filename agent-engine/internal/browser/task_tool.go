package browser

import (
	"context"
	"encoding/json"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
	"sync"
)

// TaskTool allocates a separate browser only if the model actually calls it.
// It is owned by one task execution and never shares a personal browser profile.
type TaskTool struct {
	mu         sync.Mutex
	parent     context.Context
	executable string
	workspace  *workspace.Workspace
	session    *Session
}

func NewTaskTool(parent context.Context, executable string, ws *workspace.Workspace) *TaskTool {
	return &TaskTool{parent: parent, executable: executable, workspace: ws}
}
func (*TaskTool) Definition() model.ToolDefinition    { return (Tool{}).Definition() }
func (*TaskTool) Risk(args json.RawMessage) tool.Risk { return (Tool{}).Risk(args) }
func (t *TaskTool) Execute(ctx context.Context, args json.RawMessage) tool.Result {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ctx.Err() != nil {
		return failure(ctx.Err())
	}
	if t.session == nil {
		value, err := New(t.parent, t.executable, t.workspace)
		if err != nil {
			return failure(err)
		}
		t.session = value
	}
	return t.session.Execute(ctx, args)
}
func (t *TaskTool) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.session != nil {
		t.session.Close()
		t.session = nil
	}
}
