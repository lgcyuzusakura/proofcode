package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed mcp-assets/*
var bundledMcpAssets embed.FS

var mcpTemplateFiles = []string{"package.json", "package-lock.json", ".gitignore", "README.md", "config.example.json", "src/config.mjs", "src/service.mjs", "src/http.mjs", "src/cli.mjs", "src/init.mjs", "tools/knowledge.mjs", "knowledge/.gitkeep", "tests/service.test.mjs", "templates/custom.mjs"}
var mcpScaffoldName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)

type McpServiceScaffold struct {
	Created   bool   `json:"created"`
	Name      string `json:"name"`
	Directory string `json:"directory"`
	Transport string `json:"transport"`
	URL       string `json:"url"`
	Bearer    string `json:"bearer"`
	Commands  string `json:"commands"`
	Docker    bool   `json:"docker"`
}

func (a *App) mcpTemplate() (*verifiedRuntimeBundle, error) {
	data := a.mcpTemplateData
	if data == nil {
		var err error
		data, err = bundledMcpAssets.ReadFile("mcp-assets/template.zip")
		if os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, errors.New("内置工具模板无法读取，请使用完整桌面发行包")
		}
	}
	if len(data) == 0 {
		return nil, nil
	}
	bundle, err := verifySourceBundle(data, func(name string) bool {
		for _, allowed := range mcpTemplateFiles {
			if name == allowed {
				return true
			}
		}
		return false
	}, mcpTemplateFiles)
	if err != nil {
		return nil, errors.New("内置工具模板校验失败，请使用完整桌面发行包")
	}
	return bundle, nil
}

// Passive metadata only. No installation, processes, network or credential reads.
func (a *App) GetMcpServiceTemplate() (BundledRuntime, error) {
	bundle, err := a.mcpTemplate()
	if err != nil || bundle == nil {
		return BundledRuntime{}, err
	}
	return bundle.info, nil
}

func writeMcpScaffold(staging, name, transport string, port int, docker bool) (string, string, error) {
	raw, err := os.ReadFile(filepath.Join(staging, "config.example.json"))
	if err != nil {
		return "", "", err
	}
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil || config["version"] != float64(1) {
		return "", "", errors.New("工具模板配置无效")
	}
	config["name"] = name
	config["modules"] = []string{"./tools/knowledge.mjs", "./tools/custom.mjs"}
	host, endpoint := "127.0.0.1", ""
	if docker {
		host = "0.0.0.0"
	}
	if transport == "http" {
		allowed := []string{"127.0.0.1", "localhost"}
		endpoint = fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
		if docker {
			allowed = append(allowed, "host.docker.internal")
			endpoint = fmt.Sprintf("http://host.docker.internal:%d/mcp", port)
		}
		config["transport"] = map[string]any{"type": "http", "host": host, "port": port, "tokenEnv": "PROOFCODE_MCP_TOKEN", "allowedHosts": allowed, "allowedOrigins": []string{}}
	} else {
		config["transport"] = map[string]any{"type": "stdio"}
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", "", err
	}
	secret, err := runtimeSecret()
	if err != nil {
		return "", "", err
	}
	custom, err := os.ReadFile(filepath.Join(staging, "templates", "custom.mjs"))
	if err != nil {
		return "", "", err
	}
	files := map[string][]byte{
		"config.local.json":    encoded,
		".env":                 []byte("PROOFCODE_MCP_TOKEN=" + secret + "\n"),
		"tools/custom.mjs":     custom,
		"knowledge/welcome.md": []byte("# " + name + "\n\nReplace this example with your own project documentation.\nThe default knowledge tool searches only Markdown and text files in this directory.\n"),
	}
	for name, body := range files {
		file, err := os.OpenFile(filepath.Join(staging, filepath.FromSlash(name)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", "", err
		}
		_, err = file.Write(body)
		closed := file.Close()
		if err != nil || closed != nil {
			return "", "", errors.New("工具文件写入未完成")
		}
	}
	if transport == "stdio" {
		secret = "" // stdio requires no credential in the UI.
	}
	return endpoint, secret, nil
}

func createMcpScaffold(ctx context.Context, bundle *verifiedRuntimeBundle, parent, name, transport string, port int, docker bool) (McpServiceScaffold, error) {
	if !mcpScaffoldName.MatchString(name) || (transport != "http" && transport != "stdio") || (transport == "http" && (port < 1024 || port > 65535)) || (transport == "stdio" && docker) {
		return McpServiceScaffold{}, errors.New("请使用有效服务别名、传输方式和 1024–65535 端口")
	}
	parent, err := filepath.Abs(parent)
	if err == nil {
		parent, err = filepath.EvalSymlinks(parent)
	}
	if err != nil {
		return McpServiceScaffold{}, errors.New("所选保存位置无法访问")
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return McpServiceScaffold{}, errors.New("请选择可写的文件夹")
	}
	target := filepath.Join(parent, "ProofCode-mcp-"+name)
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		return McpServiceScaffold{}, errors.New("同名工具目录已存在，请更换别名或保存位置；已有文件不会覆盖")
	}
	staging, err := os.MkdirTemp(parent, ".proofcode-mcp-")
	if err != nil {
		return McpServiceScaffold{}, errors.New("无法创建工具文件夹")
	}
	defer os.RemoveAll(staging)
	if err = extractSourceBundle(ctx, bundle, staging); err != nil {
		return McpServiceScaffold{}, errors.New("工具模板展开未完成，请重试")
	}
	endpoint, secret, err := writeMcpScaffold(staging, name, transport, port, docker)
	if err != nil {
		return McpServiceScaffold{}, errors.New("工具模板准备失败，请检查保存位置")
	}
	if ctx.Err() != nil {
		return McpServiceScaffold{}, errors.New("工具创建已取消")
	}
	if os.Mkdir(target, 0700) != nil {
		return McpServiceScaffold{}, errors.New("同名工具目录已存在或无法写入，已有文件不会覆盖")
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return McpServiceScaffold{}, errors.New("工具目录发布未完成，请检查保存位置；已有文件已保留")
	}
	for _, entry := range entries {
		if os.Rename(filepath.Join(staging, entry.Name()), filepath.Join(target, entry.Name())) != nil {
			return McpServiceScaffold{}, errors.New("工具目录发布未完成，请检查保存位置；已有文件已保留")
		}
	}
	quoted := "'" + strings.ReplaceAll(target, "'", "''") + "'"
	commands := "Set-Location -LiteralPath " + quoted + "\nnpm ci\nnpm run check\nnpm start"
	return McpServiceScaffold{Created: true, Name: name, Directory: target, Transport: transport, URL: endpoint, Bearer: secret, Commands: commands, Docker: docker}, nil
}

// Explicit creation only: never install dependencies, load modules or start a server.
// The fresh HTTP token is returned to the local form once, not saved to browser storage.
func (a *App) CreateMcpService(name, transport string, port int, docker bool) (McpServiceScaffold, error) {
	if !a.mcpScaffoldMu.TryLock() {
		return McpServiceScaffold{}, errors.New("已有工具服务正在创建，请等待完成")
	}
	defer a.mcpScaffoldMu.Unlock()
	if !mcpScaffoldName.MatchString(name) || (transport != "http" && transport != "stdio") || (transport == "http" && (port < 1024 || port > 65535)) || (transport == "stdio" && docker) {
		return McpServiceScaffold{}, errors.New("请检查工具别名、传输方式和端口")
	}
	bundle, err := a.mcpTemplate()
	if err != nil {
		return McpServiceScaffold{}, err
	}
	if bundle == nil {
		return McpServiceScaffold{}, errors.New("此开发版本没有内置工具模板，请使用下方命令创建")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return McpServiceScaffold{}, errors.New("桌面应用正在关闭")
	}
	parent, err := a.pickRuntimeDirectory("选择工具保存位置，将新建 ProofCode-mcp-" + name)
	if err != nil || parent == "" {
		return McpServiceScaffold{}, err
	}
	return createMcpScaffold(ctx, bundle, parent, name, transport, port, docker)
}
