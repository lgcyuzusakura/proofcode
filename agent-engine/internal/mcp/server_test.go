package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerRoundTripAndDisabledWrites(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "example.txt"), []byte("source evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	input, writer := io.Pipe()
	reader, output := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), input, output, tool.NewRegistry(tool.ReadFile{Workspace: ws}))
		_ = output.Close()
	}()
	encoder := json.NewEncoder(writer)
	scanner := bufio.NewScanner(reader)
	call := func(id int, method string, params any) map[string]json.RawMessage {
		t.Helper()
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
		if !scanner.Scan() {
			t.Fatal("server did not respond")
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if result := call(1, "initialize", map[string]any{}); result["error"] != nil {
		t.Fatal(string(result["error"]))
	}
	catalog := call(2, "tools/list", map[string]any{})
	var definitions struct {
		Tools []ToolDefinition `json:"tools"`
	}
	_ = json.Unmarshal(catalog["result"], &definitions)
	if len(definitions.Tools) != 1 || definitions.Tools[0].Name != "read_file" {
		t.Fatal("disabled tools exposed")
	}
	result := call(3, "tools/call", map[string]any{"name": "read_file", "arguments": map[string]any{"path": "example.txt"}})
	var value struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	_ = json.Unmarshal(result["result"], &value)
	if value.IsError || len(value.Content) != 1 || !strings.Contains(value.Content[0].Text, "source evidence") {
		t.Fatal("read failed")
	}
	result = call(4, "tools/call", map[string]any{"name": "apply_patch", "arguments": map[string]any{}})
	if result["error"] == nil {
		t.Fatal("write tool accepted")
	}
	result = call(5, "tools/call", map[string]any{"name": "read_file", "arguments": map[string]any{"path": "../outside"}})
	_ = json.Unmarshal(result["result"], &value)
	if !value.IsError {
		t.Fatal("outside read accepted")
	}
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
}
