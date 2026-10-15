package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
)

func evaluatorGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
func evaluatorRepo(t *testing.T) *workspace.Workspace {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	evaluatorGit(t, root, "init")
	evaluatorGit(t, root, "config", "user.name", "Evaluator Test")
	evaluatorGit(t, root, "config", "user.email", "evaluator@example.invalid")
	evaluatorGit(t, root, "config", "core.autocrlf", "false")
	for _, name := range []string{"one.txt", "two.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("old\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	evaluatorGit(t, root, "add", ".")
	evaluatorGit(t, root, "commit", "-m", "initial")
	ws, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}
func evaluatorDiff(path, old, next string) string {
	return "diff --git a/" + path + " b/" + path + "\n--- a/" + path + "\n+++ b/" + path + "\n@@ -1 +1 @@\n-" + old + "\n+" + next + "\n"
}
func evaluatorFence(patch string) string { return "```diff\n" + patch + "```" }

func TestEvaluatorAppliesCRLFFencedDiffAndStagesSameBytes(t *testing.T) {
	ws := evaluatorRepo(t)
	content := strings.ReplaceAll("Solution:\n"+evaluatorFence(evaluatorDiff("one.txt", "old", "new")), "\n", "\r\n")
	applied, err := applyGeneratedPatch(context.Background(), ws, content)
	if err != nil || !applied {
		t.Fatalf("CRLF fenced diff rejected: %v", err)
	}
	bytes, err := os.ReadFile(filepath.Join(ws.Root, "one.txt"))
	if err != nil || string(bytes) != "new\n" || evaluatorGit(t, ws.Root, "show", ":one.txt") != "new" {
		t.Fatalf("index/worktree disagree: %q %v", bytes, err)
	}
}

func TestEvaluatorRejectsAmbiguousAndUnsafePatchWithoutChanges(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"multiple blocks", evaluatorFence(evaluatorDiff("one.txt", "old", "new")) + "\n" + evaluatorFence(evaluatorDiff("two.txt", "old", "new"))},
		{"incomplete fence", "```diff\n" + evaluatorDiff("one.txt", "old", "new")},
		{"mode only", evaluatorFence("diff --git a/one.txt b/one.txt\nold mode 100644\nnew mode 100755\n")},
		{"symlink index", evaluatorFence("diff --git a/one.txt b/one.txt\nindex abc1234..def5678 120000\n--- a/one.txt\n+++ b/one.txt\n@@ -1 +1 @@\n-old\n+outside\n")},
		{"symlink creation", evaluatorFence("diff --git a/link b/link\nnew file mode 120000\n--- /dev/null\n+++ b/link\n@@ -0,0 +1 @@\n+outside\n")},
		{"traversal", evaluatorFence(evaluatorDiff("../outside.txt", "old", "new"))},
		{"metadata rename", evaluatorFence("diff --git a/one.txt b/.git/config\nsimilarity index 100%\nrename from one.txt\nrename to .git/config\n")},
		{"quoted metadata path", evaluatorFence("diff --git \"a/one.txt\" \"b/.proofcode/state\"\n--- \"a/one.txt\"\n+++ \"b/.proofcode/state\"\n@@ -1 +1 @@\n-old\n+new\n")},
		{"traversal rename", evaluatorFence("diff --git a/one.txt b/../outside.txt\nsimilarity index 100%\nrename from one.txt\nrename to ../outside.txt\n")},
		{"binary", evaluatorFence("diff --git a/one.txt b/one.txt\nGIT binary patch\nliteral 3\nabc\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := evaluatorRepo(t)
			applied, err := applyGeneratedPatch(context.Background(), ws, tc.content)
			if err == nil || applied {
				t.Fatalf("unsafe patch accepted: applied=%v err=%v", applied, err)
			}
			if status := evaluatorGit(t, ws.Root, "status", "--porcelain"); status != "" {
				t.Fatalf("rejected patch changed workspace/index: %s", status)
			}
		})
	}
}

