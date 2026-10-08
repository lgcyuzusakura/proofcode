package context

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const workspaceIndexVersion = "worktree-evidence-v1"

// Evidence locates exact, original bytes in one immutable worktree snapshot.
// Line numbers are display coordinates, never a substitute for BlobHash.
type Evidence struct {
	ID          string   `json:"id"`
	SnapshotID  string   `json:"snapshotId"`
	ProjectID   string   `json:"projectId"`
	WorkspaceID string   `json:"workspaceId"`
	Path        string   `json:"path"`
	Symbol      string   `json:"symbol,omitempty"`
	BlobHash    string   `json:"blobHash"`
	SourceHash  string   `json:"sourceHash"`
	StartByte   int      `json:"startByte"`
	EndByte     int      `json:"endByte"`
	StartLine   int      `json:"startLine"`
	EndLine     int      `json:"endLine"`
	Content     string   `json:"content"`
	Score       float64  `json:"score"`
	Reasons     []string `json:"reasons"`
}

type ContextResult struct {
	Content             string     `json:"content"`
	SnapshotID          string     `json:"snapshotId"`
	BaseCommit          string     `json:"baseCommit,omitempty"`
	Evidence            []Evidence `json:"evidence"`
	CacheHit            bool       `json:"cacheHit"`
	FilesScanned        int        `json:"filesScanned"`
	FilesReused         int        `json:"filesReused"`
	FilesParsed         int        `json:"filesParsed"`
	EstimatedTokens     int        `json:"estimatedTokens"`
	RetrievalRoutes     []string   `json:"retrievalRoutes"`
	EmbeddingConfigured bool       `json:"embeddingConfigured"`
	OmittedByBudget     int        `json:"omittedByBudget"`
}

// WorkspaceRetriever reads the real working tree on every retrieval. Cached
// parsing is keyed by file bytes; query results are keyed by the full snapshot.
// It deliberately implements deterministic routes, not a pretend vector index.
type WorkspaceRetriever struct {
	Root         string
	ProjectID    string
	WorkspaceID  string
	TaskID       string
	AttemptID    string
	MaxFiles     int
	MaxBytes     int64
	MaxFileBytes int64
	ByteBudget   int
	ChunkLines   int
	mu           sync.Mutex
	files        map[string]indexedFile
	results      map[string]ContextResult
}

type indexedFile struct {
	path    string
	blob    string
	content string
	chunks  []Chunk
}

type workspaceLimits struct {
	files            int
	bytes, fileBytes int64
	budget, lines    int
}

func (w *WorkspaceRetriever) limits() workspaceLimits {
	l := workspaceLimits{w.MaxFiles, w.MaxBytes, w.MaxFileBytes, w.ByteBudget, w.ChunkLines}
	if l.files <= 0 {
		l.files = 4000
	}
	if l.bytes <= 0 {
		l.bytes = 32 << 20
	}
	if l.fileBytes <= 0 {
		l.fileBytes = 512 << 10
	}
	if l.budget <= 0 {
		l.budget = 48 << 10
	}
	if l.lines <= 0 {
		l.lines = 80
	}
	return l
}

func digest(value string) string { h := sha256.Sum256([]byte(value)); return hex.EncodeToString(h[:]) }

