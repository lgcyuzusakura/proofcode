package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewCapturesCumulativeChangesWithoutMutatingIndex(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return string(out)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("init")
	write("a.txt", "base\n")
	write("remove.txt", "remove\n")
	write(".env", "BASE_SECRET=hidden\n")
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "base")
	base := strings.TrimSpace(git("rev-parse", "HEAD"))
	write("a.txt", "staged\n")
	git("add", "a.txt")
	before := git("diff", "--cached", "--binary")
	write("a.txt", "working\n")
	write("new space [file].txt", "new\n")
	write(".env", "NEW_SECRET=hidden\n")
	write("credentials.json", "private\n")
	if err := os.Remove(filepath.Join(repo, "remove.txt")); err != nil {
		t.Fatal(err)
	}
	manager, handle := Manager{}, Handle{Path: repo, Base: base}
	patch, err := manager.PreviewPatch(context.Background(), handle, 128<<10)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"+working", "new space [file].txt", "deleted file mode"} {
		if !strings.Contains(patch, expected) {
			t.Fatalf("missing %q: %s", expected, patch)
		}
	}
	if strings.Contains(patch, "SECRET") || strings.Contains(patch, "credentials") {
		t.Fatalf("private file leaked: %s", patch)
	}
	if after := git("diff", "--cached", "--binary"); after != before {
		t.Fatal("preview changed real index")
	}
	if _, err := manager.PreviewPatch(context.Background(), handle, 20); !errors.Is(err, ErrPreviewTooLarge) {
		t.Fatalf("expected byte limit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.PreviewPatch(ctx, handle, 128<<10); err == nil {
		t.Fatal("cancelled capture succeeded")
	}
}
