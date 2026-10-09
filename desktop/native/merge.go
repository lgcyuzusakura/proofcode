package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type MergeFile struct {
	Path     string `json:"path"`
	Base     string `json:"base"`
	Ours     string `json:"ours"`
	Theirs   string `json:"theirs"`
	Result   string `json:"result"`
	Resolved bool   `json:"resolved"`
	Editable bool   `json:"editable"`
	Method   string `json:"method"`
}
type MergePreview struct {
	ID       string      `json:"id"`
	Revision string      `json:"revision"`
	Target   string      `json:"target"`
	Ours     string      `json:"ours"`
	Theirs   string      `json:"theirs"`
	Base     string      `json:"base"`
	Files    []MergeFile `json:"files"`
	Patch    string      `json:"patch"`
	Ready    bool        `json:"ready"`
	Engine   string      `json:"engine"`
	Path     string      `json:"path"`
}
type mergeRun struct {
	handle, originalRevision, branch, dir string
	preview                               MergePreview
}

// Preview commands use a separate index, refs and config; repository merge
// drivers and hooks are never executed during an unapproved preview.
func mergeGit(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	base := []string{"-c", "core.hooksPath=" + filepath.Join(os.TempDir(), "proofcode-no-hooks"), "-c", "core.autocrlf=false", "-c", "core.attributesFile=" + os.DevNull, "-c", "commit.gpgSign=false", "-c", "user.name=ProofCode merge", "-c", "user.email=merge@proofcode.local"}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("Git merge preview: %w: %s", err, out)
	}
	return out, nil
}
func mergeCommit(dir, ref string) (string, error) {
	out, err := mergeGit(dir, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(string(out)), err
}
func hasMergeMarkers(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "<<<<<<<") || strings.HasPrefix(line, "|||||||") || strings.HasPrefix(line, ">>>>>>>") || line == "=======" {
			return true
		}
	}
	return false
}
func boundedMergeText(data []byte) bool {
	return len(data) <= 1<<20 && utf8.Valid(data) && !strings.ContainsRune(string(data), 0)
}
func mergirafExecutable() string {
	if value := os.Getenv("PROOFCODE_MERGIRAF_EXECUTABLE"); value != "" {
		return value
	}
	value, _ := exec.LookPath("mergiraf")
	return value
}
func (a *App) PreviewProjectMerge(handle, expectedRevision, target string) (MergePreview, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	p, err := a.projectByHandle(handle)
	if err != nil {
		return MergePreview{}, err
	}
	state, err := gitState(p)
	if err != nil {
		return MergePreview{}, err
	}
	if state.Revision != expectedRevision || len(state.Files) > 0 || state.Branch == "detached HEAD" {
		return MergePreview{}, errors.New("merge preview requires the current clean checkout on a branch")
	}
	if len(target) > 200 || strings.HasPrefix(target, "-") || strings.ContainsAny(target, "\x00\r\n") || target == "" {
		return MergePreview{}, errors.New("invalid merge target")
	}
	ours, err := mergeCommit(p.Path, "HEAD")
	if err != nil {
		return MergePreview{}, err
	}
	theirs, err := mergeCommit(p.Path, target)
	if err != nil {
		return MergePreview{}, err
	}
	base, err := mergeGit(p.Path, "merge-base", ours, theirs)
	if err != nil {
		return MergePreview{}, errors.New("merge requires a common ancestor")
	}
	dir, err := os.MkdirTemp("", "proofcode-merge-")
	if err != nil {
		return MergePreview{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	// --shared avoids copying unchanged blobs. No Git config is inherited.
	if _, err = mergeGit(dir, "clone", "--shared", "--no-checkout", "--", p.Path, dir); err != nil {
		return MergePreview{}, err
	}
	if _, err = mergeGit(dir, "checkout", "--detach", ours); err != nil {
		return MergePreview{}, err
	}
	out, mergeErr := mergeGit(dir, "-c", "merge.conflictStyle=diff3", "merge", "--no-commit", "--no-ff", theirs)
	if mergeErr != nil {
		unmerged, _ := mergeGit(dir, "ls-files", "-u")
		if len(unmerged) == 0 {
			return MergePreview{}, fmt.Errorf("preview failed: %s", out)
		}
	}
	id, err := randomID()
	if err != nil {
		return MergePreview{}, err
	}
	run := &mergeRun{handle: handle, originalRevision: state.Revision, branch: state.Branch, dir: dir, preview: MergePreview{ID: id, Target: target, Ours: ours, Theirs: theirs, Base: strings.TrimSpace(string(base)), Files: []MergeFile{}, Engine: "Git ort", Path: dir}}
	stages, err := mergeGit(dir, "ls-files", "-u", "-z")
	if err != nil {
		return MergePreview{}, err
	}
	paths := map[string]map[string]string{}
	for _, record := range strings.Split(string(stages), "\x00") {
		meta, path, ok := strings.Cut(record, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			return MergePreview{}, errors.New("invalid merge index")
		}
		if paths[path] == nil {
			paths[path] = map[string]string{}
		}
		paths[path][fields[2]] = fields[0] + " " + fields[1]
	}
	names := []string{}
	for path := range paths {
		names = append(names, path)
	}
	sort.Strings(names)
	for _, path := range names {
		if !validSourcePath(path) {
			return MergePreview{}, fmt.Errorf("protected merge path: %s", path)
		}
		file := MergeFile{Path: path, Method: "manual", Editable: len(paths[path]) == 3}
		contents := map[string]string{}
		for stage, meta := range paths[path] {
			mode, hash, _ := strings.Cut(meta, " ")
			if mode != "100644" && mode != "100755" {
				file.Editable = false
				continue
			}
			data, readErr := mergeGit(dir, "cat-file", "blob", hash)
			if readErr != nil || !boundedMergeText(data) {
				file.Editable = false
				continue
			}
			contents[stage] = string(data)
		}
		file.Base, file.Ours, file.Theirs = contents["1"], contents["2"], contents["3"]
		data, readErr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if readErr != nil || !boundedMergeText(data) {
			file.Editable = false
		} else {
			file.Result = string(data)
		}
		if engine := mergirafExecutable(); file.Editable && engine != "" {
			inputDir, inputErr := os.MkdirTemp("", "proofcode-merge-input-")
			if inputErr != nil {
				return MergePreview{}, inputErr
			}
			inputs := []string{}
			for i, content := range []string{file.Base, file.Ours, file.Theirs} {
				name := filepath.Join(inputDir, fmt.Sprintf("%d%s", i, filepath.Ext(path)))
				if err = os.WriteFile(name, []byte(content), 0600); err != nil {
					_ = os.RemoveAll(inputDir)
					return MergePreview{}, err
				}
				inputs = append(inputs, name)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			cmd := exec.CommandContext(ctx, engine, append([]string{"merge"}, inputs...)...)
			cmd.Dir = inputDir
			result, engineErr := cmd.Output()
			cancel()
			_ = os.RemoveAll(inputDir)
			if engineErr == nil && boundedMergeText(result) && !hasMergeMarkers(string(result)) {
				file.Result = string(result)
				file.Resolved = true
				file.Method = "mergiraf"
				if err = os.WriteFile(filepath.Join(dir, filepath.FromSlash(path)), result, 0644); err != nil {
					return MergePreview{}, err
				}
				if _, err = mergeGit(dir, "add", "--", path); err != nil {
					return MergePreview{}, err
				}
			}
			run.preview.Engine = "Git ort + Mergiraf"
		}
		run.preview.Files = append(run.preview.Files, file)
	}
	if err = refreshMerge(run); err != nil {
		return MergePreview{}, err
	}
	if a.merges == nil {
		a.merges = map[string]*mergeRun{}
	}
	a.merges[id] = run
	keep = true
	return run.preview, nil
}
func refreshMerge(run *mergeRun) error {
	index, err := mergeGit(run.dir, "ls-files", "--stage", "-z")
	if err != nil {
		return err
	}
	for _, record := range strings.Split(string(index), "\x00") {
		meta, _, ok := strings.Cut(record, "\t")
		if !ok {
			continue
		}
		if !strings.HasPrefix(meta, "100644 ") && !strings.HasPrefix(meta, "100755 ") {
			return errors.New("merge preview excludes symlinks and submodules")
		}
	}
	names, err := mergeGit(run.dir, "diff", "--name-only", "-z", run.preview.Ours, "--")
	if err != nil {
		return err
	}
	for _, path := range strings.Split(string(names), "\x00") {
		if path != "" && !validSourcePath(path) {
			return fmt.Errorf("protected merge path: %s", path)
		}
	}
	patch, err := mergeGit(run.dir, "diff", "--no-ext-diff", "--no-textconv", run.preview.Ours, "--")
	if err != nil {
		return err
	}
	if len(patch) > 2<<20 {
		return errors.New("merge patch exceeds 2 MiB")
	}
	unmerged, err := mergeGit(run.dir, "ls-files", "-u", "-z")
	if err != nil {
		return err
	}
	run.preview.Patch = string(patch)
	run.preview.Ready = len(unmerged) == 0
	staged, err := mergeGit(run.dir, "diff", "--cached", "--no-ext-diff", "--no-textconv", run.preview.Ours, "--")
	if err != nil {
		return err
	}
	run.preview.Revision = textHash(run.preview.Ours + run.preview.Theirs + string(patch) + string(unmerged) + string(index) + string(staged))
	return nil
}
func (a *App) mergeByID(handle, id, revision string) (*mergeRun, error) {
	if _, err := a.projectByHandle(handle); err != nil {
		return nil, err
	}
	run := a.merges[id]
	if run == nil || run.handle != handle {
		return nil, errors.New("merge preview not found in this project")
	}
	previous := run.preview.Revision
	if err := refreshMerge(run); err != nil {
		return nil, err
	}
	if previous != revision || run.preview.Revision != revision {
		return nil, errors.New("merge preview changed; create a new preview")
	}
	return run, nil
}
func (a *App) ResolveProjectMerge(handle, id, revision, path, content string) (MergePreview, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	run, err := a.mergeByID(handle, id, revision)
	if err != nil {
		return MergePreview{}, err
	}
	if !boundedMergeText([]byte(content)) || hasMergeMarkers(content) {
		return MergePreview{}, errors.New("bounded UTF-8 resolution without conflict markers required")
	}
	for i, file := range run.preview.Files {
		if file.Path != path {
			continue
		}
		if !file.Editable {
			return MergePreview{}, errors.New("binary, rename/delete and mode conflicts require external review")
		}
		if err = os.WriteFile(filepath.Join(run.dir, filepath.FromSlash(path)), []byte(content), 0644); err != nil {
			return MergePreview{}, err
		}
		if _, err = mergeGit(run.dir, "add", "--", path); err != nil {
			return MergePreview{}, err
		}
		run.preview.Files[i].Result = content
		run.preview.Files[i].Resolved = true
		run.preview.Files[i].Method = "manual"
		if err = refreshMerge(run); err != nil {
			return MergePreview{}, err
		}
		return run.preview, nil
	}
	return MergePreview{}, errors.New("select a conflict from this preview")
}
func (a *App) ApplyProjectMerge(handle, id, revision string) (GitState, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	run, err := a.mergeByID(handle, id, revision)
	if err != nil {
		return GitState{}, err
	}
	if !run.preview.Ready {
		return GitState{}, errors.New("resolve every conflict before approval")
	}
	p, err := a.projectByHandle(handle)
	if err != nil {
		return GitState{}, err
	}
	state, err := gitState(p)
	if err != nil {
		return state, err
	}
	target, err := mergeCommit(p.Path, run.preview.Target)
	if err != nil || state.Revision != run.originalRevision || target != run.preview.Theirs || state.Branch != run.branch {
		return state, errors.New("checkout or target moved; create a new merge preview")
	}
	// Both the candidate commit and approved digest are recorded before the
	// original checkout is advanced. Approval imports only this exact tree.
	tree, err := mergeGit(run.dir, "write-tree")
	if err != nil {
		return state, err
	}
	commit, err := mergeGit(run.dir, "-c", "user.name=ProofCode merge", "-c", "user.email=merge@proofcode.local", "commit-tree", strings.TrimSpace(string(tree)), "-p", run.preview.Ours, "-p", run.preview.Theirs, "-m", "Merge "+run.preview.Target+" (reviewed in ProofCode)")
	if err != nil {
		return state, err
	}
	hash := strings.TrimSpace(string(commit))
	registry, err := a.projectRegistry()
	if err != nil {
		return state, err
	}
	audit := map[string]any{"id": id, "handle": handle, "revision": revision, "ours": run.preview.Ours, "theirs": run.preview.Theirs, "commit": hash, "status": "APPROVED", "at": time.Now().UTC()}
	journal := filepath.Join(registry, "merge-history", id+".json")
	if err = atomicJSON(journal, audit); err != nil {
		return state, err
	}
	if _, err = mergeGit(p.Path, "fetch", "--no-write-fetch-head", "--", run.dir, hash); err != nil {
		return state, err
	}
	if _, err = mergeGit(p.Path, "update-ref", "refs/proofcode/backups/"+id, run.preview.Ours, ""); err != nil {
		return state, err
	}
	if _, err = mergeGit(p.Path, "merge", "--ff-only", hash); err != nil {
		audit["status"] = "APPLY_FAILED"
		_ = atomicJSON(journal, audit)
		return state, err
	}
	audit["status"] = "APPLIED"
	if err = atomicJSON(journal, audit); err != nil {
		return state, fmt.Errorf("merge applied; audit reconciliation required: %w", err)
	}
	delete(a.merges, id)
	_ = os.RemoveAll(run.dir)
	return gitState(p)
}
func (a *App) DiscardProjectMerge(handle, id string) error {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	if _, err := a.projectByHandle(handle); err != nil {
		return err
	}
	run := a.merges[id]
	if run == nil || run.handle != handle {
		return errors.New("merge preview not found")
	}
	delete(a.merges, id)
	return os.RemoveAll(run.dir)
}
