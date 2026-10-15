package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// App struct
type App struct {
	ctx         context.Context
	projectMu   sync.Mutex
	desktopRoot string
	registryDir string
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
	if method != http.MethodGet && method != http.MethodPost && method != http.MethodPatch {
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
	base := strings.TrimRight(os.Getenv("PROOFCODE_CONTROL_PLANE_URL"), "/")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	controlURL, err := url.Parse(base)
	if err != nil || (controlURL.Scheme != "http" && controlURL.Scheme != "https") || controlURL.Host == "" ||
		(controlURL.Path != "" && controlURL.Path != "/") || controlURL.RawQuery != "" || controlURL.Fragment != "" || controlURL.User != nil {
		return ProxyResponse{}, fmt.Errorf("control-plane URL is invalid")
	}
	target := strings.TrimRight(controlURL.String(), "/") + parsedPath.RequestURI()
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
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return ProxyResponse{}, fmt.Errorf("control-plane request: %w", err)
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

// GetModelConfig returns only the redacted Codex model configuration.
type RedactedModelConfig struct {
	Configured       bool   `json:"configured"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	BaseURL          string `json:"baseUrl"`
	Source           string `json:"source"`
	HasKey           bool   `json:"hasKey"`
	KeyLast4         string `json:"keyLast4"`
	MaxContextTokens int    `json:"maxContextTokens"`
	MaxOutputTokens  int    `json:"maxOutputTokens"`
	MaxTotalTokens   int    `json:"maxTotalTokens"`
	Error            string `json:"error,omitempty"`
}

func (a *App) GetModelConfig() RedactedModelConfig {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return RedactedModelConfig{Source: "Codex config.toml", Error: err.Error()}
		}
		dir = filepath.Join(home, ".codex")
	}
	result := RedactedModelConfig{Source: "Codex config.toml", MaxContextTokens: 183616, MaxOutputTokens: 16384, MaxTotalTokens: 200000}
	file, err := os.Open(filepath.Join(dir, "config.toml"))
	if err != nil {
		result.Error = "未找到配置文件：" + filepath.Join(dir, "config.toml")
		return result
	}
	defer file.Close()
	values := map[string]string{}
	section := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			result.Error = "config.toml 格式错误"
			return result
		}
		key, value := strings.TrimSpace(parts[0]), strings.Trim(strings.TrimSpace(parts[1]), "\"")
		values[section+"."+key] = value
	}
	provider := values[".model_provider"]
	if provider == "" {
		provider = "openai"
	}
	result.Provider = provider
	result.Model = values[".model"]
	result.BaseURL = values["model_providers."+provider+".base_url"]
	key := values["model_providers."+provider+".api_key"]
	result.HasKey = key != ""
	if len(key) >= 4 {
		result.KeyLast4 = "****" + key[len(key)-4:]
	}
	if result.Model == "" {
		result.Error = "缺少 model 配置"
	}
	if !result.HasKey && result.Error == "" {
		result.Error = "config.toml 未配置 api_key（不会读取环境变量）"
	}
	result.Configured = result.Error == ""
	return result
}
