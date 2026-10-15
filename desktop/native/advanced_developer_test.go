package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func mergeFixture(t *testing.T, base, ours, theirs string) (*App, ScratchProject) {
	t.Helper()
	app := localTestApp(t)
	dir := t.TempDir()
	p, err := app.registerDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, dir, "init", "-b", "main")
	fixtureGit(t, dir, "config", "user.name", "Merge fixture")
	fixtureGit(t, dir, "config", "user.email", "fixture@example.invalid")
	fixtureGit(t, dir, "config", "core.autocrlf", "false")
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(dir, "billing.js"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		fixtureGit(t, dir, "add", "billing.js")
		fixtureGit(t, dir, "commit", "-m", "fixture")
	}
	write(base)
	fixtureGit(t, dir, "branch", "colleague")
	write(ours)
	fixtureGit(t, dir, "switch", "colleague")
	write(theirs)
	fixtureGit(t, dir, "switch", "main")
	t.Cleanup(func() { app.shutdown(context.Background()) })
	return app, p
}
func TestMergeIndependentFunctionsAndApprovalIsolation(t *testing.T) {
	t.Setenv("PROOFCODE_MERGIRAF_EXECUTABLE", "")
	base := "function pay() {\n  return 1;\n}\n\n// independent boundary\n\nfunction orders() {\n  return 10;\n}\n"
	app, p := mergeFixture(t, base, strings.Replace(base, "return 1;", "return 2;", 1), strings.Replace(base, "return 10;", "return 20;", 1))
	state, err := app.GetProjectGit(p.LocalHandle)
	if err != nil {
		t.Fatal(err)
	}
	before := fixtureGit(t, p.Path, "rev-parse", "HEAD")
	index := fixtureGit(t, p.Path, "ls-files", "--stage")
	contents, _ := os.ReadFile(filepath.Join(p.Path, "billing.js"))
	preview, err := app.PreviewProjectMerge(p.LocalHandle, state.Revision, "colleague")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Ready || len(preview.Files) != 0 {
		t.Fatalf("independent functions did not merge %+v", preview)
	}
	current, _ := os.ReadFile(filepath.Join(p.Path, "billing.js"))
	if string(current) != string(contents) || fixtureGit(t, p.Path, "rev-parse", "HEAD") != before || fixtureGit(t, p.Path, "ls-files", "--stage") != index {
		t.Fatal("unapproved merge mutated checkout")
	}
	if _, err = app.ApplyProjectMerge("wrong", preview.ID, preview.Revision); err == nil {
		t.Fatal("cross project approval")
	}
	if _, err = app.ApplyProjectMerge(p.LocalHandle, preview.ID, "stale"); err == nil {
		t.Fatal("stale preview approval")
	}
	result, err := app.ApplyProjectMerge(p.LocalHandle, preview.ID, preview.Revision)
	if err != nil {
		t.Fatal(err)
	}
	merged, _ := os.ReadFile(filepath.Join(p.Path, "billing.js"))
	if !strings.Contains(string(merged), "return 2;") || !strings.Contains(string(merged), "return 20;") || len(result.Files) != 0 {
		t.Fatalf("lost merge %s %+v", merged, result)
	}
	parents := strings.Fields(fixtureGit(t, p.Path, "rev-list", "--parents", "-n", "1", "HEAD"))
	if len(parents) != 3 {
		t.Fatal("merge history parents lost")
	}
	if fixtureGit(t, p.Path, "rev-parse", "refs/proofcode/backups/"+preview.ID) != before {
		t.Fatal("missing backup")
	}
}
func TestStructuredMergeAndUnresolvedRealConflict(t *testing.T) {
	engine := os.Getenv("PROOFCODE_MERGIRAF_EXECUTABLE")
	if engine == "" {
		t.Skip("set real Mergiraf executable")
	}
	app, p := mergeFixture(t, "function notify(status) { return status; }\n", "function notify(status) { return status + 1; }\n", "function notify(status, page) { return status; }\n")
	state, _ := app.GetProjectGit(p.LocalHandle)
	preview, err := app.PreviewProjectMerge(p.LocalHandle, state.Revision, "colleague")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Ready || len(preview.Files) != 1 || preview.Files[0].Method != "mergiraf" || !strings.Contains(preview.Files[0].Result, "status + 1") || !strings.Contains(preview.Files[0].Result, "status, page") {
		t.Fatalf("real AST merge failed %+v", preview)
	}
	// Both sides changing the same expression must retain a conflict.
	app2, p2 := mergeFixture(t, "function pay() { return 1; }\n", "function pay() { return 2; }\n", "function pay() { return 3; }\n")
	s2, _ := app2.GetProjectGit(p2.LocalHandle)
	conflict, err := app2.PreviewProjectMerge(p2.LocalHandle, s2.Revision, "colleague")
	if err != nil {
		t.Fatal(err)
	}
	if conflict.Ready {
		t.Fatal("real business conflict silently resolved")
	}
	if _, err = app2.ApplyProjectMerge(p2.LocalHandle, conflict.ID, conflict.Revision); err == nil {
		t.Fatal("unresolved approval allowed")
	}
	if _, err = app2.ResolveProjectMerge(p2.LocalHandle, conflict.ID, conflict.Revision, "billing.js", conflict.Files[0].Result); err == nil {
		t.Fatal("markers accepted")
	}
	resolved, err := app2.ResolveProjectMerge(p2.LocalHandle, conflict.ID, conflict.Revision, "billing.js", "function pay() { return 4; }\n")
	if err != nil || !resolved.Ready {
		t.Fatal(err)
	}
	fixtureGit(t, p2.Path, "switch", "colleague")
	if _, err = app2.ApplyProjectMerge(p2.LocalHandle, resolved.ID, resolved.Revision); err == nil {
		t.Fatal("branch changed approval allowed")
	}
}
func TestMergePreviewStagedMutationRejected(t *testing.T) {
	app, p := mergeFixture(t, "one\n\n\n\n\nfive\n", "two\n\n\n\n\nfive\n", "one\n\n\n\n\nsix\n")
	state, _ := app.GetProjectGit(p.LocalHandle)
	preview, err := app.PreviewProjectMerge(p.LocalHandle, state.Revision, "colleague")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(preview.Path, "billing.js"), []byte("external staged edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, preview.Path, "add", "billing.js")
	fixtureGit(t, preview.Path, "checkout", preview.Ours, "--", "billing.js")
	if _, err = app.ApplyProjectMerge(p.LocalHandle, preview.ID, preview.Revision); err == nil {
		t.Fatal("preview mutation accepted")
	}
}
func TestMergeTargetMovementAndProtectedPaths(t *testing.T) {
	app, p := mergeFixture(t, "one\n\n\n\n\nfive\n", "two\n\n\n\n\nfive\n", "one\n\n\n\n\nsix\n")
	state, _ := app.GetProjectGit(p.LocalHandle)
	preview, err := app.PreviewProjectMerge(p.LocalHandle, state.Revision, "colleague")
	if err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, p.Path, "branch", "-f", "colleague", "HEAD")
	before := fixtureGit(t, p.Path, "rev-parse", "HEAD")
	if _, err = app.ApplyProjectMerge(p.LocalHandle, preview.ID, preview.Revision); err == nil {
		t.Fatal("moved merge target accepted")
	}
	if fixtureGit(t, p.Path, "rev-parse", "HEAD") != before {
		t.Fatal("rejected approval advanced HEAD")
	}
	fixtureGit(t, p.Path, "switch", "colleague")
	if err = os.WriteFile(filepath.Join(p.Path, ".env"), []byte("FIXTURE_ONLY=secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, p.Path, "add", ".env")
	fixtureGit(t, p.Path, "commit", "-m", "protected fixture")
	fixtureGit(t, p.Path, "switch", "main")
	state, _ = app.GetProjectGit(p.LocalHandle)
	if _, err = app.PreviewProjectMerge(p.LocalHandle, state.Revision, "colleague"); err == nil {
		t.Fatal("protected merge path accepted")
	}
	if _, err = os.Stat(filepath.Join(p.Path, ".env")); !os.IsNotExist(err) {
		t.Fatal("unapproved protected file reached original checkout")
	}
}
func TestProtocolFraming(t *testing.T) {
	for _, text := range []string{"Content-Length: 9999999\r\n\r\n", "Content-Length: -1\r\n\r\n", "Content-Length: 1\r\n\r\nx"} {
		if _, err := readProtocol(bufio.NewReader(strings.NewReader(text))); err == nil {
			t.Fatal("bad frame accepted")
		}
	}
	if data, err := readProtocol(bufio.NewReader(strings.NewReader("Content-Length: 2\r\n\r\n{}"))); err != nil || string(data) != "{}" {
		t.Fatal(err)
	}
}
func protocolResult(t *testing.T, app *App, p ScratchProject, s ProtocolState, method string, params any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := app.ProjectProtocolRequest(p.LocalHandle, s.ID, method, string(raw), false)
	if err != nil {
		t.Fatalf("%s %v", method, err)
	}
	var data map[string]any
	if err = json.Unmarshal(out, &data); err != nil {
		t.Fatal(err)
	}
	if data["error"] != nil || data["success"] == false {
		t.Fatalf("%s %s", method, out)
	}
	return data
}
func TestRealPythonLSPAndDAP(t *testing.T) {
	python := os.Getenv("PROOFCODE_IDE_TEST_PYTHON")
	if python == "" {
		t.Skip("set Python with pylsp and debugpy")
	}
	app := localTestApp(t)
	p, err := app.registerDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer app.shutdown(context.Background())
	source := "value = 41\nresult = value + 1\nprint(result)\n"
	path := filepath.Join(p.Path, "main.py")
	if err = os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	lsp, err := app.StartProjectProtocol(p.LocalHandle, "lsp", python, []string{"-m", "pylsp"})
	if err != nil {
		t.Fatal(err)
	}
	protocolResult(t, app, p, lsp, "initialize", map[string]any{"processId": nil, "rootUri": lsp.RootURI, "capabilities": map[string]any{}})
	if _, err = app.ProjectProtocolRequest(p.LocalHandle, lsp.ID, "initialized", "{}", true); err != nil {
		t.Fatal(err)
	}
	uri := fileURI(path)
	params, _ := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "python", "version": 1, "text": source}})
	if _, err = app.ProjectProtocolRequest(p.LocalHandle, lsp.ID, "textDocument/didOpen", string(params), true); err != nil {
		t.Fatal(err)
	}
	hover := protocolResult(t, app, p, lsp, "textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 10}})
	if hover["result"] == nil {
		t.Fatalf("no real hover %v", hover)
	}
	if _, err = app.GetProjectProtocol("other", lsp.ID, 0); err == nil {
		t.Fatal("cross-project LSP")
	}
	_ = app.StopProjectProtocol(p.LocalHandle, lsp.ID)
	dap, err := app.StartProjectProtocol(p.LocalHandle, "dap", python, []string{"-m", "debugpy.adapter"})
	if err != nil {
		t.Fatal(err)
	}
	protocolResult(t, app, p, dap, "initialize", map[string]any{"adapterID": "python", "linesStartAt1": true, "columnsStartAt1": true, "pathFormat": "path"})
	launchDone := make(chan error, 1)
	go func() {
		args, _ := json.Marshal(map[string]any{"program": path, "cwd": p.Path, "python": python, "console": "internalConsole", "stopOnEntry": false})
		_, e := app.ProjectProtocolRequest(p.LocalHandle, dap.ID, "launch", string(args), false)
		launchDone <- e
	}()
	awaitEvent := func(name string) map[string]any {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			state, e := app.GetProjectProtocol(p.LocalHandle, dap.ID, 0)
			if e != nil {
				t.Fatal(e)
			}
			for _, event := range state.Events {
				var msg map[string]any
				_ = json.Unmarshal(event.Message, &msg)
				if msg["event"] == name {
					return msg
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("missing real DAP event %s", name)
		return nil
	}
	awaitEvent("initialized")
	points := protocolResult(t, app, p, dap, "setBreakpoints", map[string]any{"source": map[string]any{"path": path}, "breakpoints": []any{map[string]any{"line": 2}}})
	if len(points["body"].(map[string]any)["breakpoints"].([]any)) != 1 {
		t.Fatal("breakpoint not registered")
	}
	protocolResult(t, app, p, dap, "configurationDone", map[string]any{})
	if err = <-launchDone; err != nil {
		t.Fatal(err)
	}
	stopped := awaitEvent("stopped")
	thread := stopped["body"].(map[string]any)["threadId"]
	stack := protocolResult(t, app, p, dap, "stackTrace", map[string]any{"threadId": thread})
	frame := stack["body"].(map[string]any)["stackFrames"].([]any)[0].(map[string]any)
	if frame["line"] != float64(2) {
		t.Fatalf("wrong breakpoint line %v", frame)
	}
	scopes := protocolResult(t, app, p, dap, "scopes", map[string]any{"frameId": frame["id"]})
	ref := scopes["body"].(map[string]any)["scopes"].([]any)[0].(map[string]any)["variablesReference"]
	variables := protocolResult(t, app, p, dap, "variables", map[string]any{"variablesReference": ref})
	data, _ := json.Marshal(variables)
	if !strings.Contains(string(data), "41") {
		t.Fatal("variables missing")
	}
	protocolResult(t, app, p, dap, "evaluate", map[string]any{"expression": "value + 1", "frameId": frame["id"], "context": "repl"})
	protocolResult(t, app, p, dap, "continue", map[string]any{"threadId": thread})
	awaitEvent("terminated")
	_ = app.StopProjectProtocol(p.LocalHandle, dap.ID)
}
