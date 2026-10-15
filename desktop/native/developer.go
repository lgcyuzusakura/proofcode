package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type EditorFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
}
type ProjectFile struct {
	Path string `json:"path"`
	Size int    `json:"size"`
	Hash string `json:"hash"`
}

func (a *App) ListProjectFiles(handle string) ([]ProjectFile, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	p, err := a.projectByHandle(handle)
	if err != nil {
		return nil, err
	}
	archive, err := captureDirectory(p.Path)
	if err != nil {
		return nil, err
	}
	files := make([]ProjectFile, 0, len(archive.Files))
	for _, f := range archive.Files {
		data, _ := base64.StdEncoding.DecodeString(f.Content)
		files = append(files, ProjectFile{f.Path, len(data), f.SHA256})
	}
	return files, nil
}
func (a *App) ReadProjectFile(handle, path string) (EditorFile, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	p, err := a.projectByHandle(handle)
	if err != nil {
		return EditorFile{}, err
	}
	if !validSourcePath(path) {
		return EditorFile{}, errors.New("invalid editor path")
	}
	archive, err := captureDirectory(p.Path)
	if err != nil {
		return EditorFile{}, err
	}
	for _, f := range archive.Files {
		if f.Path == path {
			data, _ := base64.StdEncoding.DecodeString(f.Content)
			if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
				return EditorFile{}, errors.New("binary files are not editable")
			}
			return EditorFile{path, string(data), f.SHA256}, nil
		}
	}
	return EditorFile{}, errors.New("file is absent or excluded from project source")
}
func textHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}
func (a *App) SaveProjectFile(handle, path, expectedHash, content string) (EditorFile, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	if !validSourcePath(path) || len(content) > 1<<20 || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return EditorFile{}, errors.New("bounded UTF-8 source file required")
	}
	p, err := a.projectByHandle(handle)
	if err != nil {
		return EditorFile{}, err
	}
	if err = a.recoverApplications(p); err != nil {
		return EditorFile{}, err
	}
	archive, err := captureDirectory(p.Path)
	if err != nil {
		return EditorFile{}, err
	}
	var before sourceFile
	exists := false
	for _, f := range archive.Files {
		if f.Path == path {
			before = f
			exists = true
			break
		}
	}
	if (exists && expectedHash != before.SHA256) || (!exists && expectedHash != "") {
		return EditorFile{}, errors.New("file changed or disappeared; reload before saving")
	}
	next := sourceFile{Path: path, SHA256: textHash(content), Content: base64.StdEncoding.EncodeToString([]byte(content)), Executable: before.Executable}
	id, err := randomID()
	if err != nil {
		return EditorFile{}, err
	}
	journal := filepath.Join(p.Path, ".proofcode", "editor-history", id+".json")
	entry := map[string]any{"version": 1, "path": path, "before": before, "existed": exists, "afterHash": next.SHA256, "status": "PREPARED", "createdAt": time.Now().UTC()}
	if err = atomicJSON(journal, entry); err != nil {
		return EditorFile{}, err
	}
	if err = conditionalSourceWrite(p.Path, path, before, exists, next, true); err != nil {
		entry["status"] = "CONFLICT"
		_ = atomicJSON(journal, entry)
		return EditorFile{}, err
	}
	entry["status"] = "SAVED"
	if err = atomicJSON(journal, entry); err != nil {
		return EditorFile{}, fmt.Errorf("file saved but history needs reconciliation: %w", err)
	}
	return EditorFile{path, content, next.SHA256}, nil
}

type GitFile struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath,omitempty"`
	Index        string `json:"index"`
	Working      string `json:"working"`
	Operable     bool   `json:"operable"`
}
type GitCommit struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}
type GitState struct {
	Initialized bool        `json:"initialized"`
	Branch      string      `json:"branch"`
	Revision    string      `json:"revision"`
	Files       []GitFile   `json:"files"`
	Branches    []string    `json:"branches"`
	Commits     []GitCommit `json:"commits"`
	Diff        string      `json:"diff"`
	StagedDiff  string      `json:"stagedDiff"`
	Origin      string      `json:"origin"`
}

