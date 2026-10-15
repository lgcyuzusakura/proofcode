package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Workspace is the filesystem boundary used by every tool invocation. The
// write lock is intentionally owned here so tools cannot race a patch against
// a verifier or another tool in the same worktree.
type Workspace struct {
	Root string
	mu   sync.RWMutex
}

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

// RLock and RUnlock let read-only tools coordinate with a writer without
// exposing the underlying mutex implementation to callers.
func (w *Workspace) RLock()   { w.mu.RLock() }
func (w *Workspace) RUnlock() { w.mu.RUnlock() }
func (w *Workspace) Lock()    { w.mu.Lock() }
func (w *Workspace) Unlock()  { w.mu.Unlock() }

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
