package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Workspace struct{ Root string }

func Open(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("workspace is not a directory")
	}
	real, err := filepath.EvalSymlinks(abs)
	if err == nil {
		abs = real
	}
	return &Workspace{Root: filepath.Clean(abs)}, nil
}

func (w *Workspace) Resolve(relative string) (string, error) {
	if relative == "" {
		return w.Root, nil
	}
	if filepath.IsAbs(relative) {
		return "", errors.New("absolute paths are not allowed")
	}
	candidate := filepath.Clean(filepath.Join(w.Root, relative))
	rel, err := filepath.Rel(w.Root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes workspace")
	}
	parent := candidate
	if _, err := os.Stat(candidate); err != nil {
		parent = filepath.Dir(candidate)
	}
	if real, err := filepath.EvalSymlinks(parent); err == nil {
		realRel, relErr := filepath.Rel(w.Root, real)
		if relErr != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
			return "", errors.New("symlink escapes workspace")
		}
	}
	return candidate, nil
}
