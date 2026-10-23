package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

// This integration uses the shipped Node service, not a reimplemented MCP fixture.
// Opt in after npm ci in mcp-service; normal Go builds do not require Node.
func TestCustomServiceInteroperability(t *testing.T) {
	if os.Getenv("PROOFCODE_CUSTOM_MCP_TEST") != "1" {
		t.Skip("set PROOFCODE_CUSTOM_MCP_TEST=1 after installing mcp-service dependencies")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	serviceRoot, err := filepath.Abs(filepath.Join("..", "..", "..", "mcp-service"))
	if err != nil {
		t.Fatal(err)
	}
	for _, transport := range []string{"stdio", "http"} {
		t.Run(transport, func(t *testing.T) {
			root := t.TempDir()
			plugin := `import {writeFile} from "node:fs/promises";
import {join} from "node:path";
export const tools=[{name:"write_note",description:"Write a selected project note",inputSchema:{type:"object",properties:{text:{type:"string"}},required:["text"],additionalProperties:false},execute:async({text},{root,signal})=>{await writeFile(join(root,"note.txt"),text,{signal});return {saved:true,message:process.env.PROOFCODE_FIXTURE_KEY};}}];`
			if err := os.WriteFile(filepath.Join(root, "tools.mjs"), []byte(plugin), 0600); err != nil {
				t.Fatal(err)
			}
			transportConfig := map[string]any{"type": transport}
			if transport == "http" {
				transportConfig = map[string]any{"type": "http", "host": "127.0.0.1", "port": 0, "tokenEnv": "PROOFCODE_FIXTURE_KEY", "allowedHosts": []string{"127.0.0.1"}, "allowedOrigins": []string{}}
			}
			encoded, err := json.Marshal(map[string]any{"version": 1, "name": "custom_tools", "root": root, "modules": []string{"./tools.mjs"}, "secretEnv": []string{"PROOFCODE_FIXTURE_KEY"}, "transport": transportConfig})
			if err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(root, "config.local.json")
			if err := os.WriteFile(configPath, encoded, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			secret := "fixture-private-key-012345678901234567890"
			config := ServerConfig{Name: "custom_tools", Command: node, Args: []string{filepath.Join(serviceRoot, "src", "cli.mjs"), "--config", configPath}, Env: map[string]string{"PROOFCODE_FIXTURE_KEY": secret}}
			if transport == "http" {
				command := exec.CommandContext(ctx, node, config.Args...)
				command.Env = append(os.Environ(), "PROOFCODE_FIXTURE_KEY="+secret)
				command.Stdout = io.Discard
				stderr, err := command.StderrPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := command.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { cancel(); _ = command.Wait() })
				scanner := bufio.NewScanner(stderr)
				if !scanner.Scan() {
					t.Fatal("custom HTTP service did not report readiness")
				}
				var ready struct {
					Status string `json:"status"`
					URL    string `json:"url"`
				}
				if err := json.Unmarshal(scanner.Bytes(), &ready); err != nil || ready.Status != "ready" || ready.URL == "" || strings.Contains(scanner.Text(), secret) {
					t.Fatal("invalid or sensitive custom service readiness output")
				}
				config = ServerConfig{Name: "custom_tools", URL: ready.URL, Bearer: secret, AllowPrivateHosts: true}
			}
			registry := tool.NewRegistry()
			runtime, err := Connect(ctx, []ServerConfig{config}, root, registry)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			custom, exists := registry.Get("mcp__custom_tools__write_note")
			if !exists || len(registry.Definitions()) != 1 || custom.Risk(nil) != tool.RiskExec {
				t.Fatal("custom tools were not discovered with external execution risk")
			}
			result := custom.Execute(ctx, json.RawMessage(`{"text":"custom service delivery"}`))
			if result.IsError || strings.Contains(result.Content, secret) || !strings.Contains(result.Content, "[redacted]") {
				t.Fatal("custom service call failed or leaked configured credentials")
			}
			written, err := os.ReadFile(filepath.Join(root, "note.txt"))
			if err != nil || string(written) != "custom service delivery" {
				t.Fatal("custom tool did not run against the configured workspace")
			}
			invalid := custom.Execute(ctx, json.RawMessage(`{"text":42}`))
			if !invalid.IsError {
				t.Fatal("custom service accepted arguments that violate its schema")
			}
		})
	}
}
