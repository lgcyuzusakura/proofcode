package tool

import (
	"context"
	"encoding/json"
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

func TestSafeEnvironmentRedactsSecrets(t *testing.T) {
	got := safeEnvironment([]string{"PATH=/bin", "MODEL_API_KEY=x", "NORMAL=yes"})
	joined := strings.Join(got, "|")
	if strings.Contains(joined, "MODEL_API_KEY") || !strings.Contains(joined, "NORMAL=yes") {
		t.Fatalf("unexpected environment: %v", got)
	}
}