func TestEvaluatorIndexConflictAndBadLaterHunkNeverPartiallyApply(t *testing.T) {
	for _, conflict := range []string{"index", "later hunk"} {
		t.Run(conflict, func(t *testing.T) {
			ws := evaluatorRepo(t)
			patch := evaluatorDiff("one.txt", "old", "new") + evaluatorDiff("two.txt", "old", "new")
			if conflict == "index" {
				if err := os.WriteFile(filepath.Join(ws.Root, "two.txt"), []byte("uncommitted\n"), 0644); err != nil {
					t.Fatal(err)
				}
			} else {
				patch = evaluatorDiff("one.txt", "old", "new") + evaluatorDiff("two.txt", "wrong base", "new")
			}
			before := evaluatorGit(t, ws.Root, "status", "--porcelain")
			applied, err := applyGeneratedPatch(context.Background(), ws, evaluatorFence(patch))
			if err == nil || applied {
				t.Fatalf("conflicting patch accepted: %v", err)
			}
			one, _ := os.ReadFile(filepath.Join(ws.Root, "one.txt"))
			if string(one) != "old\n" || evaluatorGit(t, ws.Root, "show", ":one.txt") != "old" || evaluatorGit(t, ws.Root, "status", "--porcelain") != before {
				t.Fatal("earlier hunk was partially applied before later failure")
			}
		})
	}
}

