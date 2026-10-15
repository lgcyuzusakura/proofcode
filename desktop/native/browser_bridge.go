package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	bridge "github.com/proofcode-dev/proofcode/agent-engine/pkg/browserbridge"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type browserBroker struct {
	server     *http.Server
	descriptor bridge.Descriptor
	path       string
	projects   map[string]string
}

// BindProjectBrowser is reachable only through the native UI binding, never HTTP.
// A runner cannot register an arbitrary path or acquire another project's tab.
func (a *App) BindProjectBrowser(projectID, handle string) error {
	if !bridge.ProjectID.MatchString(projectID) {
		return errors.New("项目 ID 无效")
	}
	a.projectMu.Lock()
	_, err := a.projectByHandle(handle)
	a.projectMu.Unlock()
	if err != nil {
		return err
	}
	a.browserMu.Lock()
	defer a.browserMu.Unlock()
	if a.broker == nil {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return err
		}
		secret := make([]byte, 32)
		if _, err = rand.Read(secret); err != nil {
			listener.Close()
			return err
		}
		p, err := bridge.DescriptorPath()
		if err != nil {
			listener.Close()
			return err
		}
		b := &browserBroker{descriptor: bridge.Descriptor{URL: "http://" + listener.Addr().String(), Token: hex.EncodeToString(secret)}, path: p, projects: map[string]string{}}
		b.server = &http.Server{Handler: http.HandlerFunc(a.browserBridgeRequest), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 30 * time.Second}
		data, _ := json.Marshal(b.descriptor)
		if err = os.MkdirAll(filepath.Dir(p), 0700); err == nil {
			err = os.WriteFile(p, data, 0600)
		}
		if err != nil {
			listener.Close()
			return errors.New("无法保存共享浏览器本机描述文件")
		}
		a.broker = b
		go b.server.Serve(listener)
	}
	if old, ok := a.broker.projects[projectID]; ok && old != handle {
		return errors.New("项目已绑定其他工程")
	}
	for id, h := range a.broker.projects {
		if h == handle && id != projectID {
			return errors.New("该工程已绑定其他项目")
		}
	}
	a.broker.projects[projectID] = handle
	return nil
}
func (a *App) browserBridgeRequest(w http.ResponseWriter, r *http.Request) {
	// No CORS: ordinary websites cannot use a native capability. Check the Host
	// as well, preventing DNS rebinding to this loopback service.
	a.browserMu.Lock()
	b := a.broker
	if b == nil {
		a.browserMu.Unlock()
		http.Error(w, "offline", 503)
		return
	}
	expectedHost := strings.TrimPrefix(b.descriptor.URL, "http://")
	authOK := subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+b.descriptor.Token)) == 1
	parts := strings.Split(r.URL.Path, "/")
	handle := ""
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "browser" && bridge.ProjectID.MatchString(parts[2]) {
		handle = b.projects[parts[2]]
	}
	a.browserMu.Unlock()
	if r.Host != expectedHost || r.Header.Get("Origin") != "" || !authOK {
		http.Error(w, "forbidden", 403)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	if handle == "" {
		http.Error(w, "scope", 404)
		return
	}
	var action bridge.Action
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&action) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid", 400)
		return
	}
	state, err := a.projectBrowser(handle, action.Action, action.Value, action.Selector, action.X, action.Y, "agent")
	if err != nil {
		if errors.Is(err, errHumanControl) {
			http.Error(w, "human control", 409)
		} else {
			http.Error(w, "browser operation failed", 422)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}
func (a *App) stopBrowserBroker() {
	if a.broker == nil {
		return
	}
	b := a.broker
	a.broker = nil
	// Close, not Shutdown while holding browserMu: pending handlers may need it.
	_ = b.server.Close()
	current, err := os.ReadFile(b.path)
	var d bridge.Descriptor
	if err == nil && json.Unmarshal(current, &d) == nil && d.Token == b.descriptor.Token {
		_ = os.Remove(b.path)
	}
}

var errHumanControl = errors.New("浏览器正在人工接管，请先交还 AI 控制")
