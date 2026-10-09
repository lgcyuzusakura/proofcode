package main

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func localTestApp(t *testing.T) *App {
	t.Helper()
	return &App{desktopRoot: filepath.Join(t.TempDir(), "redirected-desktop"), registryDir: filepath.Join(t.TempDir(), "config")}
}
func TestBootstrapSurvivesRestartAndKeepsOneDirectory(t *testing.T) {
	app := localTestApp(t)
	first, err := app.BootstrapScratchProject("bootstrap-test-1234", "订单工程")
	if err != nil {
		t.Fatal(err)
	}
	reopened := &App{desktopRoot: app.desktopRoot, registryDir: app.registryDir}
	again, err := reopened.BootstrapScratchProject("bootstrap-test-1234", "重试时名称改变")
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Fatalf("retry duplicated bootstrap: %+v %+v", first, again)
	}
	projects, err := reopened.ListLocalProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("registry: %v %+v", err, projects)
	}
	entries, err := os.ReadDir(filepath.Join(app.desktopRoot, "ProofCode-Projects"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("directories: %v %v", err, entries)
	}
	if _, err = app.BootstrapScratchProject("../../unsafe", "unsafe"); err == nil {
		t.Fatal("accepted unsafe bootstrap")
	}
}
func TestSnapshotIncludesDirtyAndUntrackedButExcludesCredentials(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git required")
	}
	app := localTestApp(t)
	directory := t.TempDir()
	project, err := app.registerDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "init", "--initial-branch=main", directory)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	for _, file := range []string{"main.go", "new.go", ".env", "auth.json"} {
		if err = os.WriteFile(filepath.Join(directory, file), []byte(file+" dirty"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	command = exec.Command("git", "add", "main.go")
	command.Dir = directory
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if err = os.WriteFile(filepath.Join(directory, "main.go"), []byte("modified after staging"), 0644); err != nil {
		t.Fatal(err)
	}
	archive, err := app.CaptureProjectSource(project.LocalHandle)
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.Files) != 2 {
		t.Fatalf("snapshot leaked or dropped files: %+v", archive.Files)
	}
	content, _ := base64.StdEncoding.DecodeString(archive.Files[0].Content)
	if string(content) != "modified after staging" {
		t.Fatalf("dirty state not captured: %s", content)
	}
	if _, err = app.CaptureProjectSource("desktop:not-registered"); err == nil {
		t.Fatal("accepted unregistered handle")
	}
}
func TestPatchApplicationPreservesGitIndexAndRejectsConcurrentChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git required")
	}
	app := localTestApp(t)
	directory := t.TempDir()
	project, err := app.registerDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, "hello.txt"), []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}
	archive, err := app.CaptureProjectSource(project.LocalHandle)
	if err != nil {
		t.Fatal(err)
	}
	patch := "diff --git a/hello.txt b/hello.txt\n--- a/hello.txt\n+++ b/hello.txt\n@@ -1 +1 @@\n-before\n+after\n"
	if err = os.WriteFile(filepath.Join(directory, "hello.txt"), []byte("concurrent\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = app.ApplyProjectPatch(project.LocalHandle, archive.ManifestHash, patch); err == nil {
		t.Fatal("overwrote concurrent edit")
	}
	data, _ := os.ReadFile(filepath.Join(directory, "hello.txt"))
	if string(data) != "concurrent\n" {
		t.Fatal("concurrent file modified")
	}
	if err = os.WriteFile(filepath.Join(directory, "hello.txt"), []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}
	applied, err := app.ApplyProjectPatch(project.LocalHandle, archive.ManifestHash, patch)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.FileCount != 1 {
		t.Fatalf("application: %+v", applied)
	}
	data, _ = os.ReadFile(filepath.Join(directory, "hello.txt"))
	if string(data) != "after\n" {
		t.Fatalf("file: %s", data)
	}
	if _, err = os.Stat(filepath.Join(directory, ".git")); !os.IsNotExist(err) {
		t.Fatal("private validation modified original Git state")
	}
	current, err := app.CaptureProjectSource(project.LocalHandle)
	if err != nil {
		t.Fatal(err)
	}
	secretPatch := "diff --git a/.env b/.env\nnew file mode 100644\n--- /dev/null\n+++ b/.env\n@@ -0,0 +1 @@\n+secret\n"
	if _, err = app.ApplyProjectPatch(project.LocalHandle, current.ManifestHash, secretPatch); err == nil {
		t.Fatal("allowed private file creation")
	}
}
func TestInterruptedPatchRollsBackRecognizedChanges(t *testing.T) {
	app := localTestApp(t)
	project, err := app.registerDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(project.Path, "hello.txt"), []byte("before\n"), 0644)
	before, err := captureDirectory(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(project.Path, "hello.txt"), []byte("after\n"), 0644)
	after, err := captureDirectory(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	registry, _ := app.projectRegistry()
	path := filepath.Join(registry, "apply", strings.TrimPrefix(project.LocalHandle, "desktop:")+".json")
	if err = atomicJSON(path, applicationJournal{Handle: project.LocalHandle, Before: before, After: after, Status: "APPLYING"}); err != nil {
		t.Fatal(err)
	}
	if _, err = app.CaptureProjectSource(project.LocalHandle); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(project.Path, "hello.txt"))
	if string(data) != "before\n" {
		t.Fatalf("partial patch wasn't restored: %s", data)
	}
}

func TestNonGitSnapshotHonorsNestedIgnoreRulesWithoutChangingFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		".gitignore":         "secrets.local.json\n*.tmp\n",
		"main.go":            "package main\n",
		"secrets.local.json": "must not upload",
		"nested/.gitignore":  "private.txt\n!keep.tmp\n",
		"nested/private.txt": "must not upload",
		"nested/keep.tmp":    "retained",
		"nested/drop.tmp":    "must not upload",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := captureDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, file := range archive.Files {
		paths[file.Path] = true
	}
	if paths["secrets.local.json"] || paths["nested/private.txt"] || paths["nested/drop.tmp"] || !paths["nested/keep.tmp"] || !paths["main.go"] {
		t.Fatalf("ignore rules were not preserved: %+v", paths)
	}
	if _, err = os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatal("snapshot created a repository in the user's folder")
	}
}
