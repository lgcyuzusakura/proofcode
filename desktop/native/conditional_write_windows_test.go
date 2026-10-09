//go:build windows

package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestConditionalWriteRejectsOpenEditorAndChangedBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}
	archive, err := captureDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	expected := archive.Files[0]
	next := expected
	next.Content = base64.StdEncoding.EncodeToString([]byte("after\n"))
	editor, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = conditionalSourceWrite(root, "source.txt", expected, true, next, true); err == nil {
		t.Fatal("overwrote a file held by another editor")
	}
	if _, err = editor.WriteAt([]byte("edited\n"), 0); err != nil {
		t.Fatal(err)
	}
	editor.Close()
	if err = conditionalSourceWrite(root, "source.txt", expected, true, next, true); err == nil {
		t.Fatal("overwrote edited bytes after the handle was released")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "edited\n" {
		t.Fatalf("editor bytes changed: %q", data)
	}
	if err = conditionalSourceWrite(root, "source.txt", sourceFile{}, false, next, true); err == nil {
		t.Fatal("CREATE_NEW overwrote an existing file")
	}
}

func TestConditionalDeleteVerifiesBytesUnderExclusiveHandle(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}
	archive, err := captureDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = conditionalSourceWrite(root, "source.txt", archive.Files[0], true, sourceFile{}, false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("conditional deletion failed: %v", err)
	}
}