func TestEvaluatorSafeRenameAndNoPatch(t *testing.T) {
	ws := evaluatorRepo(t)
	applied, err := applyGeneratedPatch(context.Background(), ws, "ordinary explanatory answer")
	if err != nil || applied {
		t.Fatalf("no patch response: %v %v", applied, err)
	}
	patch := "diff --git a/one.txt b/renamed.txt\nsimilarity index 100%\nrename from one.txt\nrename to renamed.txt\n"
	applied, err = applyGeneratedPatch(context.Background(), ws, evaluatorFence(patch))
	if err != nil || !applied {
		t.Fatalf("safe rename rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, "one.txt")); !os.IsNotExist(err) {
		t.Fatal("rename source retained")
	}
	if data, err := os.ReadFile(filepath.Join(ws.Root, "renamed.txt")); err != nil || string(data) != "old\n" {
		t.Fatalf("rename destination invalid: %q %v", data, err)
	}
}

func TestEvaluatorCreatesAndDeletesRegularFiles(t *testing.T) {
	ws := evaluatorRepo(t)
	patch := "diff --git a/new.txt b/new.txt\nnew file mode 100644\n--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1 @@\n+created\n"
	patch += "diff --git a/two.txt b/two.txt\ndeleted file mode 100644\n--- a/two.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-old\n"
	applied, err := applyGeneratedPatch(context.Background(), ws, evaluatorFence(patch))
	if err != nil || !applied {
		t.Fatalf("regular create/delete rejected: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(ws.Root, "new.txt")); err != nil || string(data) != "created\n" {
		t.Fatalf("new file missing: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, "two.txt")); !os.IsNotExist(err) {
		t.Fatal("deleted file retained")
	}
}

func TestEvaluatorRejectsExistingSymlinkTarget(t *testing.T) {
	ws := evaluatorRepo(t)
	if err := os.Symlink("one.txt", filepath.Join(ws.Root, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	evaluatorGit(t, ws.Root, "add", "link.txt")
	evaluatorGit(t, ws.Root, "commit", "-m", "link")
	patch := "diff --git a/link.txt b/link.txt\n--- a/link.txt\n+++ b/link.txt\n@@ -1 +1 @@\n-one.txt\n+two.txt\n"
	if applied, err := applyGeneratedPatch(context.Background(), ws, evaluatorFence(patch)); applied || err == nil {
		t.Fatalf("symlink target patch accepted: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(ws.Root, "link.txt")); err != nil || target != "one.txt" {
		t.Fatalf("symlink changed: %q %v", target, err)
	}
}

func TestSplitTestCommandQuotesAndRejectsShell(t *testing.T) {
	program, args, err := splitTestCommand(`go test "./path with space" '-run=Test Add' ""`)
	if err != nil || program != "go" || !reflect.DeepEqual(args, []string{"test", "./path with space", "-run=Test Add", ""}) {
		t.Fatalf("quotes changed: %q %#v %v", program, args, err)
	}
	for _, value := range []string{"", "   ", `"" test`, "go test; echo passed", "go test | cat", "go test && echo passed", "go test > out", "go test\nother", "go test $SECRET", "go test `id`", "go test 'unclosed", strings.Repeat("a", 4097)} {
		if _, _, err := splitTestCommand(value); err == nil {
			t.Fatalf("unsafe command parsed: %q", value)
		}
	}
}

func TestEvaluatorUsesActualExitCodeInsteadOfSuccessText(t *testing.T) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		goPath, err = filepath.Abs(filepath.Join("..", "..", "..", ".tools", "go", "bin", "go.exe"))
		if err != nil {
			t.Fatal(err)
		}
		if _, statErr := os.Stat(goPath); statErr != nil {
			t.Skip("go toolchain unavailable")
		}
		t.Setenv("PATH", filepath.Dir(goPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	ws := evaluatorRepo(t)
	for _, tc := range []struct {
		name, source string
		wantError    bool
	}{
		{"misleading success", "package main\nimport (\"fmt\";\"os\")\nfunc main(){fmt.Println(\"PASS all tests passed\");os.Exit(7)}\n", true},
		{"actual success", "package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"FAILED text in sample fixture\")}\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(ws.Root, "probe.go"), []byte(tc.source), 0644); err != nil {
				t.Fatal(err)
			}
			result, err := evaluateTests(context.Background(), ws, "go run probe.go", tool.DefaultCommandPolicy())
			if (err != nil) != tc.wantError || result.Metadata == nil {
				t.Fatalf("exit outcome inferred from text: %+v err=%v", result, err)
			}
		})
	}
	if _, err := evaluateTests(context.Background(), ws, "cmd /c echo PASS", tool.DefaultCommandPolicy()); err == nil {
		t.Fatal("shell evaluator was allowed")
	}
}

func TestCheckoutRevisionIsFixedAndRejectsMutableRefs(t *testing.T) {
	ws := evaluatorRepo(t)
	first := evaluatorGit(t, ws.Root, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(ws.Root, "one.txt"), []byte("second\n"), 0644); err != nil {
		t.Fatal(err)
	}
	evaluatorGit(t, ws.Root, "add", "one.txt")
	evaluatorGit(t, ws.Root, "commit", "-m", "second")
	second := evaluatorGit(t, ws.Root, "rev-parse", "HEAD")
	if err := checkoutRevision(context.Background(), ws.Root, first); err != nil {
		t.Fatal(err)
	}
	if evaluatorGit(t, ws.Root, "rev-parse", "HEAD") != first || evaluatorGit(t, ws.Root, "rev-parse", "--abbrev-ref", "HEAD") != "HEAD" {
		t.Fatal("evaluator did not pin detached immutable commit")
	}
	for _, value := range []string{"HEAD", "main", first[:12], "HEAD~1", "--help"} {
		if err := checkoutRevision(context.Background(), ws.Root, value); err == nil {
			t.Fatalf("mutable ref accepted: %s", value)
		}
	}
	if err := checkoutRevision(context.Background(), ws.Root, second); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Root, "one.txt"), []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkoutRevision(context.Background(), ws.Root, first); err == nil {
		t.Fatal("checkout silently discarded dirty work")
	}
	if evaluatorGit(t, ws.Root, "rev-parse", "HEAD") != second {
		t.Fatal("failed checkout moved HEAD")
	}
}
