package tool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
)

const maxToolOutput = 256 << 10

type ReadFile struct{ Workspace *workspace.Workspace }

func (t ReadFile) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name: "read_file", Description: "Read a UTF-8 text file from the repository with optional line boundaries.",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"path": map[string]any{"type": "string"}, "start_line": map[string]any{"type": "integer", "minimum": 1}, "end_line": map[string]any{"type": "integer", "minimum": 1},
		}, "required": []string{"path"}, "additionalProperties": false},
	}
}
func (ReadFile) Risk(json.RawMessage) Risk { return RiskRead }
func (t ReadFile) Execute(_ context.Context, raw json.RawMessage) Result {
	args, err := Decode[struct {
		Path  string `json:"path"`
		Start int    `json:"start_line"`
		End   int    `json:"end_line"`
	}](raw)
	if err != nil {
		return failed(err)
	}
	path, err := t.Workspace.Resolve(args.Path)
	if err != nil {
		return failed(err)
	}
	file, err := os.Open(path)
	if err != nil {
		return failed(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(file, maxToolOutput+1))
	scanner.Buffer(make([]byte, 4096), maxToolOutput)
	var out strings.Builder
	line := 0
	for scanner.Scan() {
		line++
		if args.Start > 0 && line < args.Start {
			continue
		}
		if args.End > 0 && line > args.End {
			break
		}
		fmt.Fprintf(&out, "%d\t%s\n", line, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return failed(err)
	}
	return Result{Content: out.String(), Metadata: map[string]any{"path": args.Path, "lines": line}}
}

type ListFiles struct{ Workspace *workspace.Workspace }

func (t ListFiles) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name: "list_files", Description: "List repository files below a directory. Generated dependency directories are skipped.",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 5000}}, "additionalProperties": false},
	}
}
func (ListFiles) Risk(json.RawMessage) Risk { return RiskRead }
func (t ListFiles) Execute(_ context.Context, raw json.RawMessage) Result {
	args, err := Decode[struct {
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}](raw)
	if err != nil {
		return failed(err)
	}
	if args.Limit == 0 {
		args.Limit = 500
	}
	root, err := t.Workspace.Resolve(args.Path)
	if err != nil {
		return failed(err)
	}
	ignored := map[string]bool{".git": true, "node_modules": true, "target": true, "dist": true, "build": true, ".idea": true, ".gradle": true, "vendor": true}
	values := make([]string, 0, args.Limit)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() && path != root && ignored[entry.Name()] {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(t.Workspace.Root, path)
		values = append(values, filepath.ToSlash(rel))
		if len(values) >= args.Limit {
			return errLimitReached
		}
		return nil
	})
	if err != nil && !errors.Is(err, errLimitReached) {
		return failed(err)
	}
	sort.Strings(values)
	return Result{Content: strings.Join(values, "\n"), Metadata: map[string]any{"count": len(values), "truncated": errors.Is(err, errLimitReached)}}
}

var errLimitReached = errors.New("limit reached")

type SearchCode struct{ Workspace *workspace.Workspace }

func (t SearchCode) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name: "search_code", Description: "Search repository text using ripgrep. Returns paths, line numbers and matching lines.",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "glob": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 500}}, "required": []string{"query"}, "additionalProperties": false},
	}
}
func (SearchCode) Risk(json.RawMessage) Risk { return RiskRead }
func (t SearchCode) Execute(ctx context.Context, raw json.RawMessage) Result {
	args, err := Decode[struct {
		Query string `json:"query"`
		Glob  string `json:"glob"`
		Limit int    `json:"limit"`
	}](raw)
	if err != nil {
		return failed(err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return failed(errors.New("query is required"))
	}
	if args.Limit == 0 {
		args.Limit = 100
	}
	commandArgs := []string{"--line-number", "--column", "--no-heading", "--color=never", "--max-count", fmt.Sprint(args.Limit), "--", args.Query, "."}
	if args.Glob != "" {
		commandArgs = []string{"--line-number", "--column", "--no-heading", "--color=never", "--max-count", fmt.Sprint(args.Limit), "--glob", args.Glob, "--", args.Query, "."}
	}
	cmd := exec.CommandContext(ctx, "rg", commandArgs...)
	cmd.Dir = t.Workspace.Root
	output, runErr := limitedCombinedOutput(cmd, maxToolOutput)
	if runErr != nil {
		if exit, ok := runErr.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return Result{Content: "No matches."}
		}
		return failed(runErr)
	}
	return Result{Content: string(output)}
}

type ApplyPatch struct{ Workspace *workspace.Workspace }
type PatchEdit struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
	Create  bool   `json:"create"`
	Delete  bool   `json:"delete"`
}

