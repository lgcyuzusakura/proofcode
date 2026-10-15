package context

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeSource(t *testing.T, root, path, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceVersionIsolationAndExactCRLFAnchors(t *testing.T) {
	root := t.TempDir()
	v1 := "package service\r\nfunc Timeout() int { return 30 }\r\n"
	writeSource(t, root, "config.go", v1)
	r := WorkspaceRetriever{Root: root, ProjectID: "p1", WorkspaceID: "w1"}
	a, err := r.Retrieve(context.Background(), "Timeout", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Evidence) != 1 || a.CacheHit || a.EmbeddingConfigured {
		t.Fatalf("unexpected initial result: %+v", a)
	}
	e := a.Evidence[0]
	if e.Content != v1 || v1[e.StartByte:e.EndByte] != e.Content || e.BlobHash != digest(v1) {
		t.Fatalf("bytes not original: %+v", e)
	}
	b, err := r.Retrieve(context.Background(), "Timeout", "", 4)
	if err != nil || !b.CacheHit {
		t.Fatalf("expected exact query cache: %v %+v", err, b)
	}
	// Returned slices may be edited without mutating cached results.
	b.Evidence[0].Reasons[0] = "poison"
	b.Evidence[0].Content = "poison"
	c, err := r.Retrieve(context.Background(), "Timeout", "", 4)
	if err != nil || c.Evidence[0].Content == "poison" || c.Evidence[0].Reasons[0] == "poison" {
		t.Fatalf("caller poisoned cache: %v %+v", err, c)
	}
	v2 := strings.Repeat("// new line\r\n", 81) + strings.Replace(v1, "30", "60", 1)
	writeSource(t, root, "config.go", v2)
	d, err := r.Retrieve(context.Background(), "Timeout", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if d.SnapshotID == a.SnapshotID || d.CacheHit {
		t.Fatalf("changed file reused old snapshot: %+v", d)
	}
	for _, e := range d.Evidence {
		if e.SnapshotID != d.SnapshotID || e.BlobHash != digest(v2) || e.Content != v2[e.StartByte:e.EndByte] || strings.Contains(e.Content, "return 30") {
			t.Fatalf("stale version evidence: %+v", e)
		}
	}
	r.ProjectID = "p2"
	r.WorkspaceID = "w2"
	n, err := r.Retrieve(context.Background(), "Timeout", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if n.SnapshotID == d.SnapshotID || n.CacheHit {
		t.Fatal("project/workspace change failed to isolate")
	}
	for _, e := range n.Evidence {
		if e.ProjectID != "p2" || e.WorkspaceID != "w2" {
			t.Fatalf("wrong scope: %+v", e)
		}
	}
}

func TestWorkspaceFeedbackRelationshipsAndIncrementalCache(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "math.go", "package p\nfunc Add(a,b int) int { return a+b }\n")
	writeSource(t, root, "consumer.go", "package p\nfunc Invoice() int { return Add(1,2) }\n")
	writeSource(t, root, "math_test.go", "package p\nfunc TestAdd() { Add(1,2) }\n")
	writeSource(t, root, "other.go", "package p\nfunc Other() {}\n")
	r := WorkspaceRetriever{Root: root, ProjectID: "p", WorkspaceID: "w"}
	a, err := r.Retrieve(context.Background(), "Add", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if a.FilesParsed != 4 {
		t.Fatalf("initial parse count %d", a.FilesParsed)
	}
	routes := map[string]bool{}
	for _, e := range a.Evidence {
		for _, route := range e.Reasons {
			routes[route] = true
		}
	}
	for _, name := range []string{"lexical", "symbol", "dependency", "test"} {
		if !routes[name] {
			t.Fatalf("missing %s route in %+v", name, a.Evidence)
		}
	}
	b, err := r.Retrieve(context.Background(), "find unrelated regression", "FAIL TestAdd: Add returned 3", 10)
	if err != nil {
		t.Fatal(err)
	}
	if b.CacheHit || b.FilesReused != 4 || b.FilesParsed != 0 || len(b.Evidence) == 0 {
		t.Fatalf("feedback/parse cache unexpected: %+v", b)
	}
	writeSource(t, root, "math.go", "package p\nfunc Add(a,b int) int { return a+b+1 }\n")
	c, err := r.Retrieve(context.Background(), "Add", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if c.FilesReused != 3 || c.FilesParsed != 1 || c.SnapshotID == b.SnapshotID {
		t.Fatalf("incremental caching incorrect: %+v", c)
	}
	if err := os.Remove(filepath.Join(root, "consumer.go")); err != nil {
		t.Fatal(err)
	}
	d, err := r.Retrieve(context.Background(), "Invoice", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range d.Evidence {
		if e.Path == "consumer.go" {
			t.Fatal("deleted evidence persisted")
		}
	}
}

func TestWorkspaceSkipsSecretsBinaryAndSymlinks(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "main.go", "package p\nfunc PublicSentinel() {}\n")
	for _, path := range []string{".env", "secrets/a.go", "credentials/b.go", ".aws/a.go", ".git/a.go", "node_modules/a.go", "auth.json", "client_secret.json"} {
		writeSource(t, root, path, "func HiddenSentinel() {}")
	}
	writeSource(t, root, "binary.go", "HiddenSentinel\x00binary")
	writeSource(t, root, "credentials_inline.go", "package p\nvar password = \"RealCredential123456\"\n")
	writeSource(t, root, "config.go", "package p\nvar api_key = \"sensitiveLiveToken123456\"\n")
	writeSource(t, root, "private.go", "-----BEGIN PRIVATE KEY-----\nSecretSentinel")
	outside := t.TempDir()
	writeSource(t, outside, "outside.go", "func OutsideSentinel() {}")
	if err := os.Symlink(filepath.Join(outside, "outside.go"), filepath.Join(root, "link.go")); err != nil {
		t.Logf("symlink unavailable: %v", err)
	}
	r := WorkspaceRetriever{Root: root}
	result, err := r.Retrieve(context.Background(), "PublicSentinel HiddenSentinel OutsideSentinel sensitiveLiveToken123456", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if result.FilesScanned != 1 {
		t.Fatalf("indexed sensitive/binary/link input: %+v", result)
	}
	for _, e := range result.Evidence {
		if e.Path != "main.go" {
			t.Fatalf("unsafe source indexed: %+v", e)
		}
	}
}

func TestWorkspaceBoundsCancellationAndBudget(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "one.go", "package p\nfunc Needle(){}\n")
	writeSource(t, root, "two.go", "package p\nfunc NeedleTwo(){}\n")
	for _, r := range []*WorkspaceRetriever{{Root: root, MaxFiles: 1}, {Root: root, MaxBytes: 10}, {Root: root, MaxFileBytes: 10}} {
		if _, err := r.Retrieve(context.Background(), "Needle", "", 4); err == nil {
			t.Fatal("bounds did not fail closed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := WorkspaceRetriever{Root: root}
	if _, err := r.Retrieve(ctx, "Needle", "", 4); err != context.Canceled {
		t.Fatalf("expected canceled, got %v", err)
	}
	r.ByteBudget = 10
	result, err := r.Retrieve(context.Background(), "Needle", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "" || len(result.Evidence) != 0 || result.OmittedByBudget == 0 {
		t.Fatalf("byte budget not honored: %+v", result)
	}
}

func TestRelationshipsUseReferencesEvenInLexicalSeeds(t *testing.T) {
	definition := Chunk{Path: "math.go", Content: "package p\nfunc Add(a,b int) int { return a+b }\n"}
	caller := Chunk{Path: "consumer.go", Content: "package p\nfunc Invoice() int { return Add(1,2) }\n"}
	unrelated := Chunk{Path: "other.go", Content: "package p\nfunc Other() {}\n"}
	dependencies, _ := rankRelationships([]Chunk{definition, caller, unrelated}, []Hit{{Chunk: definition}, {Chunk: caller}}, "Add")
	if len(dependencies) != 1 || dependencies[0].Chunk.Path != "consumer.go" {
		t.Fatalf("declarations confused with references, or lexical caller omitted: %+v", dependencies)
	}
}

func TestScopedHybridRejectsObsoleteAndForeignEvidence(t *testing.T) {
	good := Hit{Chunk: Chunk{ID: "same", SnapshotID: "current", ProjectID: "p", WorkspaceID: "w", Path: "a.go", Content: "current"}}
	old := good
	old.Chunk.SnapshotID = "old"
	old.Chunk.Content = "old"
	foreign := good
	foreign.Chunk.ProjectID = "foreign"
	h := Hybrid{SnapshotID: "current", ProjectID: "p", WorkspaceID: "w", Retrievers: []Retriever{rankedRoute{"lexical", []Hit{old, foreign, good}}, rankedRoute{"symbol", []Hit{good}}}}
	hits, err := h.Search(context.Background(), "query")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Chunk.Content != "current" {
		t.Fatalf("scope leakage: %+v", hits)
	}
}

func TestGitCommitAndTaskAttemptIsolation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
	git("init")
	git("config", "user.email", "test@example.invalid")
	git("config", "user.name", "Context Test")
	writeSource(t, root, "a.go", "package p\nfunc Needle() {}\n")
	git("add", "a.go")
	git("commit", "-m", "initial")
	r := WorkspaceRetriever{Root: root, TaskID: "task1", AttemptID: "1"}
	a, err := r.Retrieve(context.Background(), "Needle", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	git("commit", "--allow-empty", "-m", "new commit")
	b, err := r.Retrieve(context.Background(), "Needle", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if a.BaseCommit == "" || a.BaseCommit == b.BaseCommit || a.SnapshotID == b.SnapshotID {
		t.Fatalf("Git head isolation missing: %+v %+v", a, b)
	}
	r.AttemptID = "2"
	c, err := r.Retrieve(context.Background(), "Needle", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if c.SnapshotID == b.SnapshotID || c.CacheHit {
		t.Fatal("attempt reused previous snapshot")
	}
}