func gitState(p ScratchProject) (GitState, error) {
	state := GitState{Files: []GitFile{}, Branches: []string{}, Commits: []GitCommit{}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := runGit(ctx, p.Path, "rev-parse", "--show-toplevel"); err != nil {
		return state, nil
	}
	// A project nested in another checkout must not operate on its parent's index.
	top, err := runGit(ctx, p.Path, "rev-parse", "--show-toplevel")
	if err != nil {
		return state, err
	}
	root, _ := filepath.EvalSymlinks(p.Path)
	topRoot, _ := filepath.EvalSymlinks(strings.TrimSpace(string(top)))
	if !strings.EqualFold(filepath.Clean(root), filepath.Clean(topRoot)) {
		return state, errors.New("registered directory must be the Git repository root")
	}
	state.Initialized = true
	branch, _ := runGit(ctx, p.Path, "symbolic-ref", "--short", "-q", "HEAD")
	state.Branch = strings.TrimSpace(string(branch))
	if state.Branch == "" {
		state.Branch = "detached HEAD"
	}
	raw, err := runGit(ctx, p.Path, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return state, err
	}
	records := strings.Split(string(raw), "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) < 4 {
			continue
		}
		f := GitFile{Path: record[3:], Index: string(record[0]), Working: string(record[1])}
		f.Operable = validSourcePath(f.Path)
		if record[0] == 'R' || record[0] == 'C' {
			i++
			if i < len(records) {
				f.PreviousPath = records[i]
			}
		}
		state.Files = append(state.Files, f)
	}
	diff, err := runGit(ctx, p.Path, "diff", "--no-ext-diff", "--no-textconv", "--")
	if err != nil {
		return state, err
	}
	staged, err := runGit(ctx, p.Path, "diff", "--cached", "--no-ext-diff", "--no-textconv", "--")
	if err != nil {
		return state, err
	}
	if len(diff) > 2<<20 || len(staged) > 2<<20 {
		return state, errors.New("diff exceeds 2 MiB; use a scoped external review")
	}
	state.Diff = string(diff)
	state.StagedDiff = string(staged)
	origin, _ := runGit(ctx, p.Path, "remote", "get-url", "origin")
	rawOrigin := strings.TrimSpace(string(origin))
	state.Origin = displayGitOrigin(rawOrigin)
	refs, _ := runGit(ctx, p.Path, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	for _, branch := range strings.Split(strings.TrimSpace(string(refs)), "\n") {
		if branch != "" {
			state.Branches = append(state.Branches, branch)
		}
	}
	log, _ := runGit(ctx, p.Path, "log", "-30", "--format=%H%x00%an%x00%aI%x00%s")
	for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
		parts := strings.Split(line, "\x00")
		if len(parts) == 4 {
			state.Commits = append(state.Commits, GitCommit{parts[0], parts[1], parts[2], parts[3]})
		}
	}
	archive, err := captureDirectory(p.Path)
	if err != nil {
		return state, err
	}
	head, _ := runGit(ctx, p.Path, "rev-parse", "--verify", "HEAD")
	index, err := runGit(ctx, p.Path, "ls-files", "--stage", "-z")
	if err != nil {
		return state, err
	}
	state.Revision = textHash(string(raw) + "\x00" + state.Diff + "\x00" + state.StagedDiff + "\x00" + archive.ManifestHash + "\x00" + state.Branch + "\x00" + string(head) + "\x00" + string(index) + "\x00" + rawOrigin)
	return state, nil
}
func displayGitOrigin(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return value
	}
	if parsed.User != nil && !(parsed.Scheme == "ssh" && parsed.User.String() == "git") {
		parsed.User = url.User("REDACTED")
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String()
}
func (a *App) GetProjectGit(handle string) (GitState, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	p, err := a.projectByHandle(handle)
	if err != nil {
		return GitState{}, err
	}
	return gitState(p)
}
func (a *App) ProjectGitAction(handle, action, expectedRevision, value string, paths []string) (GitState, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	p, err := a.projectByHandle(handle)
	if err != nil {
		return GitState{}, err
	}
	state, err := gitState(p)
	if err != nil {
		return state, err
	}
	if state.Revision != expectedRevision {
		return state, errors.New("Git state changed; refresh before applying the action")
	}
	var args []string
	switch action {
	case "init":
		if state.Initialized {
			return state, errors.New("repository already initialized")
		}
		args = []string{"init", "-b", "main"}
	case "stage", "unstage":
		if len(paths) == 0 || len(paths) > 100 {
			return state, errors.New("select 1 to 100 files")
		}
		for _, path := range paths {
			if !validSourcePath(path) {
				return state, errors.New("Git path is outside source policy")
			}
			matched := false
			for _, f := range state.Files {
				if path == f.Path {
					matched = true
					if f.PreviousPath != "" && !validSourcePath(f.PreviousPath) {
						return state, errors.New("rename crosses source policy")
					}
				}
			}
			if !matched {
				return state, errors.New("select a changed file")
			}
		}
		if action == "stage" {
			args = []string{"add", "--"}
		} else if len(state.Commits) == 0 {
			args = []string{"rm", "--cached", "--"}
		} else {
			args = []string{"restore", "--staged", "--"}
		}
		args = append(args, paths...)
	case "commit":
		for _, file := range state.Files {
			if file.Index != " " && file.Index != "?" && !validSourcePath(file.Path) {
				return state, errors.New("staged file is excluded from source policy; review it externally")
			}
		}
		if strings.TrimSpace(value) == "" || len(value) > 2000 || strings.ContainsRune(value, 0) {
			return state, errors.New("bounded commit message required")
		}
		args = []string{"commit", "-m", value}
	case "create_branch", "switch_branch":
		if len(value) > 200 || strings.HasPrefix(value, "-") || strings.ContainsAny(value, "\r\n\x00") {
			return state, errors.New("invalid branch")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err = runGit(ctx, p.Path, "check-ref-format", "--branch", value)
		cancel()
		if err != nil {
			return state, errors.New("invalid branch name")
		}
		if action == "create_branch" {
			args = []string{"switch", "-c", value}
		} else {
			args = []string{"switch", value}
		}
	case "push":
		if state.Branch == "detached HEAD" {
			return state, errors.New("select a branch first")
		}
		args = []string{"push", "origin", state.Branch}
	case "set_origin":
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || len(value) > 2000 || strings.ContainsAny(value, "\r\n\x00") || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "ssh") || parsed.User != nil && (parsed.Scheme != "ssh" || parsed.User.String() != "git") {
			return state, errors.New("HTTPS or ssh://git@host repository URL without password required")
		}
		if state.Origin == "" {
			args = []string{"remote", "add", "origin", value}
		} else {
			args = []string{"remote", "set-url", "origin", value}
		}
	case "fetch":
		args = []string{"fetch", "origin"}
	case "pull":
		if len(state.Files) != 0 || state.Branch == "detached HEAD" {
			return state, errors.New("pull requires a clean checkout on a branch")
		}
		args = []string{"pull", "--ff-only", "origin", state.Branch}
	default:
		return state, errors.New("unsupported Git action")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = p.Path
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return state, fmt.Errorf("Git action failed: %s", string(out))
	}
	if action == "init" {
		// Keep local editor/recovery metadata out of a newly created repository.
		exclude := filepath.Join(p.Path, ".git", "info", "exclude")
		info, statErr := os.Lstat(exclude)
		if statErr != nil || !info.Mode().IsRegular() {
			return state, errors.New("repository initialized but local metadata exclusion could not be written")
		}
		file, openErr := os.OpenFile(exclude, os.O_APPEND|os.O_WRONLY, 0600)
		if openErr != nil {
			return state, openErr
		}
		_, writeErr := file.WriteString("\n/.proofcode/\n/.context-store/\n")
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return state, errors.Join(writeErr, closeErr)
		}
	}
	return gitState(p)
}

