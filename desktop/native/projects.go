package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var bootstrapPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,120}$`)
var reservedName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\..*)?$`)

type sourceFile struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Content    string `json:"content"`
	Executable bool   `json:"executable"`
}
type SourceArchive struct {
	ManifestHash string       `json:"manifestHash"`
	Files        []sourceFile `json:"files"`
}
type PatchApplication struct {
	Applied   bool   `json:"applied"`
	FileCount int    `json:"fileCount"`
	Path      string `json:"path"`
}

func randomID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func (a *App) projectRegistry() (string, error) {
	if a.registryDir != "" {
		return a.registryDir, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ProofCode"), nil
}
func (a *App) readProjects() ([]ScratchProject, error) {
	dir, err := a.projectRegistry()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "projects.json"))
	if os.IsNotExist(err) {
		return []ScratchProject{}, nil
	}
	if err != nil {
		return nil, err
	}
	var projects []ScratchProject
	if err = json.Unmarshal(data, &projects); err != nil {
		return nil, fmt.Errorf("read local project registry: %w", err)
	}
	return projects, nil
}
func atomicJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_ = tmp.Chmod(0600)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
func (a *App) saveProjects(projects []ScratchProject) error {
	dir, err := a.projectRegistry()
	if err != nil {
		return err
	}
	return atomicJSON(filepath.Join(dir, "projects.json"), projects)
}

// BootstrapScratchProject can be retried with the same identity after a network
// failure or application restart. Paths stay in the native registry.
func (a *App) BootstrapScratchProject(bootstrapID, title string) (ScratchProject, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	if !bootstrapPattern.MatchString(bootstrapID) {
		return ScratchProject{}, errors.New("invalid bootstrap identity")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "未命名工程"
	}
	runes := []rune(title)
	if len(runes) > 60 {
		title = string(runes[:60])
	}
	projects, err := a.readProjects()
	if err != nil {
		return ScratchProject{}, err
	}
	for _, project := range projects {
		if project.BootstrapID == bootstrapID {
			if _, err := os.Stat(project.Path); err != nil {
				return ScratchProject{}, fmt.Errorf("project directory unavailable: %w", err)
			}
			return project, nil
		}
	}
	desktop := a.desktopRoot
	if desktop == "" {
		desktop, err = realDesktopDirectory()
		if err != nil {
			return ScratchProject{}, fmt.Errorf("locate Desktop known folder: %w", err)
		}
	}
	slug := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '-'
		}
		return r
	}, title)
	slug = strings.Trim(slug, " .")
	if slug == "" || reservedName.MatchString(slug) {
		slug = "project"
	}
	dir := filepath.Join(desktop, "ProofCode-Projects", slug+"_"+bootstrapID)
	if err = os.MkdirAll(filepath.Dir(dir), 0750); err != nil {
		return ScratchProject{}, err
	}
	project := ScratchProject{Name: title, LocalHandle: "desktop:" + bootstrapID, BootstrapID: bootstrapID, Path: dir}
	if err = os.Mkdir(dir, 0750); err != nil && !os.IsExist(err) {
		return ScratchProject{}, err
	}
	metadataPath := filepath.Join(dir, ".proofcode", "workspace.json")
	if data, readErr := os.ReadFile(metadataPath); readErr == nil {
		var found ScratchProject
		if json.Unmarshal(data, &found) != nil || found.BootstrapID != bootstrapID {
			return ScratchProject{}, errors.New("bootstrap directory is occupied by another project")
		}
		project = found
	} else if !os.IsNotExist(readErr) {
		return ScratchProject{}, readErr
	} else {
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			return ScratchProject{}, readErr
		}
		if len(entries) > 0 {
			return ScratchProject{}, errors.New("bootstrap directory has unexpected contents")
		}
		if err = atomicJSON(metadataPath, project); err != nil {
			return ScratchProject{}, err
		}
	}
	if _, err = os.Stat(filepath.Join(dir, ".gitignore")); os.IsNotExist(err) {
		if err = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".proofcode/\n.env\n.env.*\n!.env.example\nnode_modules/\n"), 0600); err != nil {
			return ScratchProject{}, err
		}
	}
	projects = append(projects, project)
	if err = a.saveProjects(projects); err != nil {
		return ScratchProject{}, fmt.Errorf("persist bootstrap (retry the same identity): %w", err)
	}
	return project, nil
}

func (a *App) CreateScratchProject(title string) (ScratchProject, error) {
	id, err := randomID()
	if err != nil {
		return ScratchProject{}, err
	}
	return a.BootstrapScratchProject(id, title)
}

