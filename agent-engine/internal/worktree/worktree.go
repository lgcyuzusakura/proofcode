package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var safeID = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type Manager struct {
	Repository string
	Root       string
}
type Handle struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Base   string `json:"base"`
}

func (m Manager) Create(ctx context.Context, taskID string) (Handle, error) {
	repository, err := filepath.Abs(m.Repository)
	if err != nil {
		return Handle{}, err
	}
	root, err := filepath.Abs(m.Root)
	if err != nil {
		return Handle{}, err
	}
	if repository == root {
		return Handle{}, errors.New("worktree root must differ from repository")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return Handle{}, err
	}
	id := strings.Trim(safeID.ReplaceAllString(taskID, "-"), "-")
	if id == "" {
		return Handle{}, errors.New("task ID has no safe characters")
	}
	if len(id) > 64 {
		id = id[:64]
	}
	branch := "proofcode/" + id
	path := filepath.Join(root, id)
	base, err := gitOutput(ctx, repository, "rev-parse", "HEAD")
	if err != nil {
		return Handle{}, err
	}
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branch, path, "HEAD")
	cmd.Dir = repository
	if output, err := cmd.CombinedOutput(); err != nil {
		return Handle{}, fmt.Errorf("create worktree: %w: %s", err, output)
	}
	return Handle{Path: path, Branch: branch, Base: strings.TrimSpace(base)}, nil
}

func (m Manager) Checkpoint(ctx context.Context, handle Handle, message string) (string, error) {
	if _, err := gitOutput(ctx, handle.Path, "add", "-A"); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "git", "-c", "user.name=ProofCode", "-c", "user.email=proofcode@local", "commit", "--allow-empty", "-m", message)
	cmd.Dir = handle.Path
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("checkpoint commit: %w: %s", err, output)
	}
	hash, err := gitOutput(ctx, handle.Path, "rev-parse", "HEAD")
	return strings.TrimSpace(hash), err
}

// Patch returns a portable patch from the base revision to the checkpoint.
// It is captured before the ephemeral worktree is removed so the control plane
// can expose the exact change set for review or export.
func (m Manager) Patch(ctx context.Context, handle Handle, commit string) (string, error) {
	base := strings.TrimSpace(handle.Base)
	if base == "" || strings.TrimSpace(commit) == "" {
		return "", errors.New("base and commit are required")
	}
	return gitOutput(ctx, handle.Path, "diff", "--binary", base, strings.TrimSpace(commit))
}

// WorkingPatch captures the current changes for recovery without committing.
func (m Manager) WorkingPatch(ctx context.Context, handle Handle) (string, error) {
	if strings.TrimSpace(handle.Base) == "" {
		return "", errors.New("base revision is required")
	}
	if _, err := gitOutput(ctx, handle.Path, "add", "-A"); err != nil {
		return "", err
	}
	return gitOutput(ctx, handle.Path, "diff", "--cached", "--binary", "--no-ext-diff", handle.Base)
}

func (m Manager) Remove(ctx context.Context, handle Handle) error {
	repository, _ := filepath.Abs(m.Repository)
	candidate, _ := filepath.Abs(handle.Path)
	root, _ := filepath.Abs(m.Root)
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("refusing to remove worktree outside configured root")
	}
	cmd := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", candidate)
	cmd.Dir = repository
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("remove worktree: %w: %s", err, output)
	}
	return nil
}
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, output)
	}
	return string(output), nil
}