// Retrieve fails closed if the working tree changes during snapshot capture or
// its configured bounds are exceeded. Re-run after concurrent writes settle.
func (w *WorkspaceRetriever) Retrieve(ctx context.Context, query, feedback string, limit int) (ContextResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ContextResult{}, err
	}
	if len(query)+len(feedback) > 128<<10 {
		return ContextResult{}, errors.New("retrieval query exceeds 128 KiB")
	}
	root, err := filepath.Abs(w.Root)
	if err != nil {
		return ContextResult{}, err
	}
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil {
		return ContextResult{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ContextResult{}, errors.New("retrieval root must be a real directory")
	}
	l := w.limits()
	project, workspace := w.ProjectID, w.WorkspaceID
	if project == "" {
		project = digest("project\x00" + root)
	}
	if workspace == "" {
		workspace = digest("workspace\x00" + root)
	}
	if limit <= 0 {
		limit = 8
	}
	if limit > 64 {
		limit = 64
	}
	commit := headCommit(ctx, root)
	source, manifest, err := scanWorkspace(ctx, root, l)
	if err != nil {
		return ContextResult{}, err
	}
	_, confirm, err := scanWorkspace(ctx, root, l)
	if err != nil {
		return ContextResult{}, err
	}
	if manifest != confirm || commit != headCommit(ctx, root) {
		return ContextResult{}, errors.New("working tree changed during snapshot capture")
	}
	scope := strings.Join([]string{workspaceIndexVersion, root, project, workspace, w.TaskID, w.AttemptID, commit}, "\x00")
	snapshot := digest(scope + "\x00" + manifest)
	key := digest(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%d", snapshot, query, feedback, limit, l.budget, l.lines))
	if cached, ok := w.results[key]; ok {
		result := cloneContextResult(cached)
		result.CacheHit = true
		result.FilesScanned = len(source)
		result.FilesParsed = 0
		result.FilesReused = len(source)
		return result, nil
	}
	current := make(map[string]indexedFile, len(source))
	chunks := []Chunk{}
	parsed, reused := 0, 0
	for _, file := range source {
		if err := ctx.Err(); err != nil {
			return ContextResult{}, err
		}
		cacheKey := fmt.Sprintf("%s\x00%s\x00%s\x00%d", scope, file.path, file.blob, l.lines)
		entry, ok := w.files[cacheKey]
		if ok {
			reused++
		} else {
			parsed++
			entry = file
			entry.chunks = exactChunks(file.path, file.content, l.lines)
		}
		current[cacheKey] = entry
		for _, original := range entry.chunks {
			chunk := original
			chunk.SnapshotID, chunk.ProjectID, chunk.WorkspaceID, chunk.BlobHash = snapshot, project, workspace, file.blob
			chunk.ID = digest(fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", snapshot, file.path, chunk.StartByte, chunk.EndByte, chunk.Hash))
			chunks = append(chunks, chunk)
		}
	}
	w.files = current // Deleted/changed occurrences and previous scopes leave the cache.
	searchText := query + "\n" + feedback
	lexical := rankLexical(chunks, searchText)
	symbol := rankSymbols(chunks, searchText)
	dependency, tests := rankRelationships(chunks, append(append([]Hit{}, symbol...), lexical...), searchText)
	hybrid := Hybrid{SnapshotID: snapshot, ProjectID: project, WorkspaceID: workspace, MaxResults: limit,
		Retrievers: []Retriever{rankedRoute{"lexical", lexical}, rankedRoute{"symbol", symbol}, rankedRoute{"dependency", dependency}, rankedRoute{"test", tests}}}
	hits, err := hybrid.Search(ctx, searchText)
	if err != nil {
		return ContextResult{}, err
	}
	result := ContextResult{SnapshotID: snapshot, BaseCommit: commit, FilesScanned: len(source), FilesParsed: parsed, FilesReused: reused,
		RetrievalRoutes: []string{"lexical", "symbol", "dependency", "test"}, Evidence: []Evidence{}}
	var content strings.Builder
	for _, hit := range hits {
		c := hit.Chunk
		header := fmt.Sprintf("\n--- evidence=%s snapshot=%s blob=%s %s:%d-%d bytes=%d:%d routes=%s ---\n", c.ID, snapshot, c.BlobHash, c.Path, c.StartLine, c.EndLine, c.StartByte, c.EndByte, strings.Join(hit.Reason, ","))
		if content.Len()+len(header)+len(c.Content)+1 > l.budget {
			result.OmittedByBudget++
			continue
		}
		content.WriteString(header)
		content.WriteString(c.Content)
		content.WriteByte('\n')
		result.Evidence = append(result.Evidence, Evidence{ID: c.ID, SnapshotID: snapshot, ProjectID: project, WorkspaceID: workspace, Path: c.Path, Symbol: c.Symbol,
			BlobHash: c.BlobHash, SourceHash: c.Hash, StartByte: c.StartByte, EndByte: c.EndByte, StartLine: c.StartLine, EndLine: c.EndLine, Content: c.Content, Score: hit.Score, Reasons: append([]string(nil), hit.Reason...)})
	}
	result.Content = content.String()
	result.EstimatedTokens = (len(result.Content) + 3) / 4
	if w.results == nil {
		w.results = map[string]ContextResult{}
	}
	// Retain only bounded, exact-snapshot entries. A caller cannot poison the cache.
	for k, r := range w.results {
		if r.SnapshotID != snapshot {
			delete(w.results, k)
		}
	}
	if len(w.results) >= 32 {
		w.results = map[string]ContextResult{}
	}
	w.results[key] = cloneContextResult(result)
	return result, nil
}

func cloneContextResult(r ContextResult) ContextResult {
	r.Evidence = append([]Evidence(nil), r.Evidence...)
	for i := range r.Evidence {
		r.Evidence[i].Reasons = append([]string(nil), r.Evidence[i].Reasons...)
	}
	r.RetrievalRoutes = append([]string(nil), r.RetrievalRoutes...)
	return r
}

// Check the actual path entries rather than EvalSymlinks string equality:
// Windows short (8.3) path aliases legitimately resolve to different strings.
func hasLinkedComponent(root, path string) bool {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return true
		}
		if current == root {
			return false
		}
		if filepath.Dir(current) == current {
			return true
		}
	}
}

