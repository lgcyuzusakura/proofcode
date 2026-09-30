package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Resolve(filepath.Join("..", "secret")); err == nil {
		t.Fatal("expected escape rejection")
	}
	if _, err := w.Resolve("safe/file.go"); err != nil {
		t.Fatalf("safe path rejected: %v", err)
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skip(err)
	}
	w, _ := Open(dir)
	if _, err := w.Resolve("link/file"); err == nil {
		t.Fatal("expected symlink escape rejection")
	}
}
