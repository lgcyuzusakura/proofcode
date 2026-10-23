package durablefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenameReplacesCompleteContentsAndKeepsTargetOnFailure(t *testing.T) {
	dir := t.TempDir()
	source, target := filepath.Join(dir, "next"), filepath.Join(dir, "saved")
	if err := os.WriteFile(source, []byte("new state"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old state"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Rename(source, target); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new state" {
		t.Fatalf("incomplete replacement: %q %v", data, err)
	}
	if err := Rename(source, target); err == nil {
		t.Fatal("missing source accepted")
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new state" {
		t.Fatal("failed replacement damaged saved state")
	}
}
