package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var proofCodeComposeServices = []string{"artemis", "control-plane", "postgres", "redis", "runner", "web"}

type RuntimeService struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Health string `json:"health"`
}
type RuntimeManagement struct {
	Directory  string           `json:"directory"`
	Configured bool             `json:"configured"`
	Running    bool             `json:"running"`
	Observed   bool             `json:"observed"`
	Busy       bool             `json:"busy"`
	Phase      string           `json:"phase"`
	Message    string           `json:"message"`
	CheckedAt  string           `json:"checkedAt"`
	Services   []RuntimeService `json:"services"`
}
type runtimeDirectoryConfig struct {
	Directory   string `json:"directory"`
	ComposeHash string `json:"composeHash"`
}
type composeCommand func(context.Context, string, ...string) (string, error)
type runtimeDockerCommand func(context.Context, ...string) (string, error)

// Drain build output without retaining unbounded logs or exposing credentials.
type runtimeOutput struct {
	data     []byte
	overflow bool
}

func (b *runtimeOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (1 << 20) - len(b.data)
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func executeRuntimeDocker(ctx context.Context, directory string, args ...string) (string, error) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return "", errors.New("Docker CLI 不可用")
	}
	command := exec.CommandContext(ctx, docker, args...)
	command.Dir = directory
	configureRuntimeCommand(command)
	command.WaitDelay = 300 * time.Millisecond
	var output runtimeOutput
	command.Stdout, command.Stderr = &output, io.Discard
	if err = command.Run(); err != nil {
		return "", errors.New("Docker 命令未完成")
	}
	if len(args) > 7 && args[0] == "compose" && (args[7] == "up" || args[7] == "stop") {
		return "", nil
	}
	if output.overflow {
		return "", errors.New("Docker 状态输出超出限制")
	}
	return strings.TrimSpace(string(output.data)), nil
}
func dockerCompose(ctx context.Context, directory string, args ...string) (string, error) {
	base := []string{"compose", "--project-name", "proofcode", "--project-directory", directory, "-f", filepath.Join(directory, "compose.yml")}
	return executeRuntimeDocker(ctx, directory, append(base, args...)...)
}
func (a *App) runCompose(ctx context.Context, directory string, args ...string) (string, error) {
	if a.runtimeRunner != nil {
		return a.runtimeRunner(ctx, directory, args...)
	}
	return dockerCompose(ctx, directory, args...)
}
func (a *App) runRuntimeDocker(ctx context.Context, args ...string) (string, error) {
	if a.runtimeDockerRunner != nil {
		return a.runtimeDockerRunner(ctx, args...)
	}
	return executeRuntimeDocker(ctx, "", args...)
}
func canonicalComposeDirectory(ctx context.Context, directory string, run composeCommand) (string, error) {
	resolved, err := filepath.Abs(directory)
	if err == nil {
		resolved, err = filepath.EvalSymlinks(resolved)
	}
	if err != nil {
		return "", errors.New("所选目录无法访问")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("所选路径不是可访问的目录")
	}
	file, err := os.Lstat(filepath.Join(resolved, "compose.yml"))
	if err != nil || !file.Mode().IsRegular() {
		return "", errors.New("所选目录中没有普通文件 compose.yml")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := run(checkCtx, resolved, "config", "--services")
	if err != nil {
		return "", errors.New("无法读取 Compose 服务配置，请检查 Docker Compose 与 .env")
	}
	services := strings.Fields(output)
	sort.Strings(services)
	if strings.Join(services, "\n") != strings.Join(proofCodeComposeServices, "\n") {
		return "", errors.New("Compose 服务清单与 ProofCode 不匹配")
	}
	return resolved, nil
}
func composeFileHash(directory string) (string, error) {
	file, err := os.Open(filepath.Join(directory, "compose.yml"))
	if err != nil {
		return "", errors.New("无法读取 compose.yml，请重新选择部署目录")
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, (1<<20)+1))
	if err != nil || n > 1<<20 {
		return "", errors.New("compose.yml 无法读取或超出大小限制")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func (a *App) runtimeConfig() (runtimeDirectoryConfig, error) {
	registry, err := a.projectRegistry()
	if err != nil {
		return runtimeDirectoryConfig{}, errors.New("无法读取本机部署配置")
	}
	data, err := os.ReadFile(filepath.Join(registry, "runtime-compose.json"))
	if os.IsNotExist(err) {
		return runtimeDirectoryConfig{}, nil
	}
	var config runtimeDirectoryConfig
	if err != nil || len(data) > 32768 || json.Unmarshal(data, &config) != nil {
		return config, errors.New("本机部署配置无法读取，请重新选择目录")
	}
	return config, nil
}
func (a *App) saveRuntimeDirectory(directory string) error {
	hash, err := composeFileHash(directory)
	if err != nil {
		return err
	}
	registry, err := a.projectRegistry()
	if err != nil {
		return errors.New("无法保存本机部署目录")
	}
	if err = os.MkdirAll(registry, 0700); err != nil {
		return errors.New("无法保存本机部署目录")
	}
	if atomicJSON(filepath.Join(registry, "runtime-compose.json"), runtimeDirectoryConfig{Directory: directory, ComposeHash: hash}) != nil {
		return errors.New("无法保存本机部署目录")
	}
	return nil
}

// Passive getter: opening/remounting the UI cannot queue Docker operations.
func (a *App) GetRuntimeManagement() (RuntimeManagement, error) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	state := a.runtimeState
	state.Services = append([]RuntimeService{}, state.Services...)
	if state.Phase == "" {
		state.Phase = "idle"
	}
	if !state.Busy {
		config, err := a.runtimeConfig()
		if err != nil {
			return state, err
		}
		state.Directory, state.Configured = config.Directory, config.Directory != ""
	}
	return state, nil
}
func (a *App) beginRuntimeOperation(phase string, limit time.Duration) (context.Context, error) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	if a.runtimeClosed {
		return nil, errors.New("桌面应用正在关闭")
	}
	if a.runtimeState.Busy {
		return nil, errors.New("已有服务操作正在进行，请等待完成")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	a.runtimeCancel = cancel
	if a.runtimeState.Directory == "" {
		if config, err := a.runtimeConfig(); err == nil {
			a.runtimeState.Directory, a.runtimeState.Configured = config.Directory, config.Directory != ""
		}
	}
	a.runtimeState.Busy, a.runtimeState.Phase, a.runtimeState.Message = true, phase, ""
	return ctx, nil
}
func (a *App) finishRuntimeOperation(state RuntimeManagement, err error) (RuntimeManagement, error) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	if a.runtimeCancel != nil {
		a.runtimeCancel()
		a.runtimeCancel = nil
	}
	state.Busy, state.Phase = false, "idle"
	if err != nil {
		state.Phase, state.Message = "error", err.Error()
		state.Observed, state.Running = false, false
	}
	a.runtimeState = state
	return state, err
}
func (a *App) closeRuntimeOperations() {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	a.runtimeClosed = true
	if a.runtimeCancel != nil {
		a.runtimeCancel()
	}
}
func sameRuntimePath(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	first, e1 := filepath.Abs(first)
	second, e2 := filepath.Abs(second)
	if e1 != nil || e2 != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(first); err == nil {
		first = resolved
	}
	if resolved, err := filepath.EvalSymlinks(second); err == nil {
		second = resolved
	}
	return runtimePathEqual(filepath.Clean(first), filepath.Clean(second))
}

