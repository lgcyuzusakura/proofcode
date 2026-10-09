package source

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func fixture(path, content string) File {
	data := []byte(content)
	sum := sha256.Sum256(data)
	return File{Path: path, SHA256: hex.EncodeToString(sum[:]), Content: base64.StdEncoding.EncodeToString(data)}
}
func TestSourceValidatesAndMaterializesCurrentBytes(t *testing.T) {
	files := []File{fixture("src/main.go", "dirty current source"), fixture("README.md", "new untracked")}
	archive := Archive{Files: files, ManifestHash: Hash(files)}
	dir := filepath.Join(t.TempDir(), "repository")
	if err := archive.Materialize(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "src", "main.go"))
	if err != nil || string(data) != "dirty current source" {
		t.Fatalf("source: %s %v", data, err)
	}
	if err = archive.Materialize(dir); err == nil {
		t.Fatal("reused occupied destination")
	}
}
func TestSourceRejectsTraversalTamperingAndCaseCollisions(t *testing.T) {
	for _, path := range []string{"../outside", ".git/config", "C:/outside", ".env", "src/../outside", "src/CON", "auth.json", "src/secret.key"} {
		archive := Archive{Files: []File{fixture(path, "bytes")}}
		archive.ManifestHash = Hash(archive.Files)
		if err := archive.Validate(); err == nil {
			t.Fatalf("accepted path %s", path)
		}
	}
	file := fixture("main.go", "current")
	archive := Archive{Files: []File{file, fixture("MAIN.GO", "another")}}
	archive.ManifestHash = Hash(archive.Files)
	if err := archive.Validate(); err == nil {
		t.Fatal("accepted colliding paths")
	}
	archive.Files = []File{file}
	archive.ManifestHash = Hash(archive.Files)
	archive.Files[0].Content = base64.StdEncoding.EncodeToString([]byte("tampered"))
	if err := archive.Validate(); err == nil {
		t.Fatal("accepted tampered bytes")
	}
}
