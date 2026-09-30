package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
	run("add", ".")
	run("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "base")
	manager := Manager{Repository: repository, Root: filepath.Join(t.TempDir(), "trees")}
	handle, err := manager.Create(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(handle.Path, "README.md"), []byte("changed"), 0644)
	hash, err := manager.Checkpoint(context.Background(), handle, "change")
	if err != nil || hash == "" {
		t.Fatalf("checkpoint failed: %v", err)
	}
	if err := manager.Remove(context.Background(), handle); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(handle.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
}
