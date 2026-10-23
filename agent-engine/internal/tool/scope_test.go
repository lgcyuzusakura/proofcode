package tool

import (
	"context"
	"encoding/json"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"testing"
)

type countedTool struct {
	calls *int
	name  string
	risk  Risk
}

func (t countedTool) Definition() model.ToolDefinition { return model.ToolDefinition{Name: t.name} }
func (t countedTool) Risk(json.RawMessage) Risk        { return t.risk }
func (t countedTool) Execute(context.Context, json.RawMessage) Result {
	*t.calls++
	return Result{Content: "executed"}
}
func TestScopePreventsWritesAndExcludedTools(t *testing.T) {
	calls := 0
	source := NewRegistry(countedTool{&calls, "read", RiskRead}, countedTool{&calls, "write", RiskWrite}, countedTool{&calls, "external", RiskExec})
	readonly := Scope(source, nil, true)
	for _, name := range []string{"write", "external"} {
		value, _ := readonly.Get(name)
		if !value.Execute(context.Background(), json.RawMessage(`{}`)).IsError {
			t.Fatal("side effect accepted")
		}
	}
	if calls != 0 {
		t.Fatal("side effect executed")
	}
	value, _ := readonly.Get("read")
	if value.Execute(context.Background(), nil).IsError || calls != 1 {
		t.Fatal("read failed")
	}
	scoped := Scope(source, []string{"read"}, false)
	if _, ok := scoped.Get("write"); ok {
		t.Fatal("excluded tool exposed")
	}
}
