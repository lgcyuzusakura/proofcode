package tool

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
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
const maxPatchBytes = 2 << 20

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
func (t ReadFile) Execute(ctx context.Context, raw json.RawMessage) Result {
	if t.Workspace == nil {
		return failed(errors.New("workspace is required"))
	}
	t.Workspace.RLock()
	defer t.Workspace.RUnlock()
	args, err := Decode[struct {
		Path  string `json:"path"`
		Start int    `json:"start_line"`
		End   int    `json:"end_line"`
	}](raw)
	if err != nil {
		return failed(err)
	}
	if args.Start < 0 || args.End < 0 || (args.End > 0 && args.Start > args.End) {
		return failed(errors.New("invalid line range"))
	}
	if args.Start == 0 {
		args.Start = 1
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
	reader := bufio.NewReaderSize(file, 32<<10)
	var out strings.Builder
	line := 0
	truncated := false
	nextLine := 0
	for {
		if err := ctx.Err(); err != nil {
			return failed(err)
		}
		line++
		selected := line >= args.Start
		var content strings.Builder
		for {
			fragment, more, readErr := reader.ReadLine()
			if errors.Is(readErr, io.EOF) {
				line--
				return Result{Content: out.String(), Metadata: readFileMetadata(args.Path, line, truncated, nextLine)}
			}
			if readErr != nil {
				return failed(readErr)
			}
			if selected {
				if content.Len()+len(fragment) > maxToolOutput {
					if out.Len() == 0 {
						return failed(fmt.Errorf("line %d exceeds the %d byte read limit", line, maxToolOutput))
					}
					truncated, nextLine = true, line
					return Result{Content: out.String(), Metadata: readFileMetadata(args.Path, line, truncated, nextLine)}
				}
				_, _ = content.Write(fragment)
			}
			if !more {
				break
			}
			if err := ctx.Err(); err != nil {
				return failed(err)
			}
		}
		if !selected {
			continue
		}
		prefix := fmt.Sprintf("%d\t", line)
		if out.Len()+len(prefix)+content.Len()+1 > maxToolOutput {
			if out.Len() == 0 {
				return failed(fmt.Errorf("line %d exceeds the %d byte read limit", line, maxToolOutput))
			}
			truncated, nextLine = true, line
			break
		}
		out.WriteString(prefix)
		out.WriteString(content.String())
		out.WriteByte('\n')
		if args.End > 0 && line >= args.End {
			break
		}
	}
	return Result{Content: out.String(), Metadata: readFileMetadata(args.Path, line, truncated, nextLine)}
}

func readFileMetadata(path string, scannedLines int, truncated bool, nextLine int) map[string]any {
	metadata := map[string]any{"path": path, "lines": scannedLines, "truncated": truncated}
	if truncated {
		metadata["nextLine"] = nextLine
	}
	return metadata
}

type ListFiles struct{ Workspace *workspace.Workspace }

func (t ListFiles) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name: "list_files", Description: "List repository files below a directory. Generated dependency directories are skipped.",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 5000}, "offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 1000000}}, "additionalProperties": false},
	}
}
func (ListFiles) Risk(json.RawMessage) Risk { return RiskRead }
func (t ListFiles) Execute(ctx context.Context, raw json.RawMessage) Result {
	if t.Workspace == nil {
		return failed(errors.New("workspace is required"))
	}
	t.Workspace.RLock()
	defer t.Workspace.RUnlock()
	args, err := Decode[struct {
		Path   string `json:"path"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}](raw)
	if err != nil {
		return failed(err)
	}
	if args.Limit == 0 {
		args.Limit = 500
	}
	if args.Limit < 1 || args.Limit > 5000 || args.Offset < 0 || args.Offset > 1000000 {
		return failed(errors.New("invalid file list limit or offset"))
	}
	root, err := t.Workspace.Resolve(args.Path)
	if err != nil {
		return failed(err)
	}
	ignored := map[string]bool{".git": true, "node_modules": true, "target": true, "dist": true, "build": true, ".idea": true, ".gradle": true, "vendor": true}
	values := make([]string, 0, args.Limit)
	seen := 0
	outputBytes := 0
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != root && ignored[entry.Name()] {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		if seen < args.Offset {
			seen++
			return nil
		}
		if len(values) >= args.Limit {
			return errLimitReached
		}
		rel, _ := filepath.Rel(t.Workspace.Root, path)
		rel = filepath.ToSlash(rel)
		separator := 0
		if len(values) > 0 {
			separator = 1
		}
		if outputBytes+separator+len(rel) > maxToolOutput {
			return errLimitReached
		}
		values = append(values, rel)
		outputBytes += separator + len(rel)
		return nil
	})
	if err != nil && !errors.Is(err, errLimitReached) {
		return failed(err)
	}
	sort.Strings(values)
	metadata := map[string]any{"count": len(values), "truncated": errors.Is(err, errLimitReached)}
	if errors.Is(err, errLimitReached) {
		metadata["nextOffset"] = args.Offset + len(values)
	}
	return Result{Content: strings.Join(values, "\n"), Metadata: metadata}
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
	if t.Workspace == nil {
		return failed(errors.New("workspace is required"))
	}
	t.Workspace.RLock()
	defer t.Workspace.RUnlock()
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
	Path           string `json:"path"`
	OldText        string `json:"old_text"`
	NewText        string `json:"new_text"`
	ExpectedSHA256 string `json:"expected_sha256"`
	Create         bool   `json:"create"`
	Delete         bool   `json:"delete"`
}

type patchPending struct {
	path      string
	relative  string
	data      []byte
	delete    bool
	created   bool
	applied   bool
	mode      os.FileMode
	oldData   []byte
	oldMode   os.FileMode
	oldExists bool
}

func (t ApplyPatch) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name: "apply_patch", Description: "Apply exact, reviewable text edits. old_text must occur exactly once. Existing files can include expected_sha256 for optimistic concurrency. New files require create=true.",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"edits": map[string]any{"type": "array", "minItems": 1, "maxItems": 50, "items": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "old_text": map[string]any{"type": "string"}, "new_text": map[string]any{"type": "string"}, "expected_sha256": map[string]any{"type": "string", "pattern": "^[a-fA-F0-9]{64}$"}, "create": map[string]any{"type": "boolean"}, "delete": map[string]any{"type": "boolean"}}, "required": []string{"path"}, "additionalProperties": false}}}, "required": []string{"edits"}, "additionalProperties": false},
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
	if len(args.Edits) > 50 {
		return failed(errors.New("at most 50 edits are allowed per patch"))
	}
	if t.Workspace == nil {
		return failed(errors.New("workspace is required"))
	}
	// Validation and writes share one lock. This prevents a verifier or a
	// second model call from changing a file between the hash check and rename.
	t.Workspace.Lock()
	defer t.Workspace.Unlock()
	changes := make([]patchPending, 0, len(args.Edits))
	seen := make(map[string]struct{}, len(args.Edits))
	var totalBytes int
	for _, edit := range args.Edits {
		if edit.Create && edit.Delete {
			return failed(fmt.Errorf("%s cannot set both create and delete", edit.Path))
		}
		if err := validatePatchPath(edit.Path); err != nil {
			return failed(fmt.Errorf("%s: %w", edit.Path, err))
		}
		path, err := t.Workspace.Resolve(edit.Path)
		if err != nil {
			return failed(fmt.Errorf("%s: %w", edit.Path, err))
		}
		key := filepath.Clean(path)
		if _, exists := seen[key]; exists {
			return failed(fmt.Errorf("%s appears more than once in the same patch", edit.Path))
		}
		seen[key] = struct{}{}
		current, readErr := os.ReadFile(path)
		if edit.Create {
			if readErr == nil {
				return failed(fmt.Errorf("%s already exists", edit.Path))
			}
			if !os.IsNotExist(readErr) {
				return failed(readErr)
			}
			if len(edit.NewText) > maxPatchBytes {
				return failed(fmt.Errorf("%s exceeds the %d byte patch limit", edit.Path, maxPatchBytes))
			}
			totalBytes += len(edit.NewText)
			changes = append(changes, patchPending{path: path, relative: edit.Path, data: []byte(edit.NewText), created: true, mode: 0644})
			continue
		}
		if readErr != nil {
			return failed(readErr)
		}
		if len(current) > maxPatchBytes {
			return failed(fmt.Errorf("%s exceeds the %d byte patch limit", edit.Path, maxPatchBytes))
		}
		if edit.ExpectedSHA256 != "" {
			got := fmt.Sprintf("%x", sha256.Sum256(current))
			if !strings.EqualFold(got, edit.ExpectedSHA256) {
				return failed(fmt.Errorf("%s changed since it was read (expected sha256 %s, got %s)", edit.Path, edit.ExpectedSHA256, got))
			}
		}
		if !utf8.Valid(current) {
			return failed(fmt.Errorf("%s is not UTF-8 text", edit.Path))
		}
		if edit.Delete {
			changes = append(changes, patchPending{path: path, relative: edit.Path, delete: true, oldData: append([]byte(nil), current...), oldMode: fileMode(path), oldExists: true})
			continue
		}
		if edit.OldText == "" {
			return failed(fmt.Errorf("%s old_text is required for an existing file edit", edit.Path))
		}
		count := bytes.Count(current, []byte(edit.OldText))
		if count != 1 {
			return failed(fmt.Errorf("%s old_text matched %d times; expected exactly once", edit.Path, count))
		}
		updated := bytes.Replace(current, []byte(edit.OldText), []byte(edit.NewText), 1)
		if len(updated) > maxPatchBytes {
			return failed(fmt.Errorf("%s exceeds the %d byte patch limit after edit", edit.Path, maxPatchBytes))
		}
		totalBytes += len(updated)
		changes = append(changes, patchPending{path: path, relative: edit.Path, data: updated, oldData: append([]byte(nil), current...), oldMode: fileMode(path), oldExists: true, mode: fileMode(path)})
	}
	if totalBytes > maxPatchBytes*4 {
		return failed(fmt.Errorf("patch exceeds the %d byte aggregate limit", maxPatchBytes*4))
	}
	// All edits have been validated. Keep enough state to restore every file if
	// a later rename/delete fails; a multi-file patch must not leave a half edit.
	for index := range changes {
		change := &changes[index]
		if change.delete {
			if err := os.Remove(change.path); err != nil {
				rollbackPatch(changes)
				return failed(fmt.Errorf("delete %s: %w", change.relative, err))
			}
			change.applied = true
			continue
		}
		if err := os.MkdirAll(filepath.Dir(change.path), 0755); err != nil {
			rollbackPatch(changes)
			return failed(fmt.Errorf("create parent for %s: %w", change.relative, err))
		}
		tmp := change.path + ".proofcode.tmp"
		if err := os.WriteFile(tmp, change.data, 0644); err != nil {
			rollbackPatch(changes)
			return failed(fmt.Errorf("write %s: %w", change.relative, err))
		}
		if change.mode != 0 {
			_ = os.Chmod(tmp, change.mode.Perm())
		}
		if err := os.Rename(tmp, change.path); err != nil {
			_ = os.Remove(tmp)
			rollbackPatch(changes)
			return failed(fmt.Errorf("commit %s: %w", change.relative, err))
		}
		change.applied = true
	}
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, filepath.ToSlash(change.relative))
	}
	return Result{Content: fmt.Sprintf("Applied %d edit(s).", len(changes)), Metadata: map[string]any{"files": len(changes), "paths": paths, "bytes": totalBytes}}
}

func fileMode(path string) os.FileMode {
	info, err := os.Stat(path)
	if err != nil {
		return 0644
	}
	return info.Mode()
}

// validatePatchPath protects repository metadata and credential material even
// when a caller has write approval. Projects can still commit templates such
// as .env.example; actual secret files remain outside the model write surface.
func validatePatchPath(relative string) error {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(relative)))
	if clean == "." || clean == "" {
		return errors.New("a file path is required")
	}
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if part == ".git" || part == ".proofcode" {
			return errors.New("repository metadata paths are protected")
		}
	}
	base := strings.ToLower(filepath.Base(clean))
	if (strings.HasPrefix(base, ".env.") && base != ".env.example") || base == ".env" {
		return errors.New("environment secret files are protected")
	}
	for _, suffix := range []string{".pem", ".key", ".p12", ".pfx", ".jks"} {
		if strings.HasSuffix(base, suffix) {
			return errors.New("credential and certificate files are protected")
		}
	}
	for _, protected := range []string{"credentials.json", "credentials.local.json", "secrets.json", "id_rsa", "id_ed25519"} {
		if base == protected {
			return errors.New("credential and secret files are protected")
		}
	}
	return nil
}

func rollbackPatch(changes []patchPending) {
	for index := len(changes) - 1; index >= 0; index-- {
		change := changes[index]
		if !change.applied {
			continue
		}
		if !change.oldExists {
			_ = os.Remove(change.path)
			continue
		}
		tmp := change.path + ".proofcode.rollback.tmp"
		if err := os.WriteFile(tmp, change.oldData, change.oldMode.Perm()); err == nil {
			_ = os.Rename(tmp, change.path)
		} else {
			_ = os.Remove(tmp)
		}
	}
}

type RunCommand struct {
	Workspace   *workspace.Workspace
	MaxDuration time.Duration
	Policy      CommandPolicy
}

// CommandPolicy is a deliberately small, deterministic execution policy. It
// is evaluated before a process is started and is safe to expose in runner
// configuration or an approval service without passing shell text around.
type CommandPolicy struct {
	AllowedPrograms map[string]bool
	MaxDuration     time.Duration
	MaxArgs         int
	MaxArgBytes     int
	MaxOutputBytes  int
}

func DefaultCommandPolicy() CommandPolicy {
	return CommandPolicy{
		AllowedPrograms: map[string]bool{
			"cargo": true, "deno": true, "dotnet": true, "go": true,
			"gradle": true, "java": true, "javac": true, "make": true,
			"mvn": true, "node": true, "npm": true, "npx": true,
			"php": true, "pnpm": true, "python": true, "python3": true,
			"pytest": true, "ruby": true, "rustc": true, "swift": true,
			"uv": true, "yarn": true, "git": true, "rg": true,
		},
		MaxDuration:    2 * time.Minute,
		MaxArgs:        64,
		MaxArgBytes:    64 << 10,
		MaxOutputBytes: maxToolOutput,
	}
}

func (t RunCommand) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name: "run_command", Description: "Run a program without a shell in the repository. Pipes and shell expansion are intentionally unavailable.",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"program": map[string]any{"type": "string"}, "args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 600}}, "required": []string{"program"}, "additionalProperties": false},
	}
}
func (RunCommand) Risk(json.RawMessage) Risk { return RiskExec }
func (t RunCommand) Execute(ctx context.Context, raw json.RawMessage) Result {
	if t.Workspace == nil {
		return failed(errors.New("workspace is required"))
	}
	t.Workspace.Lock()
	defer t.Workspace.Unlock()
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
	if filepath.Base(args.Program) != args.Program || filepath.VolumeName(args.Program) != "" {
		return failed(errors.New("program must be a bare executable name resolved through PATH"))
	}
	policy := t.Policy
	defaults := DefaultCommandPolicy()
	if len(policy.AllowedPrograms) == 0 {
		policy.AllowedPrograms = defaults.AllowedPrograms
	}
	if policy.MaxDuration <= 0 {
		policy.MaxDuration = t.MaxDuration
	}
	if policy.MaxDuration <= 0 {
		policy.MaxDuration = defaults.MaxDuration
	}
	if policy.MaxArgs <= 0 {
		policy.MaxArgs = defaults.MaxArgs
	}
	if policy.MaxArgBytes <= 0 {
		policy.MaxArgBytes = defaults.MaxArgBytes
	}
	if policy.MaxOutputBytes <= 0 {
		policy.MaxOutputBytes = defaults.MaxOutputBytes
	}
	program := filepath.Base(filepath.Clean(args.Program))
	programKey := strings.ToLower(strings.TrimSuffix(program, filepath.Ext(program)))
	if !policy.AllowedPrograms[programKey] {
		return failed(fmt.Errorf("program %q is not allowed by runner policy", program))
	}
	if isShellProgram(program) {
		return failed(errors.New("shell interpreters are not allowed; pass a direct executable and argument list"))
	}
	if len(args.Args) > policy.MaxArgs {
		return failed(fmt.Errorf("too many command arguments: maximum is %d", policy.MaxArgs))
	}
	argBytes := 0
	for _, value := range args.Args {
		if strings.ContainsAny(value, "\x00\r\n") {
			return failed(errors.New("command arguments cannot contain control characters"))
		}
		if containsShellSyntax(value) {
			return failed(errors.New("shell syntax is not allowed in command arguments"))
		}
		if hasParentPathSegment(value) {
			return failed(errors.New("command arguments cannot escape the workspace"))
		}
		argBytes += len([]byte(value))
	}
	if err := validateCommandArguments(programKey, args.Args); err != nil {
		return failed(err)
	}
	if argBytes > policy.MaxArgBytes {
		return failed(fmt.Errorf("command arguments exceed the %d byte limit", policy.MaxArgBytes))
	}
	duration := policy.MaxDuration
	if duration <= 0 {
		duration = 2 * time.Minute
	}
	if args.Timeout > 0 {
		requested := time.Duration(args.Timeout) * time.Second
		if requested > duration {
			return failed(fmt.Errorf("timeout_seconds exceeds policy maximum of %s", duration))
		}
		duration = requested
	}
	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	cmd := exec.CommandContext(runCtx, args.Program, args.Args...)
	cmd.Dir = t.Workspace.Root
	cmd.Env = safeEnvironment(os.Environ())
	output, truncated, runErr := limitedCombinedOutputStatus(cmd, policy.MaxOutputBytes)
	metadata := map[string]any{"program": program, "args": args.Args, "exitCode": 0, "timedOut": errors.Is(runCtx.Err(), context.DeadlineExceeded), "outputTruncated": truncated, "durationMs": time.Since(start).Milliseconds()}
	if runErr != nil {
		metadata["exitCode"] = -1
		if exit, ok := runErr.(*exec.ExitError); ok {
			metadata["exitCode"] = exit.ExitCode()
		}
		return Result{Content: string(output) + "\n" + runErr.Error(), Metadata: metadata, IsError: true}
	}
	return Result{Content: string(output), Metadata: metadata}
}

func isShellProgram(program string) bool {
	switch strings.ToLower(strings.TrimSuffix(program, filepath.Ext(program))) {
	case "sh", "bash", "zsh", "fish", "cmd", "command", "powershell", "pwsh":
		return true
	default:
		return false
	}
}

func containsShellSyntax(value string) bool {
	return strings.ContainsAny(value, ";&|<>`$") || strings.Contains(value, "${")
}

func hasParentPathSegment(value string) bool {
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

func validateCommandArguments(program string, args []string) error {
	switch program {
	case "git":
		for _, value := range args {
			lower := strings.ToLower(value)
			if lower == "-c" || strings.HasPrefix(lower, "-c") || strings.HasPrefix(lower, "--git-dir") || strings.HasPrefix(lower, "--work-tree") || strings.HasPrefix(lower, "--exec-path") {
				return errors.New("git repository override flags are not allowed")
			}
		}
	case "npm", "npx", "pnpm", "yarn":
		for _, value := range args {
			lower := strings.ToLower(value)
			if lower == "-g" || lower == "--global" || lower == "--prefix" || strings.HasPrefix(lower, "--prefix=") {
				return errors.New("global package manager flags are not allowed")
			}
		}
	}
	return nil
}

type GitDiff struct{ Workspace *workspace.Workspace }

func (t GitDiff) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "git_diff", Description: "Show the current Git working tree diff.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"staged": map[string]any{"type": "boolean"}}, "additionalProperties": false}}
}
func (GitDiff) Risk(json.RawMessage) Risk { return RiskRead }
func (t GitDiff) Execute(ctx context.Context, raw json.RawMessage) Result {
	if t.Workspace == nil {
		return failed(errors.New("workspace is required"))
	}
	t.Workspace.RLock()
	defer t.Workspace.RUnlock()
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
	output, _, err := limitedCombinedOutputStatus(cmd, limit)
	return output, err
}

func limitedCombinedOutputStatus(cmd *exec.Cmd, limit int) ([]byte, bool, error) {
	var buffer limitedBuffer
	buffer.limit = limit
	cmd.Stdout = &buffer
	cmd.Stderr = &buffer
	err := cmd.Run()
	return buffer.Bytes(), buffer.truncated, err
}

type limitedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
			b.truncated = true
		}
		_, _ = b.Buffer.Write(p)
	} else if original > 0 {
		b.truncated = true
	}
	return original, nil
}
func failed(err error) Result { return Result{Content: err.Error(), IsError: true} }
