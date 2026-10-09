package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateCheckpointRemove(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	repository := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repository, 0755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repository
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
	}
	run("init")
	_ = os.WriteFile(filepath.Join(repository, "README.md"), []byte("base"), 0644)
	_ = os.MkdirAll(filepath.Join(repository, "__pycache__"), 0755)
	_ = os.WriteFile(filepath.Join(repository, "__pycache__", "tracked.txt"), []byte("tracked base"), 0644)
	run("add", ".")
	run("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "base")
	manager := Manager{Repository: repository, Root: filepath.Join(t.TempDir(), "trees")}
	handle, err := manager.Create(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(handle.Path, "README.md"), []byte("changed"), 0644)
	_ = os.WriteFile(filepath.Join(handle.Path, "__pycache__", "tracked.txt"), []byte("tracked changed"), 0644)
	_ = os.WriteFile(filepath.Join(handle.Path, "__pycache__", "new.pyc"), []byte("generated"), 0644)
	_ = os.MkdirAll(filepath.Join(handle.Path, ".proofcode", "artifacts"), 0755)
	_ = os.WriteFile(filepath.Join(handle.Path, ".proofcode", "artifacts", "browser.png"), []byte("generated screenshot"), 0644)
	recovery, err := manager.WorkingPatch(context.Background(), handle)
	if err != nil || strings.Contains(recovery, "new.pyc") || strings.Contains(recovery, "browser.png") || !strings.Contains(recovery, "tracked changed") {
		t.Fatalf("recovery excluded source or included generated files: %v %s", err, recovery)
	}
	hash, err := manager.Checkpoint(context.Background(), handle, "change")
	if err != nil || hash == "" {
		t.Fatalf("checkpoint failed: %v", err)
	}
	patch, err := manager.Patch(context.Background(), handle, hash)
	if err != nil || strings.Contains(patch, "new.pyc") || strings.Contains(patch, "browser.png") || !strings.Contains(patch, "tracked changed") {
		t.Fatalf("checkpoint scope: %v %s", err, patch)
	}
	if err := manager.Remove(context.Background(), handle); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(handle.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
}