// Include stopped containers and orphans: a fixed project name is not ownership.
func (a *App) verifyRuntimeOwnership(ctx context.Context, directory string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := a.runRuntimeDocker(ctx, "ps", "-a", "--filter", "label=com.docker.compose.project=proofcode", "--format", "{{.ID}}")
	if err != nil {
		return errors.New("无法检查容器归属，请检查 Docker Desktop")
	}
	ids := strings.Fields(output)
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > 256 {
		return errors.New("同名项目容器过多，已停止操作")
	}
	for _, id := range ids {
		if len(id) < 12 || len(id) > 64 || strings.Trim(id, "0123456789abcdef") != "" {
			return errors.New("容器标识无效，已停止操作")
		}
	}
	output, err = a.runRuntimeDocker(ctx, append([]string{"inspect", "--format", "{{json .Config.Labels}}"}, ids...)...)
	if err != nil {
		return errors.New("无法确认容器归属，已停止操作")
	}
	decoder := json.NewDecoder(strings.NewReader(output))
	for range ids {
		var labels map[string]string
		if decoder.Decode(&labels) != nil {
			return errors.New("容器归属信息无效，已停止操作")
		}
		validService := false
		for _, expected := range proofCodeComposeServices {
			if labels["com.docker.compose.service"] == expected {
				validService = true
			}
		}
		if !validService || labels["com.docker.compose.project"] != "proofcode" || !sameRuntimePath(labels["com.docker.compose.project.working_dir"], directory) || !sameRuntimePath(labels["com.docker.compose.project.config_files"], filepath.Join(directory, "compose.yml")) {
			return errors.New("发现来自其他部署目录的 proofcode 容器；请选择原部署目录，未操作任何服务")
		}
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("容器归属信息无效，已停止操作")
	}
	return nil
}
func (a *App) SelectRuntimeDirectory() (state RuntimeManagement, err error) {
	ctx, err := a.beginRuntimeOperation("selecting", time.Minute)
	if err != nil {
		return state, err
	}
	state, _ = a.GetRuntimeManagement()
	defer func() { state, err = a.finishRuntimeOperation(state, err) }()
	if a.ctx == nil {
		return state, errors.New("本机目录选择器不可用")
	}
	directory, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "选择 ProofCode Docker Compose 部署目录"})
	if err != nil {
		return state, errors.New("无法打开目录选择器")
	}
	if directory == "" {
		return state, nil
	}
	canonical, err := canonicalComposeDirectory(ctx, directory, a.runCompose)
	if err != nil {
		return state, err
	}
	if err = a.verifyRuntimeOwnership(ctx, canonical); err != nil {
		return state, err
	}
	if err = a.saveRuntimeDirectory(canonical); err != nil {
		return state, err
	}
	return RuntimeManagement{Directory: canonical, Configured: true, Services: []RuntimeService{}}, nil
}
func (a *App) validatedRuntime(ctx context.Context) (RuntimeManagement, error) {
	config, err := a.runtimeConfig()
	state := RuntimeManagement{Directory: config.Directory, Configured: config.Directory != ""}
	if err != nil {
		return state, err
	}
	if !state.Configured {
		return state, errors.New("请先选择 ProofCode Compose 部署目录")
	}
	hash, err := composeFileHash(config.Directory)
	if err != nil {
		return state, err
	}
	if hash != config.ComposeHash {
		return state, errors.New("部署配置已变化，请重新选择目录后再操作")
	}
	canonical, err := canonicalComposeDirectory(ctx, config.Directory, a.runCompose)
	if err != nil {
		return state, err
	}
	state.Directory = canonical
	if err = a.verifyRuntimeOwnership(ctx, canonical); err != nil {
		return state, err
	}
	return state, nil
}
func (a *App) observeRuntime(ctx context.Context, state RuntimeManagement) (RuntimeManagement, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := a.runCompose(ctx, state.Directory, "ps", "--all", "--format", "json")
	if err != nil {
		return state, errors.New("无法读取实际服务状态，请重新检查")
	}
	type row struct {
		Service string
		State   string
		Health  string
	}
	var rows []row
	if strings.HasPrefix(strings.TrimSpace(output), "[") {
		if json.Unmarshal([]byte(output), &rows) != nil {
			return state, errors.New("服务状态格式无效")
		}
	} else {
		decoder := json.NewDecoder(strings.NewReader(output))
		for {
			var value row
			err := decoder.Decode(&value)
			if err == io.EOF {
				break
			}
			if err != nil {
				return state, errors.New("服务状态格式无效")
			}
			rows = append(rows, value)
		}
	}
	state.Services = []RuntimeService{}
	state.Running = true
	for _, name := range proofCodeComposeServices {
		service := RuntimeService{Name: name, State: "not_created"}
		count := 0
		for _, value := range rows {
			if value.Service == name {
				count++
				if count == 1 {
					service.State, service.Health = value.State, value.Health
				}
				if value.State != "running" || (value.Health != "" && value.Health != "healthy") {
					service.State, service.Health = value.State, value.Health
					break
				}
			}
		}
		if service.State != "running" || (service.Health != "" && service.Health != "healthy") {
			state.Running = false
		}
		state.Services = append(state.Services, service)
	}
	state.Observed, state.CheckedAt = true, time.Now().UTC().Format(time.RFC3339)
	return state, nil
}
func (a *App) RefreshRuntimeManagement() (state RuntimeManagement, err error) {
	ctx, err := a.beginRuntimeOperation("checking", time.Minute)
	if err != nil {
		return state, err
	}
	defer func() { state, err = a.finishRuntimeOperation(state, err) }()
	state, err = a.validatedRuntime(ctx)
	if err != nil {
		return state, err
	}
	return a.observeRuntime(ctx, state)
}