type CommandState struct {
	ID        string   `json:"id"`
	Program   string   `json:"program"`
	Args      []string `json:"args"`
	Output    string   `json:"output"`
	Running   bool     `json:"running"`
	ExitCode  int      `json:"exitCode"`
	Truncated bool     `json:"truncated"`
}
type commandRun struct {
	mu     sync.Mutex
	state  CommandState
	cmd    *exec.Cmd
	cancel context.CancelFunc
}
type commandWriter struct{ run *commandRun }

func (w commandWriter) Write(p []byte) (int, error) {
	w.run.mu.Lock()
	defer w.run.mu.Unlock()
	n := len(p)
	remaining := (1 << 20) - len(w.run.state.Output)
	if len(p) > remaining {
		p = p[:remaining]
		w.run.state.Truncated = true
	}
	w.run.state.Output += string(p)
	return n, nil
}
func (a *App) StartProjectCommand(handle, program string, args []string) (CommandState, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	p, err := a.projectByHandle(handle)
	if err != nil {
		return CommandState{}, err
	}
	if program == "" || len(program) > 500 || strings.ContainsAny(program, "\r\n\x00") || len(args) > 100 {
		return CommandState{}, errors.New("invalid command")
	}
	for _, arg := range args {
		if len(arg) > 8192 || strings.ContainsRune(arg, 0) {
			return CommandState{}, errors.New("invalid command argument")
		}
	}
	if a.commands == nil {
		a.commands = make(map[string]*commandRun)
	}
	if old := a.commands[handle]; old != nil {
		old.mu.Lock()
		running := old.state.Running
		old.mu.Unlock()
		if running {
			return CommandState{}, errors.New("a command is already running in this project")
		}
	}
	id, err := randomID()
	if err != nil {
		return CommandState{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	resolvedProgram, resolvedArgs, err := resolveLocalProgram(program, args)
	if err != nil {
		cancel()
		return CommandState{}, err
	}
	cmd := exec.CommandContext(ctx, resolvedProgram, resolvedArgs...)
	cmd.Dir = p.Path
	run := &commandRun{state: CommandState{ID: id, Program: program, Args: args, Running: true, ExitCode: -1}, cmd: cmd, cancel: cancel}
	cmd.Stdout = commandWriter{run}
	cmd.Stderr = commandWriter{run}
	if err = cmd.Start(); err != nil {
		cancel()
		return CommandState{}, err
	}
	a.commands[handle] = run
	run.mu.Lock()
	initial := run.state
	run.mu.Unlock()
	go func() {
		err := cmd.Wait()
		run.mu.Lock()
		defer run.mu.Unlock()
		run.state.Running = false
		run.state.ExitCode = 0
		if err != nil {
			run.state.ExitCode = -1
			if exit, ok := err.(*exec.ExitError); ok {
				run.state.ExitCode = exit.ExitCode()
			}
		}
		cancel()
	}()
	return initial, nil
}

// npm/npx on Windows are .cmd launchers. Run their JavaScript entry directly
// through Node so arguments are never interpolated into a command shell.
func resolveLocalProgram(program string, args []string) (string, []string, error) {
	resolved, err := exec.LookPath(program)
	if err != nil {
		return "", nil, err
	}
	if runtime.GOOS != "windows" || !strings.EqualFold(filepath.Ext(resolved), ".cmd") && !strings.EqualFold(filepath.Ext(resolved), ".bat") {
		return resolved, args, nil
	}
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(resolved)), filepath.Ext(resolved))
	if name != "npm" && name != "npx" {
		return "", nil, errors.New("batch launchers require an explicit interpreter; use an executable or script")
	}
	cli := filepath.Join(filepath.Dir(resolved), "node_modules", "npm", "bin", name+"-cli.js")
	if _, err = os.Stat(cli); err != nil {
		return "", nil, fmt.Errorf("Node package manager entry not found: %w", err)
	}
	node, err := exec.LookPath("node")
	return node, append([]string{cli}, args...), err
}
func (a *App) GetProjectCommand(handle string) (CommandState, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	if _, err := a.projectByHandle(handle); err != nil {
		return CommandState{}, err
	}
	run := a.commands[handle]
	if run == nil {
		return CommandState{Args: []string{}}, nil
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.state, nil
}
func (a *App) StopProjectCommand(handle, id string) error {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	if _, err := a.projectByHandle(handle); err != nil {
		return err
	}
	run := a.commands[handle]
	if run == nil {
		return errors.New("command session does not match project")
	}
	run.mu.Lock()
	match, running := run.state.ID == id, run.state.Running
	run.mu.Unlock()
	if !match {
		return errors.New("command session does not match project")
	}
	if !running {
		return nil
	}
	err := stopCommandTree(run.cmd)
	run.cancel()
	return err
}
