package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

const MaxTotalTokens = 128000
const DefaultMaxInputTokens = 16000
const DefaultMaxOutputTokens = 2048

type ModelConfig struct {
	Provider, Model, BaseURL, EnvKey, Source, WireAPI string
	APIKey                                            string `json:"-"`
	MaxContextTokens, MaxOutputTokens                 int
}
type RedactedModelConfig struct {
	Configured       bool   `json:"configured"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	BaseURL          string `json:"baseUrl"`
	Source           string `json:"source"`
	WireAPI          string `json:"wireApi"`
	HasKey           bool   `json:"hasKey"`
	KeyLast4         string `json:"keyLast4"`
	MaxContextTokens int    `json:"maxContextTokens"`
	MaxOutputTokens  int    `json:"maxOutputTokens"`
	MaxTotalTokens   int    `json:"maxTotalTokens"`
	Error            string `json:"error,omitempty"`
}
type selection struct {
	Provider string `toml:"model_provider"`
	Model    string `toml:"model"`
}
type provider struct {
	BaseURL      string `toml:"base_url"`
	APIKey       string `toml:"api_key"`
	EnvKey       string `toml:"env_key"`
	WireAPI      string `toml:"wire_api"`
	RequiresAuth bool   `toml:"requires_openai_auth"`
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
	return LoadDirectory(dir)
}
func LoadDirectory(dir string) (ModelConfig, error) {
	c := ModelConfig{Source: "Codex config.toml", MaxContextTokens: DefaultMaxInputTokens, MaxOutputTokens: DefaultMaxOutputTokens}
	data, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		return c, errors.New("无法读取本机 Codex config.toml")
	}
	var root struct {
		Model     string               `toml:"model"`
		Provider  string               `toml:"model_provider"`
		Profile   string               `toml:"profile"`
		Profiles  map[string]selection `toml:"profiles"`
		Providers map[string]provider  `toml:"model_providers"`
	}
	// TOML errors can contain credential-bearing source lines; return a fixed error.
	if toml.Unmarshal(data, &root) != nil {
		return c, errors.New("Codex config.toml 格式无效")
	}
	c.Provider, c.Model = root.Provider, root.Model
	if root.Profile != "" {
		p, ok := root.Profiles[root.Profile]
		if !ok {
			return c, errors.New("Codex profile 不存在")
		}
		if p.Provider != "" {
			c.Provider = p.Provider
		}
		if p.Model != "" {
			c.Model = p.Model
		}
	}
	if c.Provider == "" {
		c.Provider = "openai"
	}
	p := root.Providers[c.Provider]
	c.BaseURL, c.WireAPI, c.APIKey, c.EnvKey = p.BaseURL, p.WireAPI, p.APIKey, p.EnvKey
	if c.BaseURL == "" {
		if c.Provider != "openai" {
			return c, errors.New("自定义 provider 缺少 base_url")
		}
		c.BaseURL = "https://api.openai.com/v1"
	}
	u, e := url.Parse(c.BaseURL)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return c, errors.New("provider base_url 无效")
	}
	if c.WireAPI == "" {
		c.WireAPI = "responses"
	}
	if c.WireAPI != "responses" && c.WireAPI != "chat" && c.WireAPI != "anthropic" {
		return c, errors.New("仅支持 responses、chat 或 anthropic wire_api")
	}
	if c.APIKey == "" && c.EnvKey != "" {
		c.APIKey = os.Getenv(c.EnvKey)
	}
	if c.APIKey == "" && (p.RequiresAuth || c.Provider == "openai") {
		b, e := os.ReadFile(filepath.Join(dir, "auth.json"))
		if e == nil {
			var auth struct {
				Key string `json:"OPENAI_API_KEY"`
			}
			if json.Unmarshal(b, &auth) == nil {
				c.APIKey = auth.Key
			}
		}
	}
	if c.Model == "" {
		return c, errors.New("缺少 model 配置")
	}
	if c.APIKey == "" {
		return c, errors.New("未找到 API key；OAuth 登录不能作为 API key 使用")
	}
	if strings.ContainsAny(c.APIKey, "\r\n") {
		c.APIKey = ""
		return c, errors.New("API key 格式无效")
	}
	return c, nil
}
func (c ModelConfig) Redacted(err error) RedactedModelConfig {
	in, out := c.MaxContextTokens, c.MaxOutputTokens
	if in == 0 {
		in = DefaultMaxInputTokens
	}
	if out == 0 {
		out = DefaultMaxOutputTokens
	}
	r := RedactedModelConfig{Configured: err == nil && c.APIKey != "", Provider: c.Provider, Model: c.Model, BaseURL: c.BaseURL, Source: c.Source, WireAPI: c.WireAPI, HasKey: c.APIKey != "", MaxContextTokens: in, MaxOutputTokens: out, MaxTotalTokens: MaxTotalTokens}
	if err != nil {
		r.Error = err.Error()
	}
	return r
}
func EstimateTokens(values ...string) int {
	n := 0
	for _, v := range values {
		n += int(math.Ceil(float64(len([]byte(v))) / 4))
	}
	return n
}
func (c ModelConfig) Models(ctx context.Context) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	values := []string{}
	seen, cursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	for page := 0; page < 16; page++ {
		query := ""
		if cursor != "" {
			query = "?after_id=" + url.QueryEscape(cursor)
		}
		req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(c.BaseURL, "/")+"/models"+query, nil)
		if err != nil {
			return nil, errors.New("模型列表地址无效")
		}
		if c.WireAPI == "anthropic" {
			req.Header.Set("x-api-key", c.APIKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+c.APIKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, errors.New("无法连接 provider 模型列表")
		}
		if resp.StatusCode != 200 {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("模型列表返回 HTTP %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		_ = resp.Body.Close()
		if err != nil || len(b) > 1<<20 {
			return nil, errors.New("模型列表超过限制")
		}
		var catalog struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if json.Unmarshal(b, &catalog) != nil || catalog.Data == nil {
			return nil, errors.New("模型列表格式无效")
		}
		for _, m := range catalog.Data {
			if m.ID != "" && len(m.ID) <= 200 && !strings.ContainsFunc(m.ID, unicode.IsControl) && !seen[m.ID] {
				values = append(values, m.ID)
				seen[m.ID] = true
				if len(values) > 1000 {
					return nil, errors.New("模型列表超过 1000 个")
				}
			}
		}
		if c.WireAPI != "anthropic" || !catalog.HasMore {
			sort.Strings(values)
			return values, nil
		}
		cursor = catalog.LastID
		if cursor == "" || len(cursor) > 200 || strings.ContainsFunc(cursor, unicode.IsControl) || cursors[cursor] {
			return nil, errors.New("模型目录分页游标无效")
		}
		cursors[cursor] = true
	}
	return nil, errors.New("模型目录超过 16 页")
}
