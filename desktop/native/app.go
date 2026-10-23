package main

import (
	"context"
	"encoding/json"
	"fmt"
	sharedconfig "github.com/proofcode-dev/proofcode/agent-engine/pkg/config"
	"github.com/proofcode-dev/proofcode/agent-engine/pkg/runtimecheck"
	"io"
	"net/http"
	"net/url"
	pathpkg "path"
	"strings"
	"sync"
	"time"
)

// App struct
type App struct {
	ctx                    context.Context
	standaloneMu           sync.Mutex
	standalone             *standaloneRun
	standaloneClosed       bool
	projectMu              sync.Mutex
	runtimeMu              sync.Mutex
	runtimeRunner          composeCommand
	runtimeDockerRunner    runtimeDockerCommand
	runtimeState           RuntimeManagement
	runtimeCancel          context.CancelFunc
	runtimeClosed          bool
	runtimeBundleData      []byte
	mcpTemplateData        []byte
	mcpScaffoldMu          sync.Mutex
	runtimeDirectoryPicker func(context.Context, string) (string, error)
	desktopRoot            string
	registryDir            string
	commands               map[string]*commandRun
	browserMu              sync.Mutex
	browser                *browserRun
	browsers               map[string]*browserRun
	broker                 *browserBroker
	merges                 map[string]*mergeRun
	protocolMu             sync.Mutex
	protocols              map[string]*protocolRun
}

// ProxyResponse is the control-plane response exposed to the local frontend.
// It deliberately contains only status and response body; model credentials
// are never read or forwarded by this binding.
type ProxyResponse struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// ScratchProject is the non-sensitive bootstrap information needed to register
// a local project. The absolute path is returned only to the native caller and
// is never sent to the control plane.
type ScratchProject struct {
	Name        string `json:"name"`
	LocalHandle string `json:"localHandle"`
	BootstrapID string `json:"bootstrapId"`
	Path        string `json:"path"`
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// ProxyRequest forwards the shared frontend's authenticated API calls to a
// local control plane. WebSocket transport remains a browser concern; the
// desktop UI uses its durable event polling fallback.
func (a *App) ProxyRequest(path, method, body, authorization, idempotencyKey string) (ProxyResponse, error) {
	if path == "" || !strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "//") || strings.ContainsRune(path, '\x00') {
		return ProxyResponse{}, fmt.Errorf("proxy path must be an /api/ route")
	}
	parsedPath, err := url.Parse(path)
	if err != nil || parsedPath.IsAbs() || parsedPath.Host != "" || parsedPath.Fragment != "" ||
		!strings.HasPrefix(parsedPath.Path, "/api/") || parsedPath.EscapedPath() != parsedPath.Path ||
		pathpkg.Clean(parsedPath.Path) != parsedPath.Path || strings.ContainsRune(parsedPath.Path, '\\') {
		return ProxyResponse{}, fmt.Errorf("proxy path is invalid")
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method != http.MethodGet && method != http.MethodPost && method != http.MethodPatch && method != http.MethodPut {
		return ProxyResponse{}, fmt.Errorf("proxy method is not allowed")
	}
	if len(body) > 20<<20 {
		return ProxyResponse{}, fmt.Errorf("proxy request body exceeds 20 MiB")
	}
	if strings.ContainsAny(authorization, "\r\n") || len(authorization) > 4096 {
		return ProxyResponse{}, fmt.Errorf("authorization header is invalid")
	}
	if strings.ContainsAny(idempotencyKey, "\r\n") || len(idempotencyKey) > 200 {
		return ProxyResponse{}, fmt.Errorf("idempotency key is invalid")
	}
	connection, err := a.GetRuntimeConnection()
	if err != nil {
		return ProxyResponse{}, err
	}
	target := connection.BaseURL + parsedPath.RequestURI()
	request, err := http.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	if err != nil {
		return ProxyResponse{}, fmt.Errorf("create control-plane request: %w", err)
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return ProxyResponse{}, fmt.Errorf("无法连接 ProofCode 后端，请检查运行环境中的地址与服务状态")
	}
	defer response.Body.Close()
	const maxResponseBytes = 32 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return ProxyResponse{}, fmt.Errorf("read control-plane response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return ProxyResponse{}, fmt.Errorf("control-plane response exceeds 32 MiB")
	}
	return ProxyResponse{Status: response.StatusCode, Body: string(data)}, nil
}

// GetModelConfig exposes metadata only; credentials stay in the native process.
type RedactedModelConfig = sharedconfig.RedactedModelConfig

func (a *App) GetModelConfig() RedactedModelConfig {
	c, err := sharedconfig.Load()
	return c.Redacted(err)
}
func (a *App) GetModelList() ([]string, error) {
	c, err := sharedconfig.Load()
	if err != nil {
		return nil, err
	}
	return c.Models(context.Background())
}

// CheckRuntime reports local development dependencies and the configured
// control-plane health without returning process output or credentials.
func (a *App) CheckRuntime() runtimecheck.Report {
	connection, err := a.GetRuntimeConnection()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	report := (runtimecheck.Doctor{BaseURL: connection.BaseURL, Standalone: connection.Source == "standalone"}).Check(ctx)
	if err != nil {
		for index := range report.Checks {
			if report.Checks[index].ID == "control-plane" {
				report.Checks[index].Detail = "后端地址配置无法读取，请在运行环境页重新保存。"
			}
		}
	}
	return report
}

// ImportCodexProvider sends credentials to the configured control plane without exposing them to the WebView.
func (a *App) ImportCodexProvider(session string) (map[string]any, error) {
	if session == "" {
		return nil, fmt.Errorf("请先登录账户")
	}
	c, err := sharedconfig.Load()
	if err != nil {
		return nil, fmt.Errorf("本机 Codex 配置不可用")
	}
	if c.APIKey == "" {
		return nil, fmt.Errorf("本机 Codex 配置缺少 API Key")
	}
	body, err := json.Marshal(map[string]any{"name": c.Provider, "baseUrl": c.BaseURL, "apiKey": c.APIKey, "wireApi": c.WireAPI})
	if err != nil {
		return nil, fmt.Errorf("无法导入配置")
	}
	response, err := a.ProxyRequest("/api/model-providers", http.MethodPost, string(body), "Bearer "+session, "")
	if err != nil {
		return nil, fmt.Errorf("模型服务导入请求失败")
	}
	if response.Status/100 != 2 {
		return nil, fmt.Errorf("模型服务导入失败，HTTP %d", response.Status)
	}
	// Only the provider's redacted DTO is returned to the frontend.
	var value map[string]any
	if err = json.Unmarshal([]byte(response.Body), &value); err != nil {
		return nil, fmt.Errorf("控制面返回了无效服务配置")
	}
	return value, nil
}