func headCommit(ctx context.Context, root string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--verify", "HEAD")
	if output, err := cmd.Output(); err == nil {
		return strings.TrimSpace(string(output))
	}
	return ""
}

var excludedDirs = map[string]bool{".git": true, ".hg": true, ".svn": true, ".ssh": true, ".aws": true, ".azure": true, ".gcloud": true, ".codex": true, ".claude": true, ".proofcode": true, ".cache": true, ".idea": true, ".tools": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true, "__pycache__": true, "dist": true, "build": true, "target": true, "coverage": true, "secrets": true, "credentials": true}
var sourceExtensions = map[string]bool{".go": true, ".rs": true, ".py": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".java": true, ".kt": true, ".cs": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true, ".rb": true, ".php": true, ".swift": true, ".scala": true, ".sh": true, ".ps1": true, ".sql": true, ".proto": true, ".graphql": true, ".html": true, ".css": true, ".scss": true, ".vue": true, ".svelte": true, ".md": true, ".txt": true, ".yaml": true, ".yml": true, ".toml": true, ".json": true, ".xml": true, ".gradle": true, ".properties": true, ".mod": true, ".sum": true}
var secretAssignment = regexp.MustCompile(`(?im)(?:api[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret|password)\s*["']?\s*[:=]\s*["']([^"'\s]{8,})["']`)

func safeSourceName(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, ".env") || strings.Contains(lower, "credential") || strings.Contains(lower, "secret") || strings.Contains(lower, "private_key") || strings.HasPrefix(lower, "id_rsa") || lower == "auth.json" || lower == ".npmrc" || lower == ".netrc" {
		return false
	}
	if lower == "dockerfile" || lower == "makefile" || lower == "justfile" {
		return true
	}
	return sourceExtensions[strings.ToLower(filepath.Ext(name))]
}

func hasSecret(content string) bool {
	if strings.Contains(content, "-----BEGIN PRIVATE KEY-----") || strings.Contains(content, "-----BEGIN RSA PRIVATE KEY-----") || strings.Contains(content, "-----BEGIN OPENSSH PRIVATE KEY-----") {
		return true
	}
	for _, match := range secretAssignment.FindAllStringSubmatch(content, -1) {
		v := strings.ToLower(match[1])
		if strings.Contains(v, "${") || strings.Contains(v, "example") || strings.Contains(v, "placeholder") || strings.Contains(v, "changeme") || strings.Contains(v, "your-") || strings.Contains(v, "test") || strings.Contains(v, "dummy") {
			continue
		}
		return true
	}
	return false
}

