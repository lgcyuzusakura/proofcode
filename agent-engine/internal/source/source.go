// Package source validates portable, immutable local working-directory snapshots.
package source

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type File struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Content    string `json:"content"`
	Executable bool   `json:"executable"`
}
type Archive struct {
	ManifestHash string `json:"manifestHash"`
	Files        []File `json:"files"`
}

var reserved = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\..*)?$`)

func SafePath(path string) bool {
	if path == "" || len(path) > 500 || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\\x00\r\n\t:*?\"<>|") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || reserved.MatchString(part) {
			return false
		}
		switch strings.ToLower(part) {
		case ".git", ".proofcode", ".context-store", "node_modules":
			return false
		}
	}
	name := strings.ToLower(filepath.Base(path))
	if name == ".env" || strings.HasPrefix(name, ".env.") && name != ".env.example" {
		return false
	}
	switch name {
	case "auth.json", "credentials.json", "credentials.local.json", "secrets.json":
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pem", ".key", ".p12", ".pfx", ".jks":
		return false
	}
	return true
}
func Hash(files []File) string {
	var text strings.Builder
	ordered := append([]File(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	for _, file := range ordered {
		fmt.Fprintf(&text, "%s\x00%s\x00", file.Path, file.SHA256)
		if file.Executable {
			text.WriteByte('1')
		} else {
			text.WriteByte('0')
		}
		text.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(text.String()))
	return hex.EncodeToString(sum[:])
}
func (a Archive) Validate() error {
	if a.Files == nil || len(a.Files) > 2000 {
		return errors.New("source snapshot must contain at most 2000 files")
	}
	total := 0
	seen := map[string]bool{}
	for _, file := range a.Files {
		fold := strings.ToLower(file.Path)
		if !SafePath(file.Path) || seen[fold] {
			return errors.New("unsafe or duplicate source path")
		}
		seen[fold] = true
		if len(file.Content) > 1400000 {
			return errors.New("source file exceeds 1 MiB")
		}
		data, err := base64.StdEncoding.DecodeString(file.Content)
		if err != nil {
			return err
		}
		total += len(data)
		if len(data) > 1<<20 || total > 8<<20 {
			return errors.New("source exceeds byte limit")
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.SHA256 {
			return errors.New("source file checksum mismatch")
		}
	}
	if Hash(a.Files) != a.ManifestHash {
		return errors.New("source manifest checksum mismatch")
	}
	return nil
}
func (a Archive) Materialize(destination string) error {
	if err := a.Validate(); err != nil {
		return err
	}
	entries, err := os.ReadDir(destination)
	if !os.IsNotExist(err) && (err != nil || len(entries) > 0) {
		return errors.New("source destination must be empty")
	}
	if err = os.MkdirAll(destination, 0750); err != nil {
		return err
	}
	for _, file := range a.Files {
		data, _ := base64.StdEncoding.DecodeString(file.Content)
		path := filepath.Join(destination, filepath.FromSlash(file.Path))
		if err = os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if file.Executable {
			mode = 0755
		}
		if err = os.WriteFile(path, data, mode); err != nil {
			return err
		}
	}
	return nil
}

// Capture includes tracked modifications and permitted untracked files, while
// preserving the user's .gitignore. It never copies Git internals or credentials.
func Capture(ctx context.Context, root string) (Archive, error) {
	command := exec.CommandContext(ctx, "git", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", ".")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return Archive{}, fmt.Errorf("list source: %w", err)
	}
	archive := Archive{Files: []File{}}
	total := 0
	seen := map[string]bool{}
	paths := strings.Split(string(output), "\x00")
	sort.Strings(paths)
	for _, relative := range paths {
		if !SafePath(relative) {
			continue
		}
		if seen[relative] {
			continue
		}
		seen[relative] = true
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Archive{}, err
		}
		if !info.Mode().IsRegular() {
			return Archive{}, errors.New("source symlinks and submodules are unsupported")
		}
		if info.Size() > 1<<20 {
			return Archive{}, errors.New("source file exceeds 1 MiB")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return Archive{}, err
		}
		total += len(data)
		if total > 8<<20 || len(archive.Files) >= 2000 {
			return Archive{}, errors.New("source snapshot exceeds limit")
		}
		hash := sha256.Sum256(data)
		archive.Files = append(archive.Files, File{relative, hex.EncodeToString(hash[:]), base64.StdEncoding.EncodeToString(data), info.Mode()&0111 != 0})
	}
	archive.ManifestHash = Hash(archive.Files)
	return archive, archive.Validate()
}
