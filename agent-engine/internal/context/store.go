package context

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	IndexVersion       = "proofcode.index.go-ast-exact.v2"
	AlgorithmVersion   = "proofcode.rag.versioned-weighted-rrf.v2"
	CompressionVersion = "proofcode.context.retrievable.v2"
	MaxReadBytes       = 64 << 10
)

// Scope is fixed by the execution host, never taken from model arguments.
// Empty legacy conversation/task identifiers are distinct from populated ones.
type Scope struct {
	ProjectID      string `json:"projectId"`
	WorkspaceID    string `json:"workspaceId"`
	ConversationID string `json:"conversationId,omitempty"`
	TaskID         string `json:"taskId,omitempty"`
	AttemptID      string `json:"attemptId,omitempty"`
}

func (s Scope) validate() error {
	if s.ProjectID == "" || s.WorkspaceID == "" {
		return errors.New("context scope requires project and workspace")
	}
	for _, v := range []string{s.ProjectID, s.WorkspaceID, s.ConversationID, s.TaskID, s.AttemptID} {
		if len(v) > 256 || strings.ContainsAny(v, "\x00\r\n") {
			return errors.New("invalid context scope")
		}
	}
	return nil
}
func (s Scope) key() string        { b, _ := json.Marshal(s); return digest(string(b)) }
func (s Scope) SourceScope() Scope { return Scope{ProjectID: s.ProjectID, WorkspaceID: s.WorkspaceID} }

type ManifestFile struct {
	Path     string  `json:"path"`
	BlobHash string  `json:"blobHash"`
	Chunks   []Chunk `json:"chunks"`
}

// Manifest is content addressed. It describes one generation only: no current
// file is ever spliced into a historical generation.
type Manifest struct {
	ID               string         `json:"id,omitempty"`
	Scope            Scope          `json:"scope"`
	BaseCommit       string         `json:"baseCommit,omitempty"`
	IndexVersion     string         `json:"indexVersion"`
	AlgorithmVersion string         `json:"algorithmVersion"`
	ChunkLines       int            `json:"chunkLines"`
	IndexHash        string         `json:"indexHash"`
	Files            []ManifestFile `json:"files"`
}

type Reference struct {
	ID         string `json:"id,omitempty"`
	Scope      Scope  `json:"scope"`
	Kind       string `json:"kind"`
	ObjectHash string `json:"objectHash"`
	SourceHash string `json:"sourceHash"`
	StartByte  int    `json:"startByte"`
	EndByte    int    `json:"endByte"`
	SnapshotID string `json:"snapshotId,omitempty"`
	Path       string `json:"path,omitempty"`
	ToolCallID string `json:"toolCallId,omitempty"`
}

type ReadResult struct {
	ReferenceID string `json:"referenceId"`
	ObjectHash  string `json:"objectHash"`
	SourceHash  string `json:"sourceHash"`
	SnapshotID  string `json:"snapshotId,omitempty"`
	Path        string `json:"path,omitempty"`
	Offset      int    `json:"offset"`
	NextOffset  int    `json:"nextOffset"`
	TotalBytes  int    `json:"totalBytes"`
	EOF         bool   `json:"eof"`
	Content     string `json:"content"`
	BytesBase64 string `json:"bytesBase64"`
	ValidUTF8   bool   `json:"validUtf8"`
}

// Store contains private original data. Deploy it outside published worktrees
// and backups intended for Git. SHA checks protect against corruption, not a
// malicious host able to replace both data and authorization records.
type Store struct {
	root string
	mu   sync.Mutex
}

