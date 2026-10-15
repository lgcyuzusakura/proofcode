package artifact

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchAndApplyRequiresExactCleanBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is unavailable")
	}
	root := t.TempDir()
	runGit(t, root, "init")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "main.go")
	runGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "base")
	base := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	patch := "diff --git a/main.go b/main.go\nindex 9a3c2c2..2cbf3b4 100644\n--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,3 @@\n package main\n \n-func main() {}\n+func main() { println(\"ok\") }\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks/task/artifacts/checkpoint" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "checkpoint", "patch": patch, "metadata": map[string]string{"base": base}})
	}))
	defer server.Close()
	artifact, err := Fetch(context.Background(), server.Client(), server.URL, "token", "task", "checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), root, artifact); err != nil {
		t.Fatal(err)
	}
	changed, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil || !strings.Contains(string(changed), `println("ok")`) {
		t.Fatalf("patch was not applied: %s %v", changed, err)
	}
	if err := Apply(context.Background(), root, artifact); err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("dirty checkout was accepted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	artifact.Metadata.Base = strings.Repeat("0", len(base))
	if err := Apply(context.Background(), root, artifact); err == nil || !strings.Contains(err.Error(), "base revision mismatch") {
		t.Fatalf("wrong base was accepted: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
