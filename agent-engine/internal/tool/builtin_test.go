package tool

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
)

func TestApplyPatchRequiresUniqueMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("same\nsame\n"), 0644); err != nil {
		t.Fatal(err)
	}
	w, _ := workspace.Open(dir)
	tool := ApplyPatch{Workspace: w}
	raw, _ := json.Marshal(map[string]any{"edits": []map[string]any{{"path": "a.txt", "old_text": "same", "new_text": "new"}}})
	result := tool.Execute(context.Background(), raw)
	if !result.IsError || !strings.Contains(result.Content, "matched 2") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestApplyPatchIsAtomicBeforeWrites(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("old"), 0644)
	w, _ := workspace.Open(dir)
	tool := ApplyPatch{Workspace: w}
	raw, _ := json.Marshal(map[string]any{"edits": []map[string]any{{"path": "a.txt", "old_text": "old", "new_text": "new"}, {"path": "missing.txt", "old_text": "x", "new_text": "y"}}})
	result := tool.Execute(context.Background(), raw)
	if !result.IsError {
		t.Fatal("expected error")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(data) != "old" {
		t.Fatalf("first file changed despite validation failure: %s", data)
	}
}

func TestApplyPatchRejectsProtectedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TOKEN=secret"), 0600); err != nil {
		t.Fatal(err)
	}
	w, _ := workspace.Open(dir)
	tool := ApplyPatch{Workspace: w}
	raw, _ := json.Marshal(map[string]any{"edits": []map[string]any{{"path": ".env", "old_text": "TOKEN=secret", "new_text": "TOKEN=changed"}}})
	result := tool.Execute(context.Background(), raw)
	if !result.IsError || !strings.Contains(result.Content, "secret") {
		t.Fatalf("expected protected path rejection: %+v", result)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if string(data) != "TOKEN=secret" {
		t.Fatalf("protected file changed: %s", data)
	}
}

func TestApplyPatchAllowsCredentialHandlingSourceAndEnvTemplate(t *testing.T) {
	dir := t.TempDir()
	w, _ := workspace.Open(dir)
	patch := ApplyPatch{Workspace: w}
	raw, _ := json.Marshal(map[string]any{"edits": []map[string]any{
		{"path": ".env.example", "new_text": "MODEL_API_KEY=\n", "create": true},
		{"path": "credentials_test.go", "new_text": "package credentials\n", "create": true},
	}})
	if result := patch.Execute(context.Background(), raw); result.IsError {
		t.Fatalf("legitimate source files should be writable: %+v", result)
	}
}

func TestApplyPatchRejectsStaleHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	w, _ := workspace.Open(dir)
	tool := ApplyPatch{Workspace: w}
	wrong := fmt.Sprintf("%x", sha256.Sum256([]byte("different")))
	raw, _ := json.Marshal(map[string]any{"edits": []map[string]any{{"path": "a.txt", "old_text": "old", "new_text": "new", "expected_sha256": wrong}}})
	result := tool.Execute(context.Background(), raw)
	if !result.IsError || !strings.Contains(result.Content, "changed since it was read") {
		t.Fatalf("expected stale hash rejection: %+v", result)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "old" {
		t.Fatalf("stale patch changed file: %s", data)
	}
}

func TestRunCommandPolicyRejectsShellAndInjection(t *testing.T) {
	dir := t.TempDir()
	w, _ := workspace.Open(dir)
	tool := RunCommand{Workspace: w, Policy: DefaultCommandPolicy()}
	for name, args := range map[string][]string{
		"shell":     []string{"-c", "echo unsafe"},
		"injection": []string{"run", "&&", "whoami"},
		"gitEscape": []string{"-C", "..", "status"},
	} {
		program := map[string]string{"shell": "sh", "injection": "git", "gitEscape": "git"}[name]
		raw, _ := json.Marshal(map[string]any{"program": program, "args": args})
		result := tool.Execute(context.Background(), raw)
		if !result.IsError {
			t.Fatalf("%s command unexpectedly allowed: %+v", name, result)
		}
	}
}