func OpenStore(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("context store directory is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return nil, err
	}
	// Reject links in every existing path component, including parent directories.
	for p := abs; ; p = filepath.Dir(p) {
		i, e := os.Lstat(p)
		if e != nil {
			return nil, e
		}
		if i.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("context store traverses symbolic link")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	s := &Store{root: abs}
	for _, dir := range []string{"objects", "indexes", "manifests", "references", "catalog", "views", "ledgers"} {
		if err = os.MkdirAll(filepath.Join(abs, dir), 0700); err != nil {
			return nil, err
		}
	}
	return s, nil
}

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func checkHash(v string) error {
	if !hashPattern.MatchString(v) {
		return errors.New("invalid context object identifier")
	}
	return nil
}

func (s *Store) path(dir, id string) (string, error) {
	if s == nil {
		return "", errors.New("context store is unavailable")
	}
	if err := checkHash(id); err != nil {
		return "", err
	}
	p := filepath.Join(s.root, dir, id)
	for parent := filepath.Dir(p); ; parent = filepath.Dir(parent) {
		i, e := os.Lstat(parent)
		if e != nil {
			return "", e
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("context store path is not a real directory")
		}
		if parent == s.root {
			break
		}
		if filepath.Dir(parent) == parent {
			return "", errors.New("context store path escaped root")
		}
	}
	if i, e := os.Lstat(p); e == nil && (!i.Mode().IsRegular() || i.Mode()&os.ModeSymlink != 0) {
		return "", errors.New("context object is not a regular file")
	}
	return p, nil
}

func (s *Store) put(dir string, data []byte) (string, error) {
	if len(data) > 64<<20 {
		return "", errors.New("context object exceeds 64 MiB durable object bound")
	}
	id := digest(string(data))
	p, err := s.path(dir, id)
	if err != nil {
		return "", err
	}
	if existing, e := s.read(dir, id, 64<<20); e == nil {
		if string(existing) != string(data) {
			return "", errors.New("immutable context object mismatch")
		}
		return id, nil
	} else if !os.IsNotExist(e) {
		return "", e
	}
	if err = atomicPrivateWrite(p, data); err != nil {
		return "", err
	}
	return id, nil
}

func atomicPrivateWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".context-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (s *Store) read(dir, id string, max int64) ([]byte, error) {
	p, err := s.path(dir, id)
	if err != nil {
		return nil, err
	}
	i, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if i.Size() > max {
		return nil, errors.New("context object exceeds read bound")
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(i, opened) {
		return nil, errors.New("context object changed while opening")
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max || digest(string(b)) != id {
		return nil, errors.New("context object hash mismatch")
	}
	return b, nil
}

func (s *Store) Publish(scope Scope, commit string, files []indexedFile, lines int) (Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scope = scope.SourceScope()
	if err := scope.validate(); err != nil {
		return Manifest{}, err
	}
	m := Manifest{Scope: scope, BaseCommit: commit, IndexVersion: IndexVersion, AlgorithmVersion: AlgorithmVersion, ChunkLines: lines, Files: []ManifestFile{}}
	for _, f := range files {
		id, err := s.put("objects", []byte(f.content))
		if err != nil {
			return Manifest{}, err
		}
		if id != f.blob {
			return Manifest{}, errors.New("source hash changed before publish")
		}
		m.Files = append(m.Files, ManifestFile{Path: f.path, BlobHash: id, Chunks: f.chunks})
	}
	indexBytes, err := json.Marshal(buildGenerationIndex(m.Files))
	if err != nil {
		return Manifest{}, err
	}
	m.IndexHash, err = s.put("indexes", indexBytes)
	if err != nil {
		return Manifest{}, err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return Manifest{}, err
	}
	id, err := s.put("manifests", b)
	if err != nil {
		return Manifest{}, err
	}
	m.ID = id
	dir := filepath.Join(s.root, "catalog", scope.key())
	if err = os.MkdirAll(dir, 0700); err != nil {
		return Manifest{}, err
	}
	if i, e := os.Lstat(dir); e != nil || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
		return Manifest{}, errors.New("invalid context catalog directory")
	}
	// Catalog entries are only hints: LoadManifest rechecks immutable bytes/scope.
	if err = atomicPrivateWrite(filepath.Join(dir, id), []byte(id)); err != nil {
		return Manifest{}, err
	}
	if err = atomicPrivateWrite(filepath.Join(dir, "current"), []byte(id)); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (s *Store) LoadManifest(scope Scope, id string) (Manifest, error) {
	scope = scope.SourceScope()
	if err := scope.validate(); err != nil {
		return Manifest{}, err
	}
	b, err := s.read("manifests", id, 64<<20)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err = json.Unmarshal(b, &m); err != nil {
		return Manifest{}, err
	}
	if m.Scope != scope {
		return Manifest{}, errors.New("context manifest belongs to another scope")
	}
	if m.IndexVersion != IndexVersion || m.AlgorithmVersion != AlgorithmVersion {
		return Manifest{}, errors.New("context manifest uses an incompatible index version")
	}
	m.ID = id
	return m, nil
}

func (s *Store) Current(scope Scope) (Manifest, error) {
	scope = scope.SourceScope()
	if err := scope.validate(); err != nil {
		return Manifest{}, err
	}
	dir := filepath.Join(s.root, "catalog", scope.key())
	i, err := os.Lstat(dir)
	if err != nil {
		return Manifest{}, err
	}
	if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
		return Manifest{}, errors.New("invalid context catalog")
	}
	p := filepath.Join(dir, "current")
	i, err = os.Lstat(p)
	if err != nil {
		return Manifest{}, err
	}
	if !i.Mode().IsRegular() || i.Mode()&os.ModeSymlink != 0 || i.Size() != 64 {
		return Manifest{}, errors.New("invalid current context pointer")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Manifest{}, err
	}
	return s.LoadManifest(scope, string(b))
}

func (s *Store) History(scope Scope, limit int) ([]Manifest, error) {
	scope = scope.SourceScope()
	if err := scope.validate(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 32 {
		limit = 16
	}
	dir := filepath.Join(s.root, "catalog", scope.key())
	i, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return []Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invalid context catalog")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		id   string
		time int64
	}
	list := []candidate{}
	for _, e := range entries {
		if !hashPattern.MatchString(e.Name()) {
			continue
		}
		i, e2 := e.Info()
		if e2 != nil {
			return nil, e2
		}
		if !i.Mode().IsRegular() || i.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("invalid history pointer")
		}
		list = append(list, candidate{e.Name(), i.ModTime().UnixNano()})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].time == list[j].time {
			return list[i].id < list[j].id
		}
		return list[i].time > list[j].time
	})
	if len(list) > limit {
		list = list[:limit]
	}
	out := []Manifest{}
	for _, c := range list {
		m, e := s.LoadManifest(scope, c.id)
		if e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *Store) SaveReference(ref Reference, data []byte) (Reference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ref.Kind == "source" {
		ref.Scope = ref.Scope.SourceScope()
	}
	if err := ref.Scope.validate(); err != nil {
		return Reference{}, err
	}
	id, err := s.put("objects", data)
	if err != nil {
		return Reference{}, err
	}
	if ref.ObjectHash != "" && ref.ObjectHash != id {
		return Reference{}, errors.New("reference source object mismatch")
	}
	ref.ObjectHash = id
	if ref.StartByte < 0 || ref.EndByte < ref.StartByte || ref.EndByte > len(data) {
		return Reference{}, errors.New("invalid source reference range")
	}
	hash := digest(string(data[ref.StartByte:ref.EndByte]))
	if ref.SourceHash != "" && ref.SourceHash != hash {
		return Reference{}, errors.New("reference source range mismatch")
	}
	ref.SourceHash = hash
	ref.ID = ""
	b, err := json.Marshal(ref)
	if err != nil {
		return Reference{}, err
	}
	ref.ID, err = s.put("references", b)
	return ref, err
}

