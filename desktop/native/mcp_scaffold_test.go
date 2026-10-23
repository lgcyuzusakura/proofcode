package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mcpTemplateFixture(t *testing.T) (*App, *verifiedRuntimeBundle) {
	t.Helper()
	app := &App{registryDir: t.TempDir()}
	bundle, err := app.mcpTemplate()
	if err != nil {
		t.Fatal(err)
	}
	if bundle == nil {
		t.Skip("run desktop/prepare-mcp-template.ps1 before template integration tests")
	}
	return app, bundle
}

func TestMcpScaffoldCreatesIndependentHttpAndStdioWithoutProcesses(t *testing.T) {
	app, _ := mcpTemplateFixture(t)
	parent := filepath.Join(t.TempDir(), "user's tools")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	app.runtimeDirectoryPicker = func(context.Context, string) (string, error) { return parent, nil }
	app.runtimeRunner = func(context.Context, string, ...string) (string, error) {
		t.Fatal("unexpected Compose process")
		return "", nil
	}
	info, err := app.GetMcpServiceTemplate()
	if err != nil || !info.Available || info.Files != len(mcpTemplateFiles) {
		t.Fatal("missing verified template metadata")
	}
	var previousSecret string
	for _, transport := range []string{"http", "stdio"} {
		result, err := app.CreateMcpService("team_"+transport, transport, 3198, false)
		if err != nil || !result.Created || result.Name != "team_"+transport || result.Transport != transport {
			t.Fatalf("create %s: %+v %v", transport, result, err)
		}
		if !strings.Contains(result.Commands, "user''s tools") || !strings.Contains(result.Commands, "npm ci\nnpm run check\nnpm start") {
			t.Fatal("PowerShell commands do not quote the actual path correctly")
		}
		env, err := os.ReadFile(filepath.Join(result.Directory, ".env"))
		if err != nil || len(strings.TrimSpace(strings.TrimPrefix(string(env), "PROOFCODE_MCP_TOKEN="))) != 64 {
			t.Fatal("missing independent secret")
		}
		if string(env) == previousSecret {
			t.Fatal("different services share credentials")
		}
		previousSecret = string(env)
		if strings.Contains(result.Commands, strings.TrimSpace(string(env))) {
			t.Fatal("commands expose credentials")
		}
		config, err := os.ReadFile(filepath.Join(result.Directory, "config.local.json"))
		if err != nil || bytes.Contains(config, []byte(result.Directory)) {
			t.Fatal("scaffold must use relative paths")
		}
		var parsed struct {
			Name      string
			Modules   []string
			Transport struct {
				Type, Host   string
				Port         int
				AllowedHosts []string
			}
		}
		if json.Unmarshal(config, &parsed) != nil || parsed.Name != result.Name || len(parsed.Modules) != 2 || parsed.Transport.Type != transport {
			t.Fatal("invalid generated config")
		}
		if transport == "http" && (result.URL != "http://127.0.0.1:3198/mcp" || len(result.Bearer) != 64 || parsed.Transport.Host != "127.0.0.1" || parsed.Transport.Port != 3198) {
			t.Fatal("HTTP connection settings differ from created config")
		}
		if transport == "stdio" && (result.Bearer != "" || result.URL != "") {
			t.Fatal("stdio exposed irrelevant HTTP credentials")
		}
		for _, file := range []string{"package-lock.json", "src/cli.mjs", "tools/custom.mjs", "tools/knowledge.mjs", "knowledge/welcome.md", "templates/custom.mjs"} {
			if _, err := os.Stat(filepath.Join(result.Directory, file)); err != nil {
				t.Fatal("missing independent service file", file)
			}
		}
		for _, forbidden := range []string{"node_modules", ".git", "thesis", "runtime-manifest.json"} {
			if _, err := os.Stat(filepath.Join(result.Directory, forbidden)); !os.IsNotExist(err) {
				t.Fatal("unexpected generated data", forbidden)
			}
		}
		if _, err = app.CreateMcpService(result.Name, transport, 3198, false); err == nil {
			t.Fatal("existing service overwritten")
		}
		after, _ := os.ReadFile(filepath.Join(result.Directory, ".env"))
		if !bytes.Equal(env, after) {
			t.Fatal("failed recreation rotated credentials")
		}
	}
	config, err := app.GetRuntimeManagement()
	if err != nil || config.Configured {
		t.Fatal("MCP scaffold changed deployment directory")
	}
}

