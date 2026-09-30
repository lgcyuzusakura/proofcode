package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// App struct
type App struct {
	ctx context.Context
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
