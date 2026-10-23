package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type standaloneRun struct {
	baseURL, token       string
	java, worker         *exec.Cmd
	javaDone, workerDone chan error
	cancel               context.CancelFunc
	log                  *os.File
	runDir               string
	lock                 io.Closer
	job                  *standaloneJob
}

type standaloneSecrets struct {
	DatabasePassword string `json:"databasePassword"`
	SnapshotKey      string `json:"snapshotKey"`
}

func standaloneSecret() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data[:]), nil
}

func standaloneEnvironment() []string {
	var result []string
	for _, entry := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(entry, "=", 2)[0])
		blocked := false
		for _, prefix := range []string{"SPRING_", "SERVER_", "MANAGEMENT_", "PROOFCODE_", "RUNNER_", "MODEL_", "JEV_", "DATA_", "CONTROL_PLANE_", "ARTEMIS_", "WORKSPACE_ROOT", "AGENT_QUEUE", "JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS", "_JAVA_OPTIONS"} {
			if strings.HasPrefix(key, prefix) {
				blocked = true
				break
			}
		}
		if !blocked {
			result = append(result, entry)
		}
	}
	return result
}

func (a *App) standaloneDirectory() (string, error) {
	registry, err := a.projectRegistry()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(registry, "standalone")
	if err = os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	if err = protectStandaloneDirectory(directory); err != nil {
		return "", err
	}
	return directory, nil
}

// A missing sidecar leaves development and explicitly configured team backends intact.
// The packaged edition ships a private Java runtime, the original control plane,
// the original Agent runner and MCP helper next to the desktop executable.
func (a *App) GetStandaloneSession() (map[string]any, error) {
	return a.openStandaloneSession(false)
}

func (a *App) StartStandaloneSession() (map[string]any, error) {
	return a.openStandaloneSession(true)
}

func (a *App) openStandaloneSession(explicit bool) (map[string]any, error) {
	a.standaloneMu.Lock()
	defer a.standaloneMu.Unlock()
	if a.standaloneClosed {
		return nil, errors.New("桌面应用正在关闭")
	}
	if a.standalone == nil {
		a.runtimeMu.Lock()
		connection, err := a.runtimeConnection()
		a.runtimeMu.Unlock()
		if err != nil {
			return nil, err
		}
		if !explicit && connection.Source != "default" {
			return nil, nil
		}
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		bundle := filepath.Join(filepath.Dir(executable), "standalone")
		if _, err = os.Stat(filepath.Join(bundle, "control-plane.jar")); os.IsNotExist(err) {
			if explicit {
				return nil, errors.New("此版本缺少内置服务，请使用完整独立桌面发行包")
			}
			return nil, nil
		}
		ctx := a.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		boot, stop := context.WithTimeout(ctx, 90*time.Second)
		defer stop()
		run, err := a.startStandalone(boot, bundle)
		if err != nil {
			return nil, err
		}
		a.standalone = run
		if explicit {
			registry, err := a.projectRegistry()
			if err == nil {
				err = atomicJSON(filepath.Join(registry, "runtime-mode.json"), map[string]string{"mode": "standalone"})
			}
			if err != nil {
				run.close()
				a.standalone = nil
				return nil, errors.New("无法保存本机运行模式")
			}
		}
	}
	run := a.standalone
	request, err := http.NewRequest(http.MethodPost, run.baseURL+"/internal/desktop-session", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+run.token)
	response, err := (&http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		return nil, errors.New("内置服务尚未就绪，请重启应用")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("无法打开本机工作区")
	}
	var session map[string]any
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&session); err != nil {
		return nil, errors.New("本机工作区响应无效")
	}
	return session, nil
}

