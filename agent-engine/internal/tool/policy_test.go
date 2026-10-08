package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

type policySpy struct {
	name  string
	calls *int
}

func (s policySpy) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: s.name, Parameters: map[string]any{"type": "object"}}
}
func (s policySpy) Risk(json.RawMessage) Risk { return RiskExec }
func (s policySpy) Execute(context.Context, json.RawMessage) Result {
	*s.calls++
	return Result{Content: "executed"}
}

func TestDeterministicPolicyBlocksBeforeExecution(t *testing.T) {
	for _, tc := range []struct{ name, args string }{
		{"read_file", `{"path":".env"}`},
		{"read_file", `{"path":"nested/.env.production"}`},
		{"list_files", `{"path":".git"}`},
		{"search_code", `{"path":".ssh/keys"}`},
		{"read_file", `{"path":"nested\\.CODEX\\auth.json"}`},
		{"apply_patch", `{"edits":[{"path":"safe.go"},{"path":"config/private.pem"}]}`},
		{"run_command", `{"program":"git.exe","args":["reset","--hard"]}`},
		{"run_command", `{"program":"git","args":["push","origin","main"]}`},
		{"run_command", `{"program":"python","args":["-c","print(1)"]}`},
		{"run_command", `{"program":"node","args":["--eval","process.exit()"]}`},
		{"run_command", `{"program":"python","args":["-cprint(1)"]}`},
		{"run_command", `{"program":"node","args":["--eval=process.exit()"]}`},
		{"run_command", `{"program":"git","args":["show","HEAD:.git/config"]}`},
		{"run_command", `{"program":"python","args":["app.py","--file=.env"]}`},
		{"run_command", `{"program":"git","args":["show",".git/config"]}`},
		{"read_file", `[]`},
	} {
		t.Run(tc.name+tc.args, func(t *testing.T) {
			calls := 0
			registry := WithDeterministicPolicy(NewRegistry(policySpy{name: tc.name, calls: &calls}))
			selected, _ := registry.Get(tc.name)
			args := json.RawMessage(tc.args)
			if selected.Risk(args) != RiskRead {
				t.Fatal("blocked call should not request mutation approval")
			}
			result := selected.Execute(context.Background(), args)
			if !result.IsError || result.Metadata["policyBlocked"] != true || calls != 0 {
				t.Fatalf("policy ran protected operation: %+v calls=%d", result, calls)
			}
		})
	}
}

func TestDeterministicPolicyKeepsSafeOperationsAndInnerRisk(t *testing.T) {
	for _, tc := range []struct{ name, args string }{
		{"read_file", `{"path":"src/main.go"}`},
		{"read_file", `{"path":".env.example"}`},
		{"list_files", `{"path":"src"}`},
		{"search_code", `{"query":"invoice"}`},
		{"apply_patch", `{"edits":[{"path":"src/main.go","old_text":"a","new_text":"b"}]}`},
		{"run_command", `{"program":"go","args":["test","./..."]}`},
		{"run_command", `{"program":"git","args":["diff","--stat"]}`},
		{"run_command", `{"program":"python","args":["-m","pytest"]}`},
	} {
		t.Run(tc.name+tc.args, func(t *testing.T) {
			calls := 0
			registry := WithDeterministicPolicy(NewRegistry(policySpy{name: tc.name, calls: &calls}))
			selected, _ := registry.Get(tc.name)
			args := json.RawMessage(tc.args)
			if selected.Risk(args) != RiskExec {
				t.Fatal("safe call lost original approval requirement")
			}
			result := selected.Execute(context.Background(), args)
			if result.IsError || calls != 1 {
				t.Fatalf("policy blocked routine development: %+v calls=%d", result, calls)
			}
		})
	}
}