func scanWorkspace(ctx context.Context, root string, l workspaceLimits) ([]indexedFile, string, error) {
	files := []indexedFile{}
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if excludedDirs[strings.ToLower(entry.Name())] {
				return filepath.SkipDir
			}
			return nil
		}
		if !safeSourceName(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > l.fileBytes {
			return fmt.Errorf("source file exceeds retrieval byte limit: %s", entry.Name())
		}
		if len(files) >= l.files {
			return errors.New("workspace exceeds retrieval file limit")
		}
		if hasLinkedComponent(root, path) {
			return errors.New("source traverses symbolic link")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		opened, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		if !os.SameFile(info, opened) || hasLinkedComponent(root, path) {
			f.Close()
			return errors.New("source file changed or traverses symbolic link")
		}
		data, err := io.ReadAll(io.LimitReader(f, l.fileBytes+1))
		f.Close()
		if err != nil {
			return err
		}
		if int64(len(data)) > l.fileBytes {
			return errors.New("source file grew beyond retrieval limit")
		}
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) || hasSecret(string(data)) {
			return nil
		}
		total += int64(len(data))
		if total > l.bytes {
			return errors.New("workspace exceeds retrieval total byte limit")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return errors.New("source outside retrieval root")
		}
		files = append(files, indexedFile{path: filepath.ToSlash(rel), blob: digest(string(data)), content: string(data)})
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	var manifest strings.Builder
	for _, f := range files {
		manifest.WriteString(f.path)
		manifest.WriteByte(0)
		manifest.WriteString(f.blob)
		manifest.WriteByte('\n')
	}
	return files, manifest.String(), nil
}

func exactChunks(path, content string, linesPerChunk int) []Chunk {
	if content == "" {
		return nil
	}
	starts := []int{0}
	for i, b := range []byte(content) {
		if b == '\n' && i+1 < len(content) {
			starts = append(starts, i+1)
		}
	}
	chunks := []Chunk{}
	for first := 0; first < len(starts); first += linesPerChunk {
		last := first + linesPerChunk
		if last > len(starts) {
			last = len(starts)
		}
		end := len(content)
		if last < len(starts) {
			end = starts[last]
		}
		text := content[starts[first]:end]
		names := definitionNames(text)
		chunks = append(chunks, Chunk{Path: path, StartByte: starts[first], EndByte: end, StartLine: first + 1, EndLine: last, Content: text, Hash: digest(text), Symbol: strings.Join(names, ",")})
	}
	return chunks
}

var definitionPattern = regexp.MustCompile(`(?m)\b(?:func\s+(?:\([^\n)]*\)\s*)?|(?:async\s+)?def\s+|function\s+|class\s+|interface\s+|struct\s+|enum\s+|type\s+|fn\s+|const\s+|let\s+)([A-Za-z_][A-Za-z0-9_]*)`)

func definitionNames(content string) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, m := range definitionPattern.FindAllStringSubmatch(content, -1) {
		if !seen[m[1]] {
			names = append(names, m[1])
			seen[m[1]] = true
		}
	}
	sort.Strings(names)
	return names
}

var stopwords = map[string]bool{"the": true, "and": true, "for": true, "with": true, "this": true, "that": true, "from": true, "into": true, "return": true, "func": true, "type": true, "import": true, "const": true, "string": true, "error": true, "true": true, "false": true, "nil": true, "fix": true, "please": true, "code": true, "test": true}

