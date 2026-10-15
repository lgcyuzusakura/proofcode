package config

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const MaxTotalTokens = 200000
const DefaultMaxOutputTokens = 16384

type ModelConfig struct {
	Provider         string
	Model            string
	BaseURL          string
	APIKey           string
	EnvKey           string
	Source           string
	MaxContextTokens int
	MaxOutputTokens  int
}

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

func Load() (ModelConfig, error) {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ModelConfig{}, err
		}
		dir = filepath.Join(home, ".codex")
	}
	path := filepath.Join(dir, "config.toml")
	file, err := os.Open(path)
	if err != nil {
		return ModelConfig{Source: "Codex config.toml"}, fmt.Errorf("未找到配置文件：%s", path)
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
			return ModelConfig{}, errors.New("config.toml 格式错误")
		}
		key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if strings.HasPrefix(value, "\"") {
			unquoted, e := strconv.Unquote(value)
			if e != nil {
				return ModelConfig{}, errors.New("config.toml 字符串格式错误")
			}
			value = unquoted
		}
		values[section+"."+key] = value
	}
	if err := scanner.Err(); err != nil {
		return ModelConfig{}, err
	}
	provider := values[".model_provider"]
	model := values[".model"]
	if provider == "" {
		provider = "openai"
	}
	prefix := "model_providers." + provider + "."
	baseURL := values[prefix+"base_url"]
	apiKey := values[prefix+"api_key"]
	envKey := values[prefix+"env_key"]
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		return ModelConfig{Provider: provider, BaseURL: baseURL, Source: "Codex config.toml"}, errors.New("缺少 model 配置")
	}
	if apiKey == "" {
		return ModelConfig{Provider: provider, Model: model, BaseURL: baseURL, EnvKey: envKey, Source: "Codex config.toml"}, errors.New("config.toml 未配置 api_key（不会读取环境变量）")
	}
	return ModelConfig{Provider: provider, Model: model, BaseURL: baseURL, APIKey: apiKey, Source: "Codex config.toml", MaxContextTokens: MaxTotalTokens - DefaultMaxOutputTokens, MaxOutputTokens: DefaultMaxOutputTokens}, nil
}

func (c ModelConfig) Redacted(err error) RedactedModelConfig {
	maxIn, maxOut := c.MaxContextTokens, c.MaxOutputTokens
	if maxIn == 0 {
		maxIn = MaxTotalTokens - DefaultMaxOutputTokens
	}
	if maxOut == 0 {
		maxOut = DefaultMaxOutputTokens
	}
	last4 := ""
	if len(c.APIKey) >= 4 {
		last4 = "****" + c.APIKey[len(c.APIKey)-4:]
	}
	r := RedactedModelConfig{Configured: err == nil && c.APIKey != "", Provider: c.Provider, Model: c.Model, BaseURL: c.BaseURL, Source: c.Source, HasKey: c.APIKey != "", KeyLast4: last4, MaxContextTokens: maxIn, MaxOutputTokens: maxOut, MaxTotalTokens: MaxTotalTokens}
	if err != nil {
		r.Error = err.Error()
	}
	return r
}

func EstimateTokens(values ...string) int {
	total := 0
	for _, value := range values {
		total += int(math.Ceil(float64(len([]byte(value))) / 4))
	}
	return total
}
