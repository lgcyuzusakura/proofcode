package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"io"
	"sync"
)

// Serve exposes an explicitly constructed tool registry over standard MCP stdio.
// stdout is JSON-RPC only. The caller decides which write/exec capabilities exist.
func Serve(ctx context.Context, input io.Reader, output io.Writer, registry *tool.Registry) error {
	var writeMu sync.Mutex
	encoder := json.NewEncoder(output)
	send := func(id json.RawMessage, result any, code int, message string) {
		response := map[string]any{"jsonrpc": "2.0", "id": id}
		if code != 0 {
			response["error"] = map[string]any{"code": code, "message": message}
		} else {
			response["result"] = result
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = encoder.Encode(response)
	}
	type request struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	var pendingMu sync.Mutex
	pending := map[string]context.CancelFunc{}
	var workers sync.WaitGroup
	lifecycle, stop := context.WithCancel(ctx)
	defer stop()
	defer func() { stop(); workers.Wait() }()
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var call request
		if json.Unmarshal(scanner.Bytes(), &call) != nil {
			send(json.RawMessage("null"), nil, -32700, "Invalid JSON")
			continue
		}
		if call.JSONRPC != "2.0" {
			send(json.RawMessage("null"), nil, -32600, "Invalid JSON-RPC version")
			continue
		}
		if len(call.ID) == 0 || string(call.ID) == "null" {
			if call.Method == "notifications/cancelled" {
				var params struct {
					RequestID json.RawMessage `json:"requestId"`
				}
				if json.Unmarshal(call.Params, &params) == nil {
					pendingMu.Lock()
					cancel := pending[string(params.RequestID)]
					pendingMu.Unlock()
					if cancel != nil {
						cancel()
					}
				}
			}
			continue
		}
		switch call.Method {
		case "initialize":
			send(call.ID, map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "ProofCode tools", "version": "0.5.0"}}, 0, "")
		case "ping":
			send(call.ID, map[string]any{}, 0, "")
		case "tools/list":
			definitions := []ToolDefinition{}
			for _, definition := range registry.Definitions() {
				definitions = append(definitions, ToolDefinition{Name: definition.Name, Description: definition.Description, InputSchema: definition.Parameters})
			}
			send(call.ID, map[string]any{"tools": definitions}, 0, "")
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if json.Unmarshal(call.Params, &params) != nil || len(params.Arguments) == 0 {
				send(call.ID, nil, -32602, "Tool name and object arguments are required")
				continue
			}
			var object map[string]any
			if json.Unmarshal(params.Arguments, &object) != nil || object == nil {
				send(call.ID, nil, -32602, "Tool arguments must be an object")
				continue
			}
			selected, exists := registry.Get(params.Name)
			if !exists {
				send(call.ID, nil, -32602, "Unknown or disabled tool")
				continue
			}
			pendingMu.Lock()
			_, duplicate := pending[string(call.ID)]
			if len(pending) >= 4 || duplicate {
				pendingMu.Unlock()
				send(call.ID, nil, -32600, "Too many calls or duplicate request ID")
				continue
			}
			callCtx, cancel := context.WithCancel(lifecycle)
			pending[string(call.ID)] = cancel
			pendingMu.Unlock()
			workers.Add(1)
			go func(id json.RawMessage, args json.RawMessage, value tool.Tool) {
				defer workers.Done()
				defer cancel()
				defer func() { pendingMu.Lock(); delete(pending, string(id)); pendingMu.Unlock() }()
				result := value.Execute(callCtx, args)
				send(id, map[string]any{"content": []any{map[string]any{"type": "text", "text": result.Content}}, "isError": result.IsError}, 0, "")
			}(call.ID, params.Arguments, selected)
		default:
			send(call.ID, nil, -32601, "Method not found")
		}
	}
	return scanner.Err()
}
