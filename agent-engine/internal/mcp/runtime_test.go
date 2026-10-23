package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/agent"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

// Actual child-process JSON-RPC fixture: exercises initialization, pagination,
// discovery, error propagation, cancellation and termination without a model API.
func TestMCPProcess(t *testing.T) {
	if os.Getenv("PROOFCODE_MCP_FIXTURE") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == 0 {
			continue
		}
		var result any = map[string]any{}
		switch request.Method {
		case "tools/list":
			name := "echo"
			cursor := "second"
			if request.Params["cursor"] == "second" {
				name = "slow"
				cursor = ""
			}
			result = map[string]any{"tools": []any{map[string]any{"name": name, "inputSchema": map[string]any{"type": "object"}}}, "nextCursor": cursor}
		case "tools/call":
			if request.Params["name"] == "slow" {
				continue
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "fixture output"}}, "isError": false}
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		if request.Method == "tools/list" && os.Getenv("PROOFCODE_MCP_STALL") == "1" {
			select {}
		}
	}
	os.Exit(0)
}

func TestCancellationInterruptsBlockedStdin(t *testing.T) {
	client, err := Start(context.Background(), ServerConfig{Command: os.Args[0], Args: []string{"-test.run=^TestMCPProcess$"}, Env: map[string]string{"PROOFCODE_MCP_FIXTURE": "1", "PROOFCODE_MCP_STALL": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	// One discovery response is enough to tell the fixture to stop reading.
	var catalog json.RawMessage
	if err := client.call(context.Background(), "tools/list", map[string]any{}, &catalog); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = client.CallTool(ctx, "echo", map[string]any{"large": strings.Repeat("x", 1<<20)})
	if err == nil || time.Since(started) > 2*time.Second {
		t.Fatal("blocked stdin ignored cancellation")
	}
}

func TestRuntimeDiscoveryCallsCancellationAndClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry := tool.NewRegistry()
	runtime, err := Connect(ctx, []ServerConfig{{Name: "fixture", Command: os.Args[0], Args: []string{"-test.run=^TestMCPProcess$"}, Env: map[string]string{"PROOFCODE_MCP_FIXTURE": "1"}}}, t.TempDir(), registry)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if len(registry.Definitions()) != 2 {
		t.Fatal("paginated catalog missing")
	}
	echo, _ := registry.Get("mcp__fixture__echo")
	if echo.Risk(nil) != tool.RiskExec {
		t.Fatal("external tool must require execution approval")
	}
	if result := echo.Execute(ctx, json.RawMessage(`{}`)); result.IsError || result.Metadata["server"] != "fixture" {
		t.Fatalf("call failed: %+v", result)
	}
	slow, _ := registry.Get("mcp__fixture__slow")
	callCtx, stop := context.WithTimeout(ctx, 30*time.Millisecond)
	defer stop()
	if result := slow.Execute(callCtx, json.RawMessage(`{}`)); !result.IsError {
		t.Fatal("cancelled call succeeded")
	}
	// A cancelled/late response must not block subsequent RPC dispatch.
	if result := echo.Execute(ctx, json.RawMessage(`{}`)); result.IsError {
		t.Fatalf("call after cancellation failed: %+v", result)
	}
	client := runtime.clients[0]
	runtime.Close()
	if _, err := client.ListTools(ctx); err == nil {
		t.Fatal("closed client accepted discovery")
	}
}

// The main task catalog is assembled before the agent asks for a tool. This
// integration check ensures built-ins and MCP tools share discovery while the
// final allowlist and read-only wrapper still govern what can be executed.
func TestUnifiedToolCatalogHonorsAllowlistAndReadOnlyScope(t *testing.T) {
	registry := tool.NewRegistry(catalogReadTool{})
	registry.Register(tool.Discover{Registry: registry})
	runtime, err := Connect(context.Background(), []ServerConfig{{Name: "fixture", Command: os.Args[0], Args: []string{"-test.run=^TestMCPProcess$"}, Env: map[string]string{"PROOFCODE_MCP_FIXTURE": "1"}}}, t.TempDir(), registry)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	allowed := []string{"read_file", "tool_search", "mcp__fixture__echo"}
	scoped := tool.Scope(registry, allowed, false)
	if _, ok := scoped.Get("mcp__fixture__slow"); ok {
		t.Fatal("MCP tool outside the allowlist remained visible")
	}
	definitions := scoped.Definitions()
	if len(definitions) != 3 {
		t.Fatalf("catalog exposed %d tools; expected read_file, tool_search and one allowed MCP tool: %+v", len(definitions), definitions)
	}
	search, ok := scoped.Get("tool_search")
	if !ok {
		t.Fatal("tool_search missing from selected catalog")
	}
	result := search.Execute(context.Background(), json.RawMessage(`{"query":"echo"}`))
	if result.IsError {
		t.Fatalf("catalog search failed: %s", result.Content)
	}
	var found []model.ToolDefinition
	if err := json.Unmarshal([]byte(result.Content), &found); err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Name != "mcp__fixture__echo" {
		t.Fatalf("unified catalog search returned unexpected tools: %+v", found)
	}
	remote, _ := scoped.Get("mcp__fixture__echo")
	if remote.Risk(json.RawMessage(`{}`)) != tool.RiskExec {
		t.Fatal("MCP annotations must not lower external tool approval risk")
	}

	readOnly := tool.Scope(registry, allowed, true)
	remote, ok = readOnly.Get("mcp__fixture__echo")
	if !ok {
		t.Fatal("read-only catalog hid the selected MCP tool instead of explaining its blocked capability")
	}
	blocked := remote.Execute(context.Background(), json.RawMessage(`{}`))
	if !blocked.IsError || blocked.Metadata["readOnlyBlocked"] != true {
		t.Fatalf("read-only scope did not block the external side effect: %+v", blocked)
	}
}

type catalogReadTool struct{}

func (catalogReadTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "read_file", Description: "Read a workspace file", Parameters: map[string]any{"type": "object"}}
}
func (catalogReadTool) Risk(json.RawMessage) tool.Risk { return tool.RiskRead }
func (catalogReadTool) Execute(context.Context, json.RawMessage) tool.Result {
	return tool.Result{Content: "fixture file"}
}

func TestInvalidAliases(t *testing.T) {
	if _, err := Connect(context.Background(), []ServerConfig{{Name: "bad alias", Command: "unused"}}, t.TempDir(), tool.NewRegistry()); err == nil {
		t.Fatal("invalid alias accepted")
	}
}

type fixtureProvider struct{ calls int }

func (p *fixtureProvider) Chat(_ context.Context, _ model.Request, _ func(string)) (model.Response, error) {
	p.calls++
	if p.calls == 1 {
		return model.Response{ToolCalls: []model.ToolCall{{ID: "external-call", Name: "mcp__fixture__echo", Arguments: json.RawMessage(`{}`)}}}, nil
	}
	return model.Response{Content: "done"}, nil
}
func TestExternalToolPausesAndResumesThroughAgentAudit(t *testing.T) {
	ctx := context.Background()
	registry := tool.NewRegistry()
	runtime, err := Connect(ctx, []ServerConfig{{Name: "fixture", Command: os.Args[0], Args: []string{"-test.run=^TestMCPProcess$"}, Env: map[string]string{"PROOFCODE_MCP_FIXTURE": "1"}}}, t.TempDir(), registry)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	sink := &event.MemorySink{}
	provider := &fixtureProvider{}
	engine := agent.Agent{Provider: provider, Tools: registry, Approval: agent.AutomaticApproval{AllowExec: true, AllowWrite: true}, Events: event.NewSequencedSink(sink)}
	state, err := engine.Run(ctx, agent.RunRequest{TaskID: "task", Prompt: "use external tool", MaxSteps: 3})
	var pause *agent.ApprovalRequiredError
	if !errors.As(err, &pause) {
		t.Fatalf("external tool bypassed approval: %v", err)
	}
	for _, item := range sink.Events {
		if item.Type == event.ToolStarted || item.Type == event.ToolCompleted {
			t.Fatal("external tool executed before approval")
		}
	}
	approved := true
	result, err := engine.Run(ctx, agent.RunRequest{TaskID: "task", MaxSteps: 3, InitialMessages: state.Messages, InitialUsage: state.Usage, ResumeToolCall: &model.ToolCall{ID: pause.CallID, Name: pause.Tool, Arguments: pause.Arguments}, ResumeApproved: &approved, Checkpoint: func(agent.RunResult) error { return nil }})
	if err != nil || result.Content != "done" {
		t.Fatalf("resume failed: %v", err)
	}
	completed := false
	for _, item := range sink.Events {
		if item.Type == event.ToolCompleted && item.Payload["tool"] == "mcp__fixture__echo" {
			completed = true
		}
	}
	if !completed {
		t.Fatal("completed MCP call missing from audit")
	}
}
