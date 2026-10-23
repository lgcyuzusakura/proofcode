package worktree

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/source"
)

var ErrPreviewTooLarge = errors.New("workspace preview exceeds limit")

type previewOutput struct {
	data     []byte
	limit    int
	overflow bool
}

func (b *previewOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - len(b.data)
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}

// PreviewPatch uses a disposable index. It includes allowed untracked files and
// deletions without staging files in the task's real index or creating commits.
// Credential paths stay at their baseline in the disposable index.
func (m Manager) PreviewPatch(ctx context.Context, handle Handle, limit int) (string, error) {
	if strings.TrimSpace(handle.Base) == "" || limit < 1 {
		return "", errors.New("preview base and byte limit are required")
	}
	file, err := os.CreateTemp("", "proofcode-preview-index-*")
	if err != nil {
		return "", errors.New("cannot create preview index")
	}
	index := file.Name()
	_ = file.Close()
	_ = os.Remove(index) // read-tree must create a valid index, not open an empty one.
	defer os.Remove(index)
	defer os.Remove(index + ".lock")
	run := func(input []byte, max int, args ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, "git", args...)
		command.Dir = handle.Path
		command.Env = append(os.Environ(), "GIT_INDEX_FILE="+index, "GIT_LITERAL_PATHSPECS=1")
		command.Stdin = bytes.NewReader(input)
		output := &previewOutput{limit: max}
		command.Stdout = output
		if err := command.Run(); err != nil {
			return nil, errors.New("cannot capture workspace preview")
		}
		if output.overflow {
			return nil, ErrPreviewTooLarge
		}
		return output.data, nil
	}
	if _, err = run(nil, 1024, "read-tree", handle.Base); err != nil {
		return "", err
	}
	baselineCommand := exec.CommandContext(ctx, "git", "ls-tree", "-r", "--name-only", "-z", handle.Base)
	baselineCommand.Dir = handle.Path
	baselineOutput := &previewOutput{limit: 1 << 20}
	baselineCommand.Stdout = baselineOutput
	if err := baselineCommand.Run(); err != nil || baselineOutput.overflow {
		return "", errors.New("cannot list preview baseline")
	}
	baseline := baselineOutput.data
	current, err := run(nil, 1<<20, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	paths := map[string]bool{}
	for _, path := range strings.Split(string(append(baseline, current...)), "\x00") {
		if !source.SafePath(path) {
			continue
		}
		// Check every component without following links. A linked parent could
		// otherwise smuggle an external regular file into git add.
		parts := strings.Split(path, "/")
		for index := range parts {
			candidate := filepath.Join(handle.Path, filepath.Join(parts[:index+1]...))
			info, statErr := os.Lstat(candidate)
			if os.IsNotExist(statErr) {
				break
			}
			if statErr != nil {
				return "", errors.New("cannot inspect preview source")
			}
			if index < len(parts)-1 {
				if !info.IsDir() {
					return "", errors.New("preview requires regular source directories")
				}
			} else if !info.Mode().IsRegular() {
				return "", errors.New("preview requires regular source files")
			}
		}
		paths[path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	if len(ordered) > 0 {
		if _, err = run([]byte(strings.Join(ordered, "\x00")+"\x00"), 1024, "add", "-A", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			return "", err
		}
	}
	output, err := run(nil, limit, "diff", "--cached", "--binary", "--no-ext-diff", "--no-textconv", handle.Base, "--")
	if err != nil {
		return "", err
	}
	if !utf8.Valid(output) {
		return "", errors.New("preview is not UTF-8")
	}
	return string(output), nil
}