func (a *App) startStandalone(boot context.Context, bundle string) (_ *standaloneRun, resultErr error) {
	for _, name := range []string{"java/bin/java.exe", "control-plane.jar", "proofcode-runner.exe", "proofcode-mcp-inspect.exe", "rg.exe"} {
		info, err := os.Stat(filepath.Join(bundle, filepath.FromSlash(name)))
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("内置运行文件不完整，请重新解压完整桌面发行包")
		}
	}
	directory, err := a.standaloneDirectory()
	if err != nil {
		return nil, errors.New("无法创建本机数据目录")
	}
	lock, err := lockStandaloneDirectory(directory)
	if err != nil {
		return nil, errors.New("本机服务已在运行，请使用已打开的窗口")
	}
	defer func() {
		if resultErr != nil {
			lock.Close()
		}
	}()
	var secrets standaloneSecrets
	secretFile := filepath.Join(directory, "credentials.local.json")
	data, err := os.ReadFile(secretFile)
	if os.IsNotExist(err) {
		secrets.DatabasePassword, err = standaloneSecret()
		if err != nil {
			return nil, err
		}
		secrets.SnapshotKey, err = standaloneSecret()
		if err != nil {
			return nil, err
		}
		if err = atomicJSON(secretFile, secrets); err != nil {
			return nil, err
		}
	} else if err != nil || json.Unmarshal(data, &secrets) != nil || secrets.DatabasePassword == "" || secrets.SnapshotKey == "" {
		return nil, errors.New("本机密钥无法读取，已有数据已保留")
	}
	runDir, err := os.MkdirTemp(directory, "run-")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &standaloneRun{cancel: cancel, runDir: runDir, lock: lock}
	defer func() {
		if resultErr != nil {
			run.close()
		}
	}()
	run.token, err = standaloneSecret()
	if err != nil {
		return nil, err
	}
	run.job, err = newStandaloneJob()
	if err != nil {
		return nil, errors.New("无法隔离本机服务进程")
	}
	// Native Git/patch bindings and the worker share the shipped Git distribution.
	gitDirectory := filepath.Join(bundle, "git", "cmd")
	if _, err = os.Stat(filepath.Join(gitDirectory, "git.exe")); err != nil {
		return nil, errors.New("内置 Git 文件不完整")
	}
	path := os.Getenv("PATH")
	if !strings.Contains(strings.ToLower(path), strings.ToLower(gitDirectory)) {
		_ = os.Setenv("PATH", bundle+string(os.PathListSeparator)+gitDirectory+string(os.PathListSeparator)+filepath.Join(bundle, "java", "bin")+string(os.PathListSeparator)+path)
	}
	// Rotate the startup log; the Java and worker command lines contain no secrets.
	logPath := filepath.Join(directory, "runtime.log")
	_ = os.Remove(logPath + ".previous")
	if err = os.Rename(logPath, logPath+".previous"); err != nil && !os.IsNotExist(err) {
		return nil, errors.New("本机服务已在运行，请使用已打开的窗口")
	}
	run.log, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	portFile := filepath.Join(runDir, "port")
	environment := standaloneEnvironment()
	environment = append(environment, "SPRING_PROFILES_ACTIVE=desktop", "PROOFCODE_DESKTOP_DATA="+filepath.ToSlash(directory), "PROOFCODE_DESKTOP_PORT_FILE="+portFile, "PROOFCODE_DATABASE_PASSWORD="+secrets.DatabasePassword, "DATA_SNAPSHOT_KEY="+secrets.SnapshotKey, "PROOFCODE_AUTH_TOKEN=change-me", "PROOFCODE_RUNNER_TOKEN="+run.token, "PROOFCODE_MCP_INSPECT_BIN="+filepath.Join(bundle, "proofcode-mcp-inspect.exe"), "SPRING_JPA_OPEN_IN_VIEW=false")
	run.java = exec.CommandContext(ctx, filepath.Join(bundle, "java", "bin", "java.exe"), "-Xmx384m", "-jar", filepath.Join(bundle, "control-plane.jar"))
	run.java.Env = environment
	run.java.Dir = directory
	run.java.Stdout = run.log
	run.java.Stderr = run.log
	configureRuntimeCommand(run.java)
	if err = run.java.Start(); err != nil {
		return nil, errors.New("内置服务未能启动")
	}
	run.javaDone = make(chan error, 1)
	go func() { run.javaDone <- run.java.Wait() }()
	if err = run.job.attach(run.java); err != nil {
		return nil, errors.New("无法隔离内置控制面进程")
	}
	client := &http.Client{Timeout: time.Second}
	for {
		if data, readErr := os.ReadFile(portFile); readErr == nil {
			port, parseErr := strconv.Atoi(string(data))
			if parseErr != nil || port < 1 || port > 65535 {
				return nil, errors.New("内置服务端口无效")
			}
			run.baseURL = "http://127.0.0.1:" + strconv.Itoa(port)
			request, _ := http.NewRequestWithContext(boot, http.MethodGet, run.baseURL+"/actuator/health", nil)
			response, probeErr := client.Do(request)
			if probeErr == nil {
				response.Body.Close()
				if response.StatusCode == 200 {
					break
				}
			}
		}
		select {
		case <-boot.Done():
			return nil, errors.New("内置服务启动超时，请查看本机 runtime.log")
		case <-run.javaDone:
			run.javaDone = nil
			return nil, errors.New("内置服务已退出，请查看本机 runtime.log")
		case <-time.After(200 * time.Millisecond):
		}
	}
	run.worker = exec.CommandContext(ctx, filepath.Join(bundle, "proofcode-runner.exe"))
	run.worker.Env = append(standaloneEnvironment(), "RUNNER_TRANSPORT=local", "CONTROL_PLANE_URL="+run.baseURL, "RUNNER_TOKEN="+run.token, "WORKSPACE_ROOT="+filepath.Join(directory, "workspaces"), "RUNNER_ALLOW_WRITE=false", "RUNNER_ALLOW_EXEC=false", "RUNNER_BROWSER_ENABLED=true", "RUNNER_BROWSER_MODE=shared")
	run.worker.Dir = directory
	run.worker.Stdout = run.log
	run.worker.Stderr = run.log
	configureRuntimeCommand(run.worker)
	if err = run.worker.Start(); err != nil {
		return nil, errors.New("Agent 引擎未能启动")
	}
	run.workerDone = make(chan error, 1)
	go func() { run.workerDone <- run.worker.Wait() }()
	if err = run.job.attach(run.worker); err != nil {
		return nil, errors.New("无法隔离 Agent 引擎进程")
	}
	return run, nil
}

func (run *standaloneRun) close() {
	// Stop only the processes launched by this instance. Files and pending tasks
	// remain durable for the next launch; unrelated services are never enumerated.
	if run.job != nil {
		run.job.close()
	}
	if run.cancel != nil {
		run.cancel()
	}
	for _, done := range []chan error{run.workerDone, run.javaDone} {
		if done != nil {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
			}
		}
	}
	if run.log != nil {
		run.log.Close()
	}
	if run.lock != nil {
		run.lock.Close()
		run.lock = nil
	}
	if run.runDir != "" {
		_ = os.Remove(filepath.Join(run.runDir, "port"))
		_ = os.Remove(run.runDir)
	}
}

func (a *App) closeStandalone() {
	a.standaloneMu.Lock()
	defer a.standaloneMu.Unlock()
	a.standaloneClosed = true
	if a.standalone != nil {
		a.standalone.close()
		a.standalone = nil
	}
}
