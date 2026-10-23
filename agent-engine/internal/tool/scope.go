package tool

import (
	"context"
	"encoding/json"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

// Scope is enforced on every invocation, including approved calls resumed from disk.
// An empty allowlist preserves normal built-in tools; readOnly still gates dynamic risks.
func Scope(source *Registry, allowed []string, readOnly bool) *Registry {
	result := NewRegistry()
	names := map[string]bool{}
	for _, name := range allowed {
		names[name] = true
	}
	for _, definition := range source.Definitions() {
		if len(allowed) > 0 && !names[definition.Name] {
			continue
		}
		value, _ := source.Get(definition.Name)
		if readOnly {
			value = readOnlyTool{value}
		}
		result.Register(value)
	}
	if _, ok := result.Get("tool_search"); ok {
		result.Register(Discover{Registry: result})
	}
	return result
}

type readOnlyTool struct{ inner Tool }

func (t readOnlyTool) Definition() model.ToolDefinition {
	v := t.inner.Definition()
	v.Description = "Read-only mode: calls with side effects are blocked. " + v.Description
	return v
}
func (t readOnlyTool) Risk(raw json.RawMessage) Risk { return RiskRead }
func (t readOnlyTool) Execute(ctx context.Context, raw json.RawMessage) Result {
	if t.inner.Risk(raw) != RiskRead {
		return Result{Content: "Read-only agent cannot execute this operation. Start an execution task to make changes.", IsError: true, Metadata: map[string]any{"readOnlyBlocked": true}}
	}
	return t.inner.Execute(ctx, raw)
}
