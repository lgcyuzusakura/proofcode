package agent

import (
	"context"
	"encoding/json"
	codecontext "github.com/proofcode-dev/proofcode/agent-engine/internal/context"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type teamProvider struct {
	mu       sync.Mutex
	calls    int
	requests []model.Request
}

type largeChildProvider struct {
	t        *testing.T
	requests int
	ref      string
}

func (p *largeChildProvider) Chat(_ context.Context, r model.Request, _ func(string)) (model.Response, error) {
	p.requests++
	_, report, err := codecontext.SelectMessages(r.Messages, r.Tools, childInputBudget())
	if err != nil || report.EstimatedInputTokens > report.AvailableInputTokens || r.MaxTokens != childOutputTokens {
		p.t.Fatalf("child request outside budget: %+v %v", report, err)
	}
	switch p.requests {
	case 1:
		return model.Response{ToolCalls: []model.ToolCall{{ID: "large", Name: "read_file", Arguments: json.RawMessage(`{"path":"large.txt"}`)}}}, nil
	case 2:
		for _, m := range r.Messages {
			if m.ToolCallID == "large" {
				var result tool.Result
				if json.Unmarshal([]byte(m.Content), &result) != nil || result.Metadata["contextPreview"] != true {
					p.t.Fatal("large child evidence wasn't compacted")
				}
				p.ref, _ = result.Metadata["contextReferenceId"].(string)
			}
		}
		if p.ref == "" {
			p.t.Fatal("child lost original reference")
		}
		args, _ := json.Marshal(map[string]any{"referenceId": p.ref, "offset": 0, "maxBytes": 1024})
		return model.Response{ToolCalls: []model.ToolCall{{ID: "recover", Name: "context_read", Arguments: args}}}, nil
	default:
		for _, m := range r.Messages {
			if m.ToolCallID == "recover" {
				var result tool.Result
				if json.Unmarshal([]byte(m.Content), &result) != nil || result.IsError || !strings.Contains(result.Content, "evidence") {
					p.t.Fatal("child could not retrieve exact original evidence")
				}
			}
		}
		return model.Response{Content: "bounded evidence recovered"}, nil
	}
}

func TestChildLargeFileUsesPrivateRetrievableContext(t *testing.T) {
	root := t.TempDir()
	data := strings.Repeat("evidence from repository\n", 8000)
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	ws, _ := workspace.Open(root)
	path := filepath.Join(t.TempDir(), "team.json")
	p := &largeChildProvider{t: t}
	team, err := OpenTeam(context.Background(), p, tool.NewRegistry(tool.ReadFile{Workspace: ws}), nil, "task", "model", path)
	if err != nil {
		t.Fatal(err)
	}
	defer team.Close()
	child, err := team.spawn("large", "Inspect large.txt, retrieve the original evidence, and keep a small context.")
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"agentId": child.ID, "timeoutMs": 20000})
	result := (teamTool{team: team, actor: "main", operation: "wait_agent"}).Execute(context.Background(), args)
	var finished ChildTask
	if json.Unmarshal([]byte(result.Content), &finished) != nil || finished.Status != "completed" || p.requests != 3 {
		t.Fatalf("child=%+v result=%+v", finished, result)
	}
	store, err := codecontext.OpenStore(filepath.Join(filepath.Dir(path), "child-context"))
	if err != nil {
		t.Fatal(err)
	}
	scope := codecontext.Scope{ProjectID: "task", WorkspaceID: "task", ConversationID: child.ID, TaskID: child.ID, AttemptID: "1"}
	page, err := store.Read(context.Background(), scope, p.ref, 0, 1024)
	if err != nil || !strings.Contains(page.Content, "evidence") {
		t.Fatal("durable original lost", err)
	}
	scope.TaskID = "different-child"
	if _, err = store.Read(context.Background(), scope, p.ref, 0, 1024); err == nil {
		t.Fatal("another child read private evidence")
	}
}

type dynamicChildTool struct{ executed *bool }

func (dynamicChildTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "dynamic", Parameters: map[string]any{"type": "object"}}
}
func (dynamicChildTool) Risk(raw json.RawMessage) tool.Risk {
	if strings.Contains(string(raw), "write") {
		return tool.RiskWrite
	}
	return tool.RiskRead
}
func (t dynamicChildTool) Execute(context.Context, json.RawMessage) tool.Result {
	*t.executed = true
	return tool.Result{Content: "executed"}
}

func TestChildExecutionEnforcesReadOnlyAndStrictArguments(t *testing.T) {
	executed := false
	wrapped := childReadOnlyTool{dynamicChildTool{&executed}}
	if result := wrapped.Execute(context.Background(), json.RawMessage(`{"action":"write"}`)); !result.IsError || executed {
		t.Fatal("child invoked mutating operation")
	}
	if result := wrapped.Execute(context.Background(), json.RawMessage(`{"action":"read"}`)); result.IsError || !executed {
		t.Fatal("child couldn't read")
	}
	if result := (teamTool{operation: "list_agents"}).Execute(context.Background(), json.RawMessage(`{} {}`)); !result.IsError {
		t.Fatal("trailing team arguments accepted")
	}
}

