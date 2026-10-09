package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Opt-in UI integration: React drives the real App methods through a temporary
// loopback test transport. Wails production bindings are checked by its build.
func TestDeveloperUIEndToEnd(t *testing.T) {
	if os.Getenv("PROOFCODE_UI_SMOKE") != "1" {
		t.Skip("opt-in: requires Vite, disposable control plane, Edge and Playwright")
	}
	app := localTestApp(t)
	dir := t.TempDir()
	project, err := app.registerDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.shutdown(context.Background())
	if err = os.WriteFile(filepath.Join(dir, "hello.ts"), []byte("export const initial = true;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, err := app.ProjectGitAction(project.LocalHandle, "init", "", "", nil)
	if err != nil || !state.Initialized {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"config", "user.name", "ProofCode UI fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Git config: %v %s", err, out)
		}
	}
	bridgeToken, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"ListLocalProjects", "CaptureProjectSource", "ApplyProjectPatch", "ListProjectFiles", "ReadProjectFile", "SaveProjectFile", "GetProjectGit", "ProjectGitAction", "StartProjectCommand", "GetProjectCommand", "StopProjectCommand", "ProjectBrowser", "PreviewProjectMerge", "ResolveProjectMerge", "ApplyProjectMerge", "DiscardProjectMerge", "StartProjectProtocol", "ProjectProtocolRequest", "GetProjectProtocol", "StopProjectProtocol"}
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := os.Getenv("PROOFCODE_UI_URL")
		if origin == "" {
			origin = "http://127.0.0.1:15173"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		if r.URL.Path == "/preview" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<title>ProofCode UI preview</title><h1>Real native browser</h1><script>console.log('preview-ok')</script>`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+bridgeToken || r.Method != "POST" || r.URL.Path != "/rpc" {
			http.Error(w, "forbidden", 403)
			return
		}
		var request struct {
			Method string            `json:"method"`
			Args   []json.RawMessage `json:"args"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&request); err != nil || !allowed[request.Method] {
			http.Error(w, "invalid test method", 400)
			return
		}
		method := reflect.ValueOf(app).MethodByName(request.Method)
		if len(request.Args) != method.Type().NumIn() {
			http.Error(w, "invalid argument count", 400)
			return
		}
		inputs := make([]reflect.Value, len(request.Args))
		for i, arg := range request.Args {
			v := reflect.New(method.Type().In(i))
			if err := json.Unmarshal(arg, v.Interface()); err != nil {
				http.Error(w, "invalid argument", 400)
				return
			}
			inputs[i] = v.Elem()
		}
		values := method.Call(inputs)
		result := map[string]any{}
		if last := values[len(values)-1]; !last.IsNil() {
			result["error"] = last.Interface().(error).Error()
		} else if len(values) > 1 {
			result["value"] = values[0].Interface()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer func() {
		server.CloseClientConnections()
		server.Close()
	}()
	script, err := filepath.Abs(filepath.Join("..", "..", "smoke", "developer", "ui.cjs"))
	if err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"bridge": server.URL, "bridgeToken": bridgeToken, "handle": project.LocalHandle, "names": names, "python": os.Getenv("PROOFCODE_IDE_TEST_PYTHON")})
	cmd := exec.Command("node", script)
	cmd.Env = append(os.Environ(), "PROOFCODE_UI_NATIVE="+string(config))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("UI integration: %v\n%s", err, out)
	}
	t.Log(string(out))
	saved, err := os.ReadFile(filepath.Join(dir, "hello.ts"))
	if err != nil || string(saved) != "export const proof = \"草稿恢复\";\n" {
		t.Fatalf("UI did not persist source: %v %s", err, saved)
	}
	state, err = app.GetProjectGit(project.LocalHandle)
	if err != nil || len(state.Commits) == 0 {
		t.Fatalf("UI did not make real Git commit: %v %+v", err, state)
	}
	if os.Getenv("PROOFCODE_IDE_TEST_PYTHON") != "" {
		cmd := exec.Command("git", "log", "--all", "--format=%s")
		cmd.Dir = dir
		history, e := cmd.Output()
		if e != nil || !strings.Contains(string(history), "Merge colleague (reviewed in ProofCode)") || !strings.Contains(string(history), "Add debugger fixture") {
			t.Fatalf("UI did not persist reviewed merge and debugger fixture: %v %s", e, history)
		}
		cmd = exec.Command("git", "rev-list", "--parents", "-n", "1", "HEAD~1")
		cmd.Dir = dir
		parents, e := cmd.Output()
		if e != nil || len(strings.Fields(string(parents))) != 3 {
			t.Fatalf("UI merge must have two parents: %v %s", e, parents)
		}
	}
	if _, err = os.Stat(filepath.Join(dir, "app.py")); err != nil {
		t.Fatalf("review did not apply real agent patch: %v", err)
	}
}