// Stop preserves containers, networks, images and volumes; never down or prune.
func (a *App) ManageRuntime(action string) (state RuntimeManagement, err error) {
	if action != "start" && action != "stop" {
		return state, errors.New("不支持的服务操作")
	}
	phase, limit := "stopping", 3*time.Minute
	if action == "start" {
		phase, limit = "starting", 15*time.Minute
	}
	ctx, err := a.beginRuntimeOperation(phase, limit)
	if err != nil {
		return state, err
	}
	defer func() { state, err = a.finishRuntimeOperation(state, err) }()
	state, err = a.validatedRuntime(ctx)
	if err != nil {
		return state, err
	}
	a.runtimeMu.Lock()
	a.runtimeState.Directory, a.runtimeState.Configured = state.Directory, state.Configured
	a.runtimeMu.Unlock()
	args := []string{"stop"}
	if action == "start" {
		args = []string{"up", "-d", "--build"}
		offline, checkErr := a.offlineRuntime(ctx, state.Directory)
		if checkErr != nil {
			return state, checkErr
		}
		if offline {
			args = []string{"up", "-d", "--no-build", "--pull", "never"}
		}
	}
	if _, err = a.runCompose(ctx, state.Directory, args...); err != nil {
		if ctx.Err() != nil {
			return state, errors.New("服务操作超时或桌面应用已关闭，请重新检查实际状态")
		}
		return state, errors.New("服务操作失败，请检查 Docker Desktop、镜像构建与端口占用后重新检查状态")
	}
	state, err = a.observeRuntime(ctx, state)
	if err == nil {
		state.Message = "操作已结束，已读取实际服务状态。"
		if action == "start" && !state.Running {
			state.Message = "启动命令已结束，部分服务尚未就绪，请稍后重新检查。"
		}
	}
	return state, err
}