func (p *teamProvider) Chat(ctx context.Context, r model.Request, d func(string)) (model.Response, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	p.requests = append(p.requests, r)
	p.mu.Unlock()
	if r.MaxTokens != 1024 {
		panic("child output budget missing")
	}
	if n == 1 {
		return model.Response{ToolCalls: []model.ToolCall{{ID: "read", Name: "read_file", Arguments: json.RawMessage(`{"path":"source.txt"}`)}, {ID: "send", Name: "send_message", Arguments: json.RawMessage(`{"agentId":"main","message":"found source"}`)}}, Usage: model.Usage{InputTokens: 20, OutputTokens: 10, Reported: true}}, nil
	}
	return model.Response{Content: "source verified", Usage: model.Usage{InputTokens: 30, OutputTokens: 5, Reported: true}}, nil
}
func TestTeamRealToolsMessagesIsolationLimitsAndRecovery(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "source.txt"), []byte("repository evidence"), 0600)
	ws, _ := workspace.Open(root)
	p := &teamProvider{}
	memory := &event.MemorySink{}
	path := filepath.Join(t.TempDir(), "team.json")
	team, err := OpenTeam(context.Background(), p, tool.NewRegistry(tool.ReadFile{Workspace: ws}), event.NewSequencedSink(memory), "task", "model", path)
	if err != nil {
		t.Fatal(err)
	}
	child, err := team.spawn("inspect", "Read source.txt and message main")
	if err != nil {
		t.Fatal(err)
	}
	wait := teamTool{team: team, actor: "main", operation: "wait_agent"}
	raw, _ := json.Marshal(map[string]any{"agentId": child.ID, "timeoutMs": 20000})
	result := wait.Execute(context.Background(), raw)
	var completed ChildTask
	_ = json.Unmarshal([]byte(result.Content), &completed)
	if result.IsError || completed.Status != "completed" || completed.Result != "source verified" {
		t.Fatalf("child=%+v result=%+v", completed, result)
	}
	messages := (teamTool{team: team, actor: "main", operation: "read_agent_messages"}).Execute(context.Background(), json.RawMessage(`{"after":0}`))
	if !json.Valid([]byte(messages.Content)) {
		t.Fatal(messages)
	}
	bad := (teamTool{team: team, actor: "main", operation: "send_message"}).Execute(context.Background(), json.RawMessage(`{"agentId":"another-task","message":"private"}`))
	if !bad.IsError {
		t.Fatal("cross task message accepted")
	}
	for _, v := range team.Tools(child.ID) {
		if v.Definition().Name == "spawn_agent" {
			t.Fatal("recursive child capability")
		}
	}
	for _, e := range memory.Events {
		if e.Type != event.AgentDelegated {
			t.Fatal("child events contaminated main stream")
		}
	}
	team.Close()
	recovered, err := OpenTeam(context.Background(), p, tool.NewRegistry(), nil, "task", "model", path)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if len(recovered.state.Children) != 1 || recovered.state.Children[0].Status != "completed" {
		t.Fatal("durable child result lost")
	}
	recovered.state.Children[0].Status = "running"
	if err = recovered.saveLocked(); err != nil {
		t.Fatal(err)
	}
	recovered.Close()
	restart, err := OpenTeam(context.Background(), p, tool.NewRegistry(), nil, "task", "model", path)
	if err != nil {
		t.Fatal(err)
	}
	defer restart.Close()
	if restart.state.Children[0].Status != "interrupted" {
		t.Fatal("interrupted child replayed")
	}
}

type blockProvider struct{ entered chan struct{} }

func (p blockProvider) Chat(ctx context.Context, r model.Request, d func(string)) (model.Response, error) {
	select {
	case p.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return model.Response{}, ctx.Err()
}
func TestTeamConcurrencyOneCancelAndSpawnLimit(t *testing.T) {
	entered := make(chan struct{}, 4)
	team, err := OpenTeam(context.Background(), blockProvider{entered}, tool.NewRegistry(), nil, "task", "model", filepath.Join(t.TempDir(), "team.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer team.Close()
	children := []ChildTask{}
	for i := 0; i < 4; i++ {
		c, err := team.spawn("read", "inspect")
		if err != nil {
			t.Fatal(err)
		}
		children = append(children, c)
	}
	if _, err = team.spawn("fifth", "inspect"); err == nil {
		t.Fatal("spawn limit bypassed")
	}
	<-entered
	select {
	case <-entered:
		t.Fatal("concurrency exceeded one")
	default:
	}
	for _, child := range children {
		raw, _ := json.Marshal(map[string]any{"agentId": child.ID})
		r := (teamTool{team: team, actor: "main", operation: "cancel_agent"}).Execute(context.Background(), raw)
		if r.IsError {
			t.Fatal(r)
		}
	}
}