func (t ApplyPatch) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name: "apply_patch", Description: "Apply exact, reviewable text edits. old_text must occur exactly once. New files require create=true.",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"edits": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "old_text": map[string]any{"type": "string"}, "new_text": map[string]any{"type": "string"}, "create": map[string]any{"type": "boolean"}, "delete": map[string]any{"type": "boolean"}}, "required": []string{"path"}, "additionalProperties": false}}}, "required": []string{"edits"}, "additionalProperties": false},
	}
}
func (ApplyPatch) Risk(json.RawMessage) Risk { return RiskWrite }
func (t ApplyPatch) Execute(_ context.Context, raw json.RawMessage) Result {
	args, err := Decode[struct {
		Edits []PatchEdit `json:"edits"`
	}](raw)
	if err != nil {
		return failed(err)
	}
	if len(args.Edits) == 0 {
		return failed(errors.New("at least one edit is required"))
	}
	type pending struct {
		path   string
		data   []byte
		delete bool
	}
	changes := make([]pending, 0, len(args.Edits))
	for _, edit := range args.Edits {
		path, err := t.Workspace.Resolve(edit.Path)
		if err != nil {
			return failed(fmt.Errorf("%s: %w", edit.Path, err))
		}
		current, readErr := os.ReadFile(path)
		if edit.Create {
			if readErr == nil {
				return failed(fmt.Errorf("%s already exists", edit.Path))
			}
			if !os.IsNotExist(readErr) {
				return failed(readErr)
			}
			changes = append(changes, pending{path: path, data: []byte(edit.NewText)})
			continue
		}
		if readErr != nil {
			return failed(readErr)
		}
		if !utf8.Valid(current) {
			return failed(fmt.Errorf("%s is not UTF-8 text", edit.Path))
		}
		if edit.Delete {
			changes = append(changes, pending{path: path, delete: true})
			continue
		}
		count := bytes.Count(current, []byte(edit.OldText))
		if count != 1 {
			return failed(fmt.Errorf("%s old_text matched %d times; expected exactly once", edit.Path, count))
		}
		updated := bytes.Replace(current, []byte(edit.OldText), []byte(edit.NewText), 1)
		changes = append(changes, pending{path: path, data: updated})
	}
	for _, change := range changes {
		if change.delete {
			if err := os.Remove(change.path); err != nil {
				return failed(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(change.path), 0755); err != nil {
			return failed(err)
		}
		tmp := change.path + ".proofcode.tmp"
		if err := os.WriteFile(tmp, change.data, 0644); err != nil {
			return failed(err)
		}
		if err := os.Rename(tmp, change.path); err != nil {
			return failed(err)
		}
	}
	return Result{Content: fmt.Sprintf("Applied %d edit(s).", len(changes)), Metadata: map[string]any{"files": len(changes)}}
}

type RunCommand struct {
	Workspace   *workspace.Workspace
	MaxDuration time.Duration
}

func (t RunCommand) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name: "run_command", Description: "Run a program without a shell in the repository. Pipes and shell expansion are intentionally unavailable.",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"program": map[string]any{"type": "string"}, "args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 600}}, "required": []string{"program"}, "additionalProperties": false},
	}
}
func (RunCommand) Risk(json.RawMessage) Risk { return RiskExec }
func (t RunCommand) Execute(ctx context.Context, raw json.RawMessage) Result {
	args, err := Decode[struct {
		Program string   `json:"program"`
		Args    []string `json:"args"`
		Timeout int      `json:"timeout_seconds"`
	}](raw)
	if err != nil {
		return failed(err)
	}
	if strings.TrimSpace(args.Program) == "" || strings.ContainsAny(args.Program, "\r\n\x00") {
		return failed(errors.New("invalid program"))
	}
	duration := t.MaxDuration
	if duration <= 0 {
		duration = 2 * time.Minute
	}
	if args.Timeout > 0 && time.Duration(args.Timeout)*time.Second < duration {
		duration = time.Duration(args.Timeout) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	cmd := exec.CommandContext(runCtx, args.Program, args.Args...)
	cmd.Dir = t.Workspace.Root
	cmd.Env = safeEnvironment(os.Environ())
	output, runErr := limitedCombinedOutput(cmd, maxToolOutput)
	metadata := map[string]any{"program": args.Program, "args": args.Args, "exitCode": 0, "timedOut": errors.Is(runCtx.Err(), context.DeadlineExceeded)}
	if runErr != nil {
		metadata["exitCode"] = -1
		if exit, ok := runErr.(*exec.ExitError); ok {
			metadata["exitCode"] = exit.ExitCode()
		}
		return Result{Content: string(output) + "\n" + runErr.Error(), Metadata: metadata, IsError: true}
	}
	return Result{Content: string(output), Metadata: metadata}
}

type GitDiff struct{ Workspace *workspace.Workspace }

func (t GitDiff) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "git_diff", Description: "Show the current Git working tree diff.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"staged": map[string]any{"type": "boolean"}}, "additionalProperties": false}}
}
func (GitDiff) Risk(json.RawMessage) Risk { return RiskRead }
func (t GitDiff) Execute(ctx context.Context, raw json.RawMessage) Result {
	args, err := Decode[struct {
		Staged bool `json:"staged"`
	}](raw)
	if err != nil {
		return failed(err)
	}
	values := []string{"diff", "--no-ext-diff", "--no-color"}
	if args.Staged {
		values = append(values, "--cached")
	}
	cmd := exec.CommandContext(ctx, "git", values...)
	cmd.Dir = t.Workspace.Root
	output, err := limitedCombinedOutput(cmd, maxToolOutput)
	if err != nil {
		return failed(fmt.Errorf("git diff: %w: %s", err, output))
	}
	return Result{Content: string(output)}
}

func safeEnvironment(values []string) []string {
	blocked := []string{"API_KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL"}
	out := make([]string, 0, len(values))
	for _, value := range values {
		key, _, _ := strings.Cut(value, "=")
		upper := strings.ToUpper(key)
		deny := false
		for _, term := range blocked {
			if strings.Contains(upper, term) {
				deny = true
				break
			}
		}
		if !deny {
			out = append(out, value)
		}
	}
	return out
}
func limitedCombinedOutput(cmd *exec.Cmd, limit int) ([]byte, error) {
	var buffer limitedBuffer
	buffer.limit = limit
	cmd.Stdout = &buffer
	cmd.Stderr = &buffer
	err := cmd.Run()
	return buffer.Bytes(), err
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return original, nil
}
func failed(err error) Result { return Result{Content: err.Error(), IsError: true} }