func TestMcpScaffoldDockerCancellationValidationAndSingleFlight(t *testing.T) {
	app, bundle := mcpTemplateFixture(t)
	parent := t.TempDir()
	app.runtimeDirectoryPicker = func(context.Context, string) (string, error) { return parent, nil }
	value, err := app.CreateMcpService("docker_tools", "http", 3200, true)
	if err != nil || value.URL != "http://host.docker.internal:3200/mcp" || !value.Docker {
		t.Fatalf("Docker create: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(value.Directory, "config.local.json"))
	if !bytes.Contains(raw, []byte(`"0.0.0.0"`)) || !bytes.Contains(raw, []byte(`"host.docker.internal"`)) {
		t.Fatal("Docker listener/host whitelist not applied")
	}
	for _, input := range []struct {
		name, transport string
		port            int
		docker          bool
	}{{"../escape", "http", 3190, false}, {"a", "socket", 3190, false}, {"a", "http", 80, false}, {"a", "http", 65536, false}, {"a", "stdio", 0, true}} {
		if _, err := app.CreateMcpService(input.name, input.transport, input.port, input.docker); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	app.runtimeDirectoryPicker = func(context.Context, string) (string, error) { return "", nil }
	if v, err := app.CreateMcpService("cancel", "http", 3190, false); err != nil || v.Created || v.Bearer != "" {
		t.Fatal("cancel altered state")
	}
	app.mcpScaffoldMu.Lock()
	if _, err := app.CreateMcpService("busy", "http", 3190, false); err == nil {
		t.Fatal("duplicate create allowed")
	}
	app.mcpScaffoldMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := createMcpScaffold(ctx, bundle, parent, "cancelled", "http", 3190, false); err == nil {
		t.Fatal("cancelled extraction published")
	}
	entries, _ := os.ReadDir(parent)
	if len(entries) != 1 || entries[0].Name() != "ProofCode-mcp-docker_tools" {
		t.Fatal("failed request left staging data")
	}
	app.mcpTemplateData = []byte("broken")
	if _, err = app.GetMcpServiceTemplate(); err == nil {
		t.Fatal("corrupt template accepted")
	}
	app.mcpTemplateData = []byte{}
	if v, err := app.GetMcpServiceTemplate(); err != nil || v.Available {
		t.Fatal("missing template metadata")
	}
	if _, err = app.CreateMcpService("missing", "http", 3190, false); err == nil {
		t.Fatal("missing template accepted")
	}
}

func TestMcpScaffoldGeneratedServiceLoadsWithRealNode(t *testing.T) {
	if os.Getenv("PROOFCODE_MCP_SCAFFOLD_NODE_TEST") != "1" {
		t.Skip("opt-in: Node 22 and npm cache required")
	}
	app, _ := mcpTemplateFixture(t)
	parent := t.TempDir()
	app.runtimeDirectoryPicker = func(context.Context, string) (string, error) { return parent, nil }
	for _, transport := range []string{"http", "stdio"} {
		result, err := app.CreateMcpService("node_"+transport, transport, 3190, false)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		npm, err := exec.LookPath("npm.cmd")
		if err != nil {
			npm, err = exec.LookPath("npm")
		}
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(ctx, npm, "ci", "--offline", "--ignore-scripts", "--no-audit", "--no-fund")
		command.Dir = result.Directory
		configureRuntimeCommand(command)
		if raw, err := command.CombinedOutput(); err != nil {
			t.Fatalf("independent offline npm install: %v %s", err, raw)
		}
		command = exec.CommandContext(ctx, "node", "--env-file-if-exists=.env", "src/cli.mjs", "--config", "config.local.json", "--check")
		command.Dir = result.Directory
		configureRuntimeCommand(command)
		raw, err := command.CombinedOutput()
		if err != nil || !bytes.Contains(raw, []byte(`"project_brief"`)) || !bytes.Contains(raw, []byte(`"knowledge_search"`)) || !bytes.Contains(raw, []byte(`"status":"valid"`)) || (result.Bearer != "" && bytes.Contains(raw, []byte(result.Bearer))) {
			t.Fatalf("actual generated service validation failed: %v %s", err, raw)
		}
		// Imports and dependencies resolve only in the generated standalone directory.
		script := `import assert from 'node:assert/strict';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import { loadConfig } from './src/config.mjs';
import { createService } from './src/service.mjs';
import { listenHTTP } from './src/http.mjs';
const config = await loadConfig('config.local.json');
const client = new Client({name:'standalone-native-test',version:'1'}, {capabilities:{}});
let endpoint;
try {
 if(config.transport.type==='http') {
  config.transport.port=0;
  endpoint=await listenHTTP(config,await createService(config));
  await client.connect(new StreamableHTTPClientTransport(new URL(endpoint.url), {requestInit:{headers:{Authorization:'Bearer '+process.env.PROOFCODE_MCP_TOKEN}}}));
 } else {
  await client.connect(new StdioClientTransport({command:process.execPath,args:['--env-file-if-exists=.env','src/cli.mjs','--config','config.local.json'],cwd:process.cwd(),stderr:'pipe'}));
 }
 const catalog=await client.listTools();assert.deepEqual(catalog.tools.map(value=>value.name),['knowledge_search','project_brief']);
 const brief=await client.callTool({name:'project_brief',arguments:{topic:'Native scaffold',requirements:['standalone execution']}});
 assert.equal(brief.isError,false);assert.match(JSON.stringify(brief),/standalone execution/);
 const knowledge=await client.callTool({name:'knowledge_search',arguments:{query:'documentation'}});
 assert.equal(knowledge.isError,false);assert.match(JSON.stringify(knowledge),/welcome.md/);
 const invalid=await client.callTool({name:'project_brief',arguments:{topic:42}});assert.equal(invalid.isError,true);
 console.log('PASS standalone '+config.transport.type+' discovery, custom tool, knowledge search and parameter rejection');
} finally {await client.close();if(endpoint)await endpoint.close();}
`
		command = exec.CommandContext(ctx, "node", "--env-file-if-exists=.env", "--input-type=module", "-e", script)
		command.Dir = result.Directory
		configureRuntimeCommand(command)
		if raw, err = command.CombinedOutput(); err != nil {
			t.Fatalf("standalone generated service interoperability: %v %s", err, raw)
		}
		t.Log(strings.TrimSpace(string(raw)))
	}
}