func (a *App) RegisterLocalProject() (ScratchProject, error) {
	if a.ctx == nil {
		return ScratchProject{}, errors.New("native directory selector is unavailable")
	}
	directory, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择 ProofCode 工程目录"})
	if err != nil {
		return ScratchProject{}, err
	}
	if directory == "" {
		return ScratchProject{}, errors.New("未选择目录")
	}
	return a.registerDirectory(directory)
}
func (a *App) registerDirectory(directory string) (ScratchProject, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	abs, err := filepath.Abs(directory)
	if err != nil {
		return ScratchProject{}, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return ScratchProject{}, err
	}
	stat, err := os.Stat(abs)
	if err != nil || !stat.IsDir() {
		return ScratchProject{}, errors.New("local project must be an existing directory")
	}
	projects, err := a.readProjects()
	if err != nil {
		return ScratchProject{}, err
	}
	for _, project := range projects {
		if strings.EqualFold(project.Path, abs) {
			return project, nil
		}
	}
	id, err := randomID()
	if err != nil {
		return ScratchProject{}, err
	}
	project := ScratchProject{Name: filepath.Base(abs), LocalHandle: "desktop:" + id, BootstrapID: id, Path: abs}
	projects = append(projects, project)
	if err = a.saveProjects(projects); err != nil {
		return ScratchProject{}, err
	}
	return project, nil
}
func (a *App) ListLocalProjects() ([]ScratchProject, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	return a.readProjects()
}
func (a *App) projectByHandle(handle string) (ScratchProject, error) {
	projects, err := a.readProjects()
	if err != nil {
		return ScratchProject{}, err
	}
	for _, project := range projects {
		if project.LocalHandle == handle {
			return project, nil
		}
	}
	return ScratchProject{}, errors.New("local handle is not registered on this computer")
}

func validSourcePath(path string) bool {
	if path == "" || len(path) > 500 || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\\x00\r\n\t:*?\"<>|") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		lower := strings.ToLower(part)
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || reservedName.MatchString(part) {
			return false
		}
		switch lower {
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
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pem", ".key", ".p12", ".pfx", ".jks":
		return false
	}
	return true
}
func skipDirectory(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".proofcode", "node_modules", ".context-store", "dist", "build", "target", ".venv", "venv", "__pycache__", ".idea":
		return true
	}
	return false
}
func snapshotHash(files []sourceFile) string {
	var manifest strings.Builder
	for _, file := range files {
		fmt.Fprintf(&manifest, "%s\x00%s\x00", file.Path, file.SHA256)
		if file.Executable {
			manifest.WriteByte('1')
		} else {
			manifest.WriteByte('0')
		}
		manifest.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(manifest.String()))
	return hex.EncodeToString(sum[:])
}
func captureDirectory(directory string) (SourceArchive, error) {
	realDirectory, resolveErr := filepath.EvalSymlinks(directory)
	if resolveErr != nil {
		return SourceArchive{}, resolveErr
	}
	directory = realDirectory
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", ".")
	command.Dir = directory
	output, gitErr := command.Output()
	var candidates []string
	if gitErr == nil {
		for _, entry := range bytes.Split(output, []byte{0}) {
			if len(entry) > 0 {
				candidates = append(candidates, filepath.ToSlash(string(entry)))
			}
		}
	} else {
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if path != directory && skipDirectory(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			relative, err := filepath.Rel(directory, path)
			if err != nil {
				return err
			}
			candidates = append(candidates, filepath.ToSlash(relative))
			return nil
		})
		if err != nil {
			return SourceArchive{}, err
		}
		// Evaluate Git ignore rules in a disposable repository, so opening a
		// non-Git folder never creates .git in the user's directory.
		candidates, err = filterIgnoredFiles(ctx, directory, candidates)
		if err != nil {
			return SourceArchive{}, err
		}
	}
	sort.Strings(candidates)
	archive := SourceArchive{Files: []sourceFile{}}
	seen := map[string]bool{}
	total := 0
	for _, candidate := range candidates {
		if !validSourcePath(candidate) {
			continue
		}
		fold := strings.ToLower(candidate)
		if seen[fold] {
			return SourceArchive{}, errors.New("snapshot contains case-colliding paths")
		}
		seen[fold] = true
		path := filepath.Join(directory, filepath.FromSlash(candidate))
		stat, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return SourceArchive{}, err
		}
		if !stat.Mode().IsRegular() {
			return SourceArchive{}, fmt.Errorf("snapshot does not support symlinks/submodules: %s", candidate)
		}
		// Check every ancestor as well, including Windows directory junctions.
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return SourceArchive{}, err
		}
		relative, err := filepath.Rel(directory, resolved)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return SourceArchive{}, errors.New("snapshot path resolves outside the registered directory")
		}
		if stat.Size() > 1<<20 {
			return SourceArchive{}, fmt.Errorf("snapshot file exceeds 1 MiB: %s", candidate)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return SourceArchive{}, err
		}
		total += len(data)
		if len(data) > 1<<20 || total > 8<<20 || len(archive.Files) >= 2000 {
			return SourceArchive{}, errors.New("source snapshot exceeds 8 MiB or 2000 files")
		}
		sum := sha256.Sum256(data)
		archive.Files = append(archive.Files, sourceFile{Path: candidate, SHA256: hex.EncodeToString(sum[:]), Content: base64.StdEncoding.EncodeToString(data), Executable: stat.Mode()&0111 != 0})
	}
	archive.ManifestHash = snapshotHash(archive.Files)
	return archive, nil
}
func (a *App) CaptureProjectSource(handle string) (SourceArchive, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	project, err := a.projectByHandle(handle)
	if err != nil {
		return SourceArchive{}, err
	}
	if err = a.recoverApplications(project); err != nil {
		return SourceArchive{}, err
	}
	return captureDirectory(project.Path)
}