func (s *Store) Read(ctx context.Context, scope Scope, id string, offset, maxBytes int) (ReadResult, error) {
	if err := ctx.Err(); err != nil {
		return ReadResult{}, err
	}
	if err := scope.validate(); err != nil {
		return ReadResult{}, err
	}
	if offset < 0 || maxBytes < 1 || maxBytes > MaxReadBytes {
		return ReadResult{}, errors.New("context_read requires offset >= 0 and maxBytes between 1 and 65536")
	}
	b, err := s.read("references", id, 16<<10)
	if err != nil {
		return ReadResult{}, err
	}
	var r Reference
	if err = json.Unmarshal(b, &r); err != nil {
		return ReadResult{}, err
	}
	if r.Kind == "source" {
		scope = scope.SourceScope()
	}
	if r.Scope != scope {
		return ReadResult{}, errors.New("context reference belongs to another scope")
	}
	data, err := s.read("objects", r.ObjectHash, 64<<20)
	if err != nil {
		return ReadResult{}, err
	}
	if r.StartByte < 0 || r.EndByte < r.StartByte || r.EndByte > len(data) || digest(string(data[r.StartByte:r.EndByte])) != r.SourceHash {
		return ReadResult{}, errors.New("context reference source hash mismatch")
	}
	data = data[r.StartByte:r.EndByte]
	if offset > len(data) {
		return ReadResult{}, errors.New("context_read offset exceeds source length")
	}
	end := len(data)
	if maxBytes < end-offset {
		end = offset + maxBytes
	}
	part := data[offset:end]
	valid := utf8.Valid(part)
	content := ""
	if valid {
		content = string(part)
	}
	return ReadResult{ReferenceID: id, ObjectHash: r.ObjectHash, SourceHash: r.SourceHash, SnapshotID: r.SnapshotID, Path: r.Path, Offset: offset, NextOffset: end, TotalBytes: len(data), EOF: end == len(data), Content: content, BytesBase64: base64.StdEncoding.EncodeToString(part), ValidUTF8: valid}, nil
}

// ValidateCurrent rejects applying historical evidence to a changed live file.
// The caller must hold the workspace write lock through validation and writing.
func (w *WorkspaceRetriever) ValidateCurrent(ctx context.Context, ref Reference) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := filepath.Abs(w.Root)
	if err != nil {
		return err
	}
	if ref.Kind != "source" || ref.Scope != w.scope(root).SourceScope() {
		return errors.New("patch evidence belongs to another scope")
	}
	rel := filepath.FromSlash(ref.Path)
	if rel == "" || strings.ContainsAny(rel, ":\x00") || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return errors.New("invalid evidence path")
	}
	p := filepath.Join(root, rel)
	relative, err := filepath.Rel(root, p)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return errors.New("evidence path escapes workspace")
	}
	if hasLinkedComponent(root, p) {
		return errors.New("evidence traverses symbolic link")
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, w.limits().fileBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > w.limits().fileBytes || digest(string(data)) != ref.ObjectHash {
		return fmt.Errorf("stale source evidence for %s", ref.Path)
	}
	return nil
}