func TestSafeEnvironmentRedactsSecrets(t *testing.T) {
	got := safeEnvironment([]string{"PATH=/bin", "MODEL_API_KEY=x", "NORMAL=yes"})
	joined := strings.Join(got, "|")
	if strings.Contains(joined, "MODEL_API_KEY") || !strings.Contains(joined, "NORMAL=yes") {
		t.Fatalf("unexpected environment: %v", got)
	}
}

func TestReadFileCanReachLaterLinesInLargeFile(t *testing.T) {
	dir := t.TempDir()
	var content strings.Builder
	content.WriteString(strings.Repeat("x", maxToolOutput+1))
	content.WriteByte('\n')
	for line := 2; line <= 30000; line++ {
		fmt.Fprintf(&content, "line %d\n", line)
	}
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), []byte(content.String()), 0644); err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"path": "large.txt", "start_line": 29999, "end_line": 30000})
	result := (ReadFile{Workspace: w}).Execute(context.Background(), raw)
	if result.IsError || result.Content != "29999\tline 29999\n30000\tline 30000\n" {
		t.Fatalf("later lines were not read: %+v", result)
	}
	if result.Metadata["truncated"] != false {
		t.Fatalf("unexpected truncation: %+v", result.Metadata)
	}
}

func TestReadFileReportsNextLineWhenOutputIsBounded(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat(strings.Repeat("a", 100)+"\n", 4000)
	if err := os.WriteFile(filepath.Join(dir, "many.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	w, _ := workspace.Open(dir)
	result := (ReadFile{Workspace: w}).Execute(context.Background(), json.RawMessage(`{"path":"many.txt"}`))
	if result.IsError || len(result.Content) > maxToolOutput || result.Metadata["truncated"] != true {
		t.Fatalf("expected bounded output: %+v", result)
	}
	nextLine, ok := result.Metadata["nextLine"].(int)
	if !ok || nextLine < 2 || !strings.HasSuffix(result.Content, fmt.Sprintf("%d\t%s\n", nextLine-1, strings.Repeat("a", 100))) {
		t.Fatalf("incorrect next line: %+v", result.Metadata)
	}
	continuation, _ := json.Marshal(map[string]any{"path": "many.txt", "start_line": nextLine, "end_line": nextLine})
	continued := (ReadFile{Workspace: w}).Execute(context.Background(), continuation)
	if continued.IsError || continued.Content != fmt.Sprintf("%d\t%s\n", nextLine, strings.Repeat("a", 100)) {
		t.Fatalf("continuation skipped or repeated a line: %+v", continued)
	}
}

func TestReadFileRejectsInvalidRangeAndOversizedSelectedLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "line.txt"), []byte(strings.Repeat("x", maxToolOutput+1)), 0644); err != nil {
		t.Fatal(err)
	}
	w, _ := workspace.Open(dir)
	read := ReadFile{Workspace: w}
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"path":"line.txt","start_line":3,"end_line":2}`),
		json.RawMessage(`{"path":"line.txt","start_line":-1}`),
		json.RawMessage(`{"path":"line.txt"}`),
	} {
		if result := read.Execute(context.Background(), raw); !result.IsError {
			t.Fatalf("expected an error for %s: %+v", raw, result)
		}
	}
}

func TestListFilesSupportsPagination(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	w, _ := workspace.Open(dir)
	list := ListFiles{Workspace: w}
	result := list.Execute(context.Background(), json.RawMessage(`{"limit":2,"offset":2}`))
	if result.IsError || result.Content != "c.txt\nd.txt" || result.Metadata["truncated"] != true || result.Metadata["nextOffset"] != 4 {
		t.Fatalf("unexpected page: %+v", result)
	}
	last := list.Execute(context.Background(), json.RawMessage(`{"limit":2,"offset":4}`))
	if last.IsError || last.Content != "e.txt" || last.Metadata["truncated"] != false {
		t.Fatalf("unexpected final page: %+v", last)
	}
	if bad := list.Execute(context.Background(), json.RawMessage(`{"limit":-1}`)); !bad.IsError {
		t.Fatalf("negative limit unexpectedly allowed: %+v", bad)
	}
}
