package runtimecheck

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	checkBudget   = 7 * time.Second
	commandBudget = 2 * time.Second
)

type Check struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

type Report struct {
	CheckedAt time.Time `json:"checkedAt"`
	Checks    []Check   `json:"checks"`
}

type Doctor struct {
	LookPath func(string) (string, error)
	Run      func(context.Context, string, ...string) ([]byte, error)
	Client   *http.Client
	BaseURL  string
}

func (d Doctor) Check(ctx context.Context) Report {
	ctx, cancel := context.WithTimeout(ctx, checkBudget)
	defer cancel()
	if d.LookPath == nil {
		d.LookPath = exec.LookPath
	}
	if d.Run == nil {
		d.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			command := exec.CommandContext(ctx, name, args...)
			configureCommand(command)
			command.WaitDelay = 200 * time.Millisecond
			// Versions and daemon diagnostics are deliberately not retained.
			command.Stdout, command.Stderr = io.Discard, io.Discard
			return nil, command.Run()
		}
	}
	client := http.Client{}
	if d.Client != nil {
		client = *d.Client
	}
	client.Timeout = commandBudget
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	d.Client = &client
	checks := make([]Check, 5)
	var group sync.WaitGroup
	group.Add(3)
	go func() { defer group.Done(); checks[0] = executable(ctx, d, "git", "Git", "--version") }()
	go func() { defer group.Done(); checks[1] = executable(ctx, d, "docker", "Docker CLI", "--version") }()
	go func() { defer group.Done(); checks[4] = d.controlPlane(ctx) }()
	group.Wait()
	if checks[1].State == "ready" {
		checks[2] = commandCheck(ctx, d, "docker-engine", "Docker 引擎", "docker", "info", "--format", "{{.ServerVersion}}")
		// Compose is a CLI plugin, so it can be installed even when the
		// daemon is stopped. Keep its status independent from the engine.
		checks[3] = commandCheck(ctx, d, "compose", "Docker Compose", "docker", "compose", "version", "--short")
	} else {
		checks[2] = Check{ID: "docker-engine", Name: "Docker 引擎", State: "blocked", Detail: "Docker CLI 不可用，无法连接引擎。"}
		checks[3] = Check{ID: "compose", Name: "Docker Compose", State: "blocked", Detail: "Docker CLI 不可用，无法检查 Compose。"}
	}
	return Report{CheckedAt: time.Now().UTC(), Checks: checks}
}

func executable(ctx context.Context, d Doctor, id, name string, args ...string) Check {
	path, err := d.LookPath(id)
	if err != nil {
		return Check{ID: id, Name: name, State: "missing", Detail: "未安装或不在 PATH 中。"}
	}
	return runAtPath(ctx, d, id, name, path, args...)
}

func commandCheck(ctx context.Context, d Doctor, id, name, program string, args ...string) Check {
	path, err := d.LookPath(program)
	if err != nil {
		return Check{ID: id, Name: name, State: "missing", Detail: "程序未安装或不在 PATH 中。"}
	}
	return runAtPath(ctx, d, id, name, path, args...)
}

func runAtPath(ctx context.Context, d Doctor, id, name, path string, args ...string) Check {
	call, cancel := context.WithTimeout(ctx, commandBudget)
	defer cancel()
	if call.Err() != nil {
		return Check{ID: id, Name: name, State: "unavailable", Detail: "检查已取消或超时。"}
	}
	_, err := d.Run(call, path, args...)
	if err != nil {
		if call.Err() != nil {
			return Check{ID: id, Name: name, State: "unavailable", Detail: "检查超时或进程无法启动。"}
		}
		return Check{ID: id, Name: name, State: "unavailable", Detail: "程序存在，但检查命令失败。"}
	}
	return Check{ID: id, Name: name, State: "ready", Detail: "检查通过"}
}

func (d Doctor) controlPlane(ctx context.Context) Check {
	base, err := url.Parse(strings.TrimRight(d.BaseURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return Check{ID: "control-plane", Name: "ProofCode 后端", State: "missing", Detail: "桌面启动地址无效，请检查 PROOFCODE_CONTROL_PLANE_URL。"}
	}
	base.Path = "/actuator/health"
	call, cancel := context.WithTimeout(ctx, commandBudget)
	defer cancel()
	request, err := http.NewRequestWithContext(call, http.MethodGet, base.String(), nil)
	if err != nil {
		return Check{ID: "control-plane", Name: "ProofCode 后端", State: "unavailable", Detail: "无法构造健康检查请求。"}
	}
	response, err := d.Client.Do(request)
	if err != nil {
		return Check{ID: "control-plane", Name: "ProofCode 后端", State: "unavailable", Detail: "后端不可连接或健康检查超时。"}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Check{ID: "control-plane", Name: "ProofCode 后端", State: "unavailable", Detail: "健康检查未通过，请检查服务状态。"}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(body) > 4096 {
		return Check{ID: "control-plane", Name: "ProofCode 后端", State: "unavailable", Detail: "健康检查响应无效。"}
	}
	var payload struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Status == "" {
		return Check{ID: "control-plane", Name: "ProofCode 后端", State: "unavailable", Detail: "健康检查响应无效。"}
	}
	if payload.Status != "UP" {
		return Check{ID: "control-plane", Name: "ProofCode 后端", State: "unavailable", Detail: "后端报告为非健康状态。"}
	}
	return Check{ID: "control-plane", Name: "ProofCode 后端", State: "ready", Detail: "健康检查通过"}
}
