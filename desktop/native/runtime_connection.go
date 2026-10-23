package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// This is the ProofCode backend address, never a model endpoint or API key.
type RuntimeConnection struct {
	BaseURL string `json:"baseUrl"`
	Source  string `json:"source"`
}

func normalizeControlPlaneURL(value string) (string, error) {
	invalid := errors.New("后端地址须为 http 或 https 根地址，不能包含账号、路径、查询参数或片段")
	if len(value) > 2048 || strings.ContainsAny(value, "\r\n\t\\") {
		return "", invalid
	}
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(value, "#") {
		return "", invalid
	}
	host := strings.ToLower(parsed.Hostname())
	if strings.HasSuffix(parsed.Host, ":") {
		return "", invalid
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", invalid
	}
	if strings.Contains(parsed.Host, ":") && parsed.Port() == "" && net.ParseIP(host) == nil {
		return "", invalid
	}
	port := parsed.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", invalid
		}
	}
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	parsed.Host = host
	if strings.Contains(host, ":") {
		parsed.Host = "[" + host + "]"
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(host, port)
	}
	parsed.Path = ""
	return parsed.String(), nil
}

func (a *App) runtimeConnection() (RuntimeConnection, error) {
	registry, err := a.projectRegistry()
	if err != nil {
		return RuntimeConnection{}, errors.New("无法读取后端连接配置")
	}
	data, err := os.ReadFile(filepath.Join(registry, "runtime-connection.json"))
	if err == nil {
		var config RuntimeConnection
		if len(data) > 8192 || json.Unmarshal(data, &config) != nil {
			return RuntimeConnection{}, errors.New("本机后端地址配置损坏，请重新保存")
		}
		base, err := normalizeControlPlaneURL(config.BaseURL)
		if err != nil {
			return RuntimeConnection{}, errors.New("本机后端地址配置无效，请重新保存")
		}
		return RuntimeConnection{BaseURL: base, Source: "saved"}, nil
	}
	if !os.IsNotExist(err) {
		return RuntimeConnection{}, errors.New("无法读取后端连接配置")
	}
	base, source := strings.TrimSpace(os.Getenv("PROOFCODE_CONTROL_PLANE_URL")), "environment"
	if base == "" {
		base, source = "http://127.0.0.1:8080", "default"
	}
	base, err = normalizeControlPlaneURL(base)
	if err != nil {
		return RuntimeConnection{}, errors.New("启动环境中的后端地址无效，请在运行环境页重新保存")
	}
	return RuntimeConnection{BaseURL: base, Source: source}, nil
}

func (a *App) GetRuntimeConnection() (RuntimeConnection, error) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	return a.runtimeConnection()
}

// Saving never probes a host or sends credentials. The UI clears its account
// session before invoking this binding when changing backend origins.
func (a *App) SaveRuntimeConnection(baseURL string) (RuntimeConnection, error) {
	base, err := normalizeControlPlaneURL(baseURL)
	if err != nil {
		return RuntimeConnection{}, err
	}
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	if a.runtimeState.Busy {
		return RuntimeConnection{}, errors.New("请等待本机服务操作完成后更改后端地址")
	}
	if a.runtimeClosed {
		return RuntimeConnection{}, errors.New("桌面应用正在关闭")
	}
	registry, err := a.projectRegistry()
	if err != nil {
		return RuntimeConnection{}, errors.New("无法保存后端地址")
	}
	if os.MkdirAll(registry, 0700) != nil {
		return RuntimeConnection{}, errors.New("无法保存后端地址")
	}
	config := RuntimeConnection{BaseURL: base, Source: "saved"}
	if atomicJSON(filepath.Join(registry, "runtime-connection.json"), config) != nil {
		return RuntimeConnection{}, errors.New("无法保存后端地址")
	}
	return config, nil
}