func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
func materializeSource(dir string, archive SourceArchive) error {
	for _, file := range archive.Files {
		data, err := base64.StdEncoding.DecodeString(file.Content)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, filepath.FromSlash(file.Path))
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

type applicationJournal struct {
	Handle string        `json:"handle"`
	Before SourceArchive `json:"before"`
	After  SourceArchive `json:"after"`
	Status string        `json:"status"`
}

func fileMap(archive SourceArchive) map[string]sourceFile {
	values := map[string]sourceFile{}
	for _, file := range archive.Files {
		values[file.Path] = file
	}
	return values
}
func fileEqual(left sourceFile, leftOK bool, right sourceFile, rightOK bool) bool {
	return leftOK == rightOK && (!leftOK || left.SHA256 == right.SHA256 && left.Executable == right.Executable)
}
func safeTarget(root, relative string) (string, error) {
	resolvedRoot, resolveErr := filepath.EvalSymlinks(root)
	if resolveErr != nil {
		return "", resolveErr
	}
	root = resolvedRoot
	if !validSourcePath(relative) {
		return "", errors.New("patch contains an unsafe path")
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	parent := filepath.Dir(path)
	for {
		_, err := os.Lstat(parent)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return "", err
			}
			rel, err := filepath.Rel(root, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", errors.New("patch target escapes project")
			}
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent = filepath.Dir(parent)
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return "", errors.New("patch cannot replace symlinks or directories")
	}
	return path, nil
}
func changedPaths(before, after SourceArchive) []string {
	left, right := fileMap(before), fileMap(after)
	set := map[string]bool{}
	for path := range left {
		set[path] = true
	}
	for path := range right {
		set[path] = true
	}
	var paths []string
	for path := range set {
		l, lok := left[path]
		r, rok := right[path]
		if !fileEqual(l, lok, r, rok) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

// Recovery never overwrites a third-party edit. A partial application is rolled
// back from the persisted before image only if every touched file is recognizable.
func (a *App) recoverApplications(project ScratchProject) error {
	registry, err := a.projectRegistry()
	if err != nil {
		return err
	}
	path := filepath.Join(registry, "apply", strings.TrimPrefix(project.LocalHandle, "desktop:")+".json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal applicationJournal
	if err = json.Unmarshal(data, &journal); err != nil {
		return err
	}
	if journal.Status != "APPLYING" {
		return nil
	}
	current, err := captureDirectory(project.Path)
	if err != nil {
		return err
	}
	now, before, after := fileMap(current), fileMap(journal.Before), fileMap(journal.After)
	paths := changedPaths(journal.Before, journal.After)
	for _, relative := range paths {
		value, ok := now[relative]
		b, bok := before[relative]
		f, fok := after[relative]
		if !fileEqual(value, ok, b, bok) && !fileEqual(value, ok, f, fok) {
			return fmt.Errorf("interrupted patch conflicts with current file %s; backup preserved in local app storage", relative)
		}
	}
	for _, relative := range paths {
		value, ok := before[relative]
		previous, present := now[relative]
		if fileEqual(previous, present, value, ok) {
			continue
		}
		if err = conditionalSourceWrite(project.Path, relative, previous, present, value, ok); err != nil {
			return err
		}
	}
	journal.Status = "ROLLED_BACK"
	return atomicJSON(path, journal)
}
func (a *App) ApplyProjectPatch(handle, expectedManifestHash, patch string) (PatchApplication, error) {
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	project, err := a.projectByHandle(handle)
	if err != nil {
		return PatchApplication{}, err
	}
	if err = a.recoverApplications(project); err != nil {
		return PatchApplication{}, err
	}
	before, err := captureDirectory(project.Path)
	if err != nil {
		return PatchApplication{}, err
	}
	if before.ManifestHash != expectedManifestHash {
		return PatchApplication{}, errors.New("工程文件已发生变化，请重新生成或合并补丁")
	}
	if patch == "" || len(patch) > 8<<20 {
		return PatchApplication{}, errors.New("patch must be nonempty and no larger than 8 MiB")
	}
	registry, err := a.projectRegistry()
	if err != nil {
		return PatchApplication{}, err
	}
	if err = os.MkdirAll(registry, 0700); err != nil {
		return PatchApplication{}, err
	}
	stage, err := os.MkdirTemp(registry, "patch-stage-*")
	if err != nil {
		return PatchApplication{}, err
	}
	defer os.RemoveAll(stage)
	if err = materializeSource(stage, before); err != nil {
		return PatchApplication{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err = runGit(ctx, stage, "init", "--initial-branch=main"); err != nil {
		return PatchApplication{}, err
	}
	// Keep the captured bytes stable regardless of the user's global Git
	// newline settings; this repository is used only to validate the patch.
	if _, err = runGit(ctx, stage, "config", "core.autocrlf", "false"); err != nil {
		return PatchApplication{}, err
	}
	if _, err = runGit(ctx, stage, "add", "-A"); err != nil {
		return PatchApplication{}, err
	}
	if _, err = runGit(ctx, stage, "-c", "user.name=ProofCode", "-c", "user.email=proofcode@local", "-c", "core.hooksPath=", "commit", "--allow-empty", "-m", "Private source baseline"); err != nil {
		return PatchApplication{}, err
	}
	command := exec.CommandContext(ctx, "git", "apply", "--index", "--binary", "--whitespace=nowarn", "-")
	command.Dir = stage
	command.Stdin = strings.NewReader(patch)
	if output, err := command.CombinedOutput(); err != nil {
		return PatchApplication{}, fmt.Errorf("validate patch: %w: %s", err, output)
	}
	// Inspect the entire resulting tree: filtering secrets here would hide an
	// invalid patch instead of rejecting it.
	err = filepath.WalkDir(stage, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == stage {
			return nil
		}
		relative, _ := filepath.Rel(stage, path)
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !validSourcePath(filepath.ToSlash(relative)) || entry.Type()&os.ModeSymlink != 0 {
			return errors.New("patch contains unsupported or private files")
		}
		return nil
	})
	if err != nil {
		return PatchApplication{}, err
	}
	after, err := captureDirectory(stage)
	if err != nil {
		return PatchApplication{}, err
	}
	paths := changedPaths(before, after)
	if len(paths) == 0 {
		return PatchApplication{Applied: true, Path: project.Path}, nil
	}
	current, err := captureDirectory(project.Path)
	if err != nil {
		return PatchApplication{}, err
	}
	if current.ManifestHash != before.ManifestHash {
		return PatchApplication{}, errors.New("工程在补丁校验期间发生变化，请重新合并")
	}
	journal := applicationJournal{Handle: handle, Before: before, After: after, Status: "APPLYING"}
	journalPath := filepath.Join(registry, "apply", strings.TrimPrefix(handle, "desktop:")+".json")
	if err = atomicJSON(journalPath, journal); err != nil {
		return PatchApplication{}, err
	}
	values := fileMap(after)
	beforeValues := fileMap(before)
	for _, relative := range paths {
		previous, present := beforeValues[relative]
		value, ok := values[relative]
		if err = conditionalSourceWrite(project.Path, relative, previous, present, value, ok); err != nil {
			rollbackErr := a.recoverApplications(project)
			return PatchApplication{}, errors.Join(err, rollbackErr)
		}
	}
	journal.Status = "APPLIED"
	if err = atomicJSON(journalPath, journal); err != nil {
		return PatchApplication{}, fmt.Errorf("files written; local application receipt pending recovery: %w", err)
	}
	return PatchApplication{Applied: true, FileCount: len(paths), Path: project.Path}, nil
}

func filterIgnoredFiles(ctx context.Context, directory string, candidates []string) ([]string, error) {
	if len(candidates) == 0 {
		return candidates, nil
	}
	stage, err := os.MkdirTemp("", "proofcode-ignore-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if _, err = runGit(ctx, stage, "init", "--quiet"); err != nil {
		return nil, errors.New("Git is required to respect ignore rules before uploading a folder")
	}
	command := exec.CommandContext(ctx, "git", "--git-dir="+filepath.Join(stage, ".git"), "--work-tree="+directory, "check-ignore", "--no-index", "-z", "--stdin")
	command.Dir = directory
	command.Stdin = strings.NewReader(strings.Join(candidates, "\x00") + "\x00")
	output, err := command.Output()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return nil, fmt.Errorf("evaluate folder ignore rules: %w", err)
	}
	ignored := map[string]bool{}
	for _, path := range strings.Split(string(output), "\x00") {
		ignored[path] = true
	}
	result := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if !ignored[path] {
			result = append(result, path)
		}
	}
	return result, nil
}
