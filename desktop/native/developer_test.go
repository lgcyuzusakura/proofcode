package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitOriginDisplayDoesNotExposeCredentials(t *testing.T) {
	for _, value := range []string{"https://secret-token@github.com/user/repo.git", "https://user:secret-token@example.com/repo?token=secret-token#secret-token"} {
		if strings.Contains(displayGitOrigin(value), "secret-token") {
			t.Fatal("Git origin credentials exposed")
		}
	}
	for _, value := range []string{"ssh://git@github.com/user/repo.git", "git@github.com:user/repo.git", "https://github.com/user/repo.git"} {
		if displayGitOrigin(value) != value {
			t.Fatal("ordinary Git origin altered")
		}
	}
}

func TestEditorConflictHistoryAndIsolation(t *testing.T) {
	app := localTestApp(t)
	dir := t.TempDir()
	p, err := app.registerDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(dir, "hello.txt")
	if err = os.WriteFile(original, []byte("before"), 0644); err != nil {
		t.Fatal(err)
	}
	f, err := app.ReadProjectFile(p.LocalHandle, "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(original, []byte("external"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = app.SaveProjectFile(p.LocalHandle, f.Path, f.Hash, "draft"); err == nil {
		t.Fatal("overwrote external change")
	}
	current, _ := os.ReadFile(original)
	if string(current) != "external" {
		t.Fatal("lost external content")
	}
	for _, path := range []string{"../escape", ".env", ".git/config"} {
		if _, err = app.SaveProjectFile(p.LocalHandle, path, "", "private"); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	f, err = app.SaveProjectFile(p.LocalHandle, "src/new.txt", "", "你好\n")
	if err != nil {
		t.Fatal(err)
	}
	reread, err := app.ReadProjectFile(p.LocalHandle, f.Path)
	if err != nil || reread != f {
		t.Fatalf("roundtrip %v %+v", err, reread)
	}
	journals, _ := filepath.Glob(filepath.Join(dir, ".proofcode", "editor-history", "*.json"))
	if len(journals) != 1 {
		t.Fatalf("missing history %v", journals)
	}
	if _, err = app.ReadProjectFile("unknown", f.Path); err == nil {
		t.Fatal("accepted unknown project")
	}
}

func TestGitExactStageVersionCheckAndBranches(t *testing.T) {
	app := localTestApp(t)
	dir := t.TempDir()
	p, err := app.registerDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.GetProjectGit(p.LocalHandle)
	if err != nil {
		t.Fatal(err)
	}
	state, err = app.ProjectGitAction(p.LocalHandle, "init", state.Revision, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"config", "user.name", "ProofCode test"}, {"config", "user.email", "test@example.invalid"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	if err = os.MkdirAll(filepath.Join(dir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"code.txt", ".env"} {
		if err = os.WriteFile(filepath.Join(dir, "src", name), []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	state, err = app.GetProjectGit(p.LocalHandle)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 2 {
		t.Fatalf("not exact files: %+v", state.Files)
	}
	if _, err = app.ProjectGitAction(p.LocalHandle, "stage", state.Revision, "", []string{"src"}); err == nil {
		t.Fatal("directory stage allowed")
	}
	if _, err = app.ProjectGitAction(p.LocalHandle, "stage", state.Revision, "", []string{"src/.env"}); err == nil {
		t.Fatal("credential stage allowed")
	}
	if err = os.WriteFile(filepath.Join(dir, "src", "code.txt"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = app.ProjectGitAction(p.LocalHandle, "stage", state.Revision, "", []string{"src/code.txt"}); err == nil {
		t.Fatal("stale Git action accepted")
	}
	state, _ = app.GetProjectGit(p.LocalHandle)
	state, err = app.ProjectGitAction(p.LocalHandle, "stage", state.Revision, "", []string{"src/code.txt"})
	if err != nil {
		t.Fatal(err)
	}
	state, err = app.ProjectGitAction(p.LocalHandle, "commit", state.Revision, "initial", nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err = app.ProjectGitAction(p.LocalHandle, "create_branch", state.Revision, "feature/test", nil)
	if err != nil || state.Branch != "feature/test" {
		t.Fatalf("branch: %v %+v", err, state)
	}
	state, err = app.ProjectGitAction(p.LocalHandle, "switch_branch", state.Revision, "main", nil)
	if err != nil || len(state.Commits) != 1 {
		t.Fatalf("history: %v %+v", err, state)
	}
	child, _ := app.registerDirectory(filepath.Join(dir, "src"))
	if _, err = app.GetProjectGit(child.LocalHandle); err == nil {
		t.Fatal("parent repository index exposed")
	}
}

func TestCommandOutputStopAndProjectScope(t *testing.T) {
	app := localTestApp(t)
	p, err := app.registerDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer app.shutdown(context.Background())
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node required")
	}
	state, err := app.StartProjectCommand(p.LocalHandle, node, []string{"-e", "require('child_process').spawn(process.execPath,['-e',\"setInterval(()=>require('fs').appendFileSync('heartbeat.txt','x'),40)\"],{stdio:'inherit'}); console.log('proofcode-command'); setInterval(()=>{},1000)"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.StartProjectCommand(p.LocalHandle, node, nil); err == nil {
		t.Fatal("parallel command allowed")
	}
	if err = app.StopProjectCommand(p.LocalHandle, "wrong-id"); err == nil {
		t.Fatal("wrong session allowed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := app.GetProjectCommand(p.LocalHandle)
		if strings.Contains(current.Output, "proofcode-command") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	current, _ := app.GetProjectCommand(p.LocalHandle)
	if !strings.Contains(current.Output, "proofcode-command") {
		t.Fatal("no real command output")
	}
	heartbeat := filepath.Join(p.Path, "heartbeat.txt")
	for time.Now().Before(deadline) {
		if info, err := os.Stat(heartbeat); err == nil && info.Size() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if info, err := os.Stat(heartbeat); err != nil || info.Size() == 0 {
		t.Fatal("child process did not start")
	}
	if err = app.StopProjectCommand(p.LocalHandle, state.ID); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		current, _ = app.GetProjectCommand(p.LocalHandle)
		if !current.Running {
			time.Sleep(80 * time.Millisecond)
			before, _ := os.Stat(heartbeat)
			time.Sleep(160 * time.Millisecond)
			after, _ := os.Stat(heartbeat)
			if before.Size() != after.Size() {
				t.Fatal("child process survived stop")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("command did not stop")
}

func TestRealProjectBrowserClickInputScreenshotAndScope(t *testing.T) {
	if browserExecutable() == "" {
		t.Skip("Chrome or Edge required")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><title>ProofCode browser test</title><body><input style="position:absolute;left:10px;top:10px;width:200px;height:40px" oninput="document.getElementById('value').innerText=this.value"><div id="value"></div><button style="position:absolute;left:10px;top:100px;width:200px;height:40px" onclick="document.getElementById('value').innerText='clicked'">Run</button><script>console.log('proofcode-browser')</script></body></html>`))
	}))
	defer server.Close()
	app := localTestApp(t)
	p, _ := app.registerDirectory(t.TempDir())
	other, _ := app.registerDirectory(t.TempDir())
	defer app.shutdown(context.Background())
	for _, url := range []string{"file:///C:/private", "https://user:password@example.com", "javascript:alert(1)"} {
		if _, err := app.ProjectBrowser(p.LocalHandle, "open", url, 0, 0); err == nil {
			t.Fatalf("accepted URL %s", url)
		}
	}
	state, err := app.ProjectBrowser(p.LocalHandle, "open", server.URL, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if state.Title != "ProofCode browser test" || !strings.Contains(strings.Join(state.Console, "\n"), "proofcode-browser") {
		t.Fatalf("browser: %+v", state.Console)
	}
	image, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(state.Image, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(image))
	if err != nil || cfg.Width != 1280 || cfg.Height != 800 {
		t.Fatalf("screenshot %v %+v", err, cfg)
	}
	if _, err = app.ProjectBrowser(other.LocalHandle, "inspect", "", 0, 0); err == nil {
		t.Fatal("browser project scope leaked")
	}
	if _, err = app.ProjectBrowser(p.LocalHandle, "click", "", 40, 30); err != nil {
		t.Fatal(err)
	}
	state, err = app.ProjectBrowser(p.LocalHandle, "type", "typed", 0, 0)
	if err != nil || !strings.Contains(state.Text, "typed") {
		t.Fatalf("input: %v %s", err, state.Text)
	}
	state, err = app.ProjectBrowser(p.LocalHandle, "click", "", 40, 120)
	if err != nil || !strings.Contains(state.Text, "clicked") {
		t.Fatalf("click: %v %s", err, state.Text)
	}
	if _, err = app.ProjectBrowser(p.LocalHandle, "close", "", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = app.ProjectBrowser(p.LocalHandle, "inspect", "", 0, 0); err == nil {
		t.Fatal("closed browser remained available")
	}
}
