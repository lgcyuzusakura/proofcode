package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

// Runtime owns task-local processes. Server startup is an administrator-configured
// capability; account profiles select aliases, never executable paths or secrets.
type toolClient interface {
	ListTools(context.Context) ([]ToolDefinition, error)
	CallTool(context.Context, string, map[string]any) (json.RawMessage, error)
	Close() error
}

type Runtime struct{ clients []toolClient }

func (r *Runtime) Close() {
	for _, client := range r.clients {
		_ = client.Close()
	}
	r.clients = nil
}

func Connect(ctx context.Context, configs []ServerConfig, directory string, registry *tool.Registry) (*Runtime, error) {
	runtime := &Runtime{}
	fail := func(err error) (*Runtime, error) { runtime.Close(); return nil, err }
	if len(configs) > 16 {
		return fail(errors.New("at most 16 MCP servers per task"))
	}
	aliases := map[string]bool{}
	for _, config := range configs {
		if !aliasPattern.MatchString(config.Name) || aliases[config.Name] {
			return fail(errors.New("invalid or duplicate MCP server alias"))
		}
		aliases[config.Name] = true
		config.Directory = directory
		// Start with the task context, so a discovery timeout does not kill a healthy process.
		client, err := startBounded(ctx, config)
		if err != nil {
			return fail(fmt.Errorf("MCP server %s failed to initialize", config.Name))
		}
		runtime.clients = append(runtime.clients, client)
		discovery, cancel := context.WithTimeout(ctx, 15*time.Second)
		definitions, err := client.ListTools(discovery)
		cancel()
		if err != nil {
			return fail(fmt.Errorf("MCP server %s discovery failed", config.Name))
		}
		for _, definition := range definitions {
			name, err := qualifiedToolName(config.Name, definition.Name)
			if err != nil {
				return fail(err)
			}
			if _, exists := registry.Get(name); exists {
				return fail(errors.New("MCP tool name collision"))
			}
			registry.Register(remoteTool{client: client, server: config.Name, name: name, definition: definition})
		}
	}
	return runtime, nil
}

var aliasPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,31}$`)

func qualifiedToolName(alias, name string) (string, error) {
	qualified := "mcp__" + alias + "__" + name
	if len(qualified) > 64 {
		return "", errors.New("qualified MCP tool name exceeds 64 characters")
	}
	return qualified, nil
}

func startBounded(ctx context.Context, config ServerConfig) (toolClient, error) {
	lifecycle, stop := context.WithCancel(ctx)
	timer := time.AfterFunc(15*time.Second, stop)
	var client toolClient
	var err error
	if config.URL != "" {
		client, err = StartHTTP(lifecycle, config)
	} else {
		client, err = Start(lifecycle, config)
	}
	if !timer.Stop() || err != nil {
		stop()
		if client != nil && err == nil {
			_ = client.Close()
		}
		return nil, errors.New("MCP initialization failed or timed out")
	}
	if local, ok := client.(*Client); ok {
		local.cancel = stop
	}
	if remote, ok := client.(*HTTPClient); ok {
		remote.cancel = stop
	}
	// lifecycle follows the task; Close terminates the process at task completion.
	return client, nil
}

type remoteTool struct {
	client       toolClient
	server, name string
	definition   ToolDefinition
}

func (t remoteTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: t.name, Description: "External MCP tool (" + t.server + "): " + t.definition.Description, Parameters: t.definition.InputSchema}
}

// MCP annotations are hints, never authority to auto-approve side effects.
func (t remoteTool) Risk(json.RawMessage) tool.Risk { return tool.RiskExec }
func (t remoteTool) Execute(ctx context.Context, raw json.RawMessage) tool.Result {
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil || args == nil {
		return tool.Result{Content: "MCP arguments must be an object", IsError: true}
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	result, err := t.client.CallTool(callCtx, t.definition.Name, args)
	metadata := map[string]any{"server": t.server, "externalTool": t.definition.Name}
	if err != nil {
		return tool.Result{Content: "MCP call failed or timed out", IsError: true, Metadata: metadata}
	}
	var status struct {
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(result, &status) != nil {
		return tool.Result{Content: "Invalid MCP result", IsError: true, Metadata: metadata}
	}
	return tool.Result{Content: string(result), IsError: status.IsError, Metadata: metadata}
}
