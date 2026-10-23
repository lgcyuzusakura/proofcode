package tool

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestPlanPersistsWithVersionAndValidatesTransitions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	plan := &Plan{Path: path}
	call := func(p *Plan, input string) Result { return p.Execute(context.Background(), json.RawMessage(input)) }
	initial := call(plan, `{"action":"get"}`)
	if initial.IsError || initial.Content != `{"version":0,"summary":"","steps":[]}` {
		t.Fatal(initial)
	}
	valid := `{"action":"set","expectedVersion":0,"summary":"Investigate","steps":[{"id":"inspect","text":"Read evidence","status":"in_progress"}]}`
	if result := call(plan, valid); result.IsError {
		t.Fatal(result)
	}
	reloaded := &Plan{Path: path}
	var state PlanState
	if err := json.Unmarshal([]byte(call(reloaded, `{"action":"get"}`).Content), &state); err != nil || state.Version != 1 || state.Steps[0].ID != "inspect" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	for _, input := range []string{valid,
		`{"action":"set","expectedVersion":1,"steps":[{"id":"a","text":"a","status":"in_progress"},{"id":"b","text":"b","status":"in_progress"}]}`,
		`{"action":"set","expectedVersion":1,"steps":[{"id":"a","text":"a","status":"pending"},{"id":"a","text":"b","status":"pending"}]}`} {
		if result := call(reloaded, input); !result.IsError {
			t.Fatal("invalid edit accepted", input)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := reloaded.Execute(ctx, json.RawMessage(`{"action":"set","expectedVersion":1,"steps":[{"id":"a","text":"a","status":"completed"}]}`)); !result.IsError {
		t.Fatal("cancelled edit accepted")
	}
	scoped := Scope(NewRegistry(reloaded), nil, true)
	reader, _ := scoped.Get("update_plan")
	if result := callTool(reader, `{"action":"set","expectedVersion":1,"steps":[{"id":"inspect","text":"Evidence inspected","status":"completed"}]}`); result.IsError {
		t.Fatal(result)
	}
}

func callTool(value Tool, input string) Result {
	return value.Execute(context.Background(), json.RawMessage(input))
}