func words(s string) []string {
	raw := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	out := []string{}
	seen := map[string]bool{}
	add := func(v string) {
		v = strings.ToLower(v)
		if len(v) > 1 && !stopwords[v] && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, v := range raw {
		add(v)
		var part strings.Builder
		var previous rune
		for _, r := range v {
			if r == '_' || (unicode.IsUpper(r) && unicode.IsLower(previous)) {
				add(part.String())
				part.Reset()
			}
			if r != '_' {
				part.WriteRune(r)
			}
			previous = r
		}
		add(part.String())
	}
	return out
}
func wordCounts(s string) map[string]int {
	counts := map[string]int{}
	for _, v := range words(s) {
		counts[v]++
	}
	return counts
}

type rankedRoute struct {
	name string
	hits []Hit
}

func (r rankedRoute) Name() string { return r.name }
func (r rankedRoute) Search(ctx context.Context, _ string, n int) ([]Hit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(r.hits) > n {
		return r.hits[:n], nil
	}
	return r.hits, nil
}
func sortedHits(hits []Hit) []Hit {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].Chunk.Path != hits[j].Chunk.Path {
			return hits[i].Chunk.Path < hits[j].Chunk.Path
		}
		return hits[i].Chunk.StartByte < hits[j].Chunk.StartByte
	})
	return hits
}

func rankLexical(chunks []Chunk, query string) []Hit {
	tokens := words(query)
	counts := make([]map[string]int, len(chunks))
	df := map[string]int{}
	for i, c := range chunks {
		counts[i] = wordCounts(c.Path + "\n" + c.Content)
		for term := range counts[i] {
			df[term]++
		}
	}
	hits := []Hit{}
	for i, c := range chunks {
		score := 0.0
		for _, term := range tokens {
			if counts[i][term] > 0 {
				score += math.Log(1 + float64(len(chunks)+1)/float64(df[term]+1))
				if strings.Contains(strings.ToLower(c.Path), term) {
					score += 2
				}
			}
		}
		if score > 0 {
			hits = append(hits, Hit{Chunk: c, Score: score})
		}
	}
	return sortedHits(hits)
}
func rankSymbols(chunks []Chunk, query string) []Hit {
	tokens := wordCounts(query)
	hits := []Hit{}
	for _, c := range chunks {
		score := 0.0
		for _, name := range definitionNames(c.Content) {
			for _, term := range words(name) {
				if tokens[term] > 0 {
					score += 3
				}
			}
			if tokens[strings.ToLower(name)] > 0 {
				score += 5
			}
		}
		if score > 0 {
			hits = append(hits, Hit{Chunk: c, Score: score})
		}
	}
	return sortedHits(hits)
}
func rankRelationships(chunks []Chunk, seeds []Hit, query string) ([]Hit, []Hit) {
	seedPaths := map[string]bool{}
	names := map[string]bool{}
	stems := map[string]bool{}
	for i, h := range seeds {
		if i >= 8 {
			break
		}
		seedPaths[h.Chunk.Path] = true
		stems[fileStem(h.Chunk.Path)] = true
		for _, name := range definitionNames(h.Chunk.Content) {
			names[strings.ToLower(name)] = true
		}
	}
	for _, q := range words(query) {
		names[q] = true
	}
	dependencies, tests := []Hit{}, []Hit{}
	for _, c := range chunks {
		// Removing declaration spans distinguishes references from a definition
		// merely repeating its own name. This remains a heuristic, not an AST
		// call graph; a lexical seed can itself contain a useful dependency.
		references := definitionPattern.ReplaceAllString(c.Content, "")
		terms := wordCounts(references)
		score := 0.0
		for name := range names {
			if terms[name] > 0 {
				score++
			}
		}
		for path := range seedPaths {
			stem := fileStem(path)
			if path != c.Path && stem != "" && strings.Contains(strings.ToLower(references), stem) {
				score++
			}
		}
		if score > 0 {
			dependencies = append(dependencies, Hit{Chunk: c, Score: score})
		}
		lower := strings.ToLower(c.Path)
		isTest := strings.Contains(lower, "test") || strings.Contains(lower, "spec")
		if isTest {
			if stems[fileStem(c.Path)] {
				score += 5
			}
			if score > 0 {
				tests = append(tests, Hit{Chunk: c, Score: score})
			}
		}
	}
	return sortedHits(dependencies), sortedHits(tests)
}
func fileStem(path string) string {
	stem := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	for _, suffix := range []string{"_test", ".test", ".spec", "test", "spec"} {
		stem = strings.TrimSuffix(stem, suffix)
	}
	return stem
}
