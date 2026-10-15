package context

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type VersionSelector struct {
	Mode         string `json:"mode"`
	SnapshotID   string `json:"snapshotId,omitempty"`
	Revision     string `json:"revision,omitempty"`
	MaxSnapshots int    `json:"maxSnapshots,omitempty"`
}
type ManifestSummary struct {
	SnapshotID   string         `json:"snapshotId"`
	BaseCommit   string         `json:"baseCommit,omitempty"`
	IndexVersion string         `json:"indexVersion"`
	Files        []ManifestPath `json:"files"`
}
type ManifestPath struct {
	Path     string `json:"path"`
	BlobHash string `json:"blobHash"`
}

func (w *WorkspaceRetriever) scope(root string) Scope {
	project, workspace := w.ProjectID, w.WorkspaceID
	if project == "" {
		project = digest("project\x00" + root)
	}
	if workspace == "" {
		workspace = digest("workspace\x00" + root)
	}
	return Scope{ProjectID: project, WorkspaceID: workspace, ConversationID: w.ConversationID, TaskID: w.TaskID, AttemptID: w.AttemptID}
}

func (w *WorkspaceRetriever) capture(ctx context.Context, root string, l workspaceLimits) (Manifest, int, int, error) {
	scope := w.scope(root).SourceScope()
	if err := scope.validate(); err != nil {
		return Manifest{}, 0, 0, err
	}
	commit := headCommit(ctx, root)
	source, hash, err := scanWorkspace(ctx, root, l)
	if err != nil {
		return Manifest{}, 0, 0, err
	}
	_, confirm, err := scanWorkspace(ctx, root, l)
	if err != nil {
		return Manifest{}, 0, 0, err
	}
	if confirm != hash || commit != headCommit(ctx, root) {
		return Manifest{}, 0, 0, errors.New("working tree changed during snapshot capture")
	}
	// Parsing cache is content keyed, not HEAD keyed. Load the previous durable
	// generation after restart, so unchanged files do not need parsing again.
	if w.files == nil {
		w.files = map[string]indexedFile{}
	}
	var previous *Manifest
	if w.Store != nil {
		stored, e := w.Store.Current(scope)
		if e != nil && !os.IsNotExist(e) {
			return Manifest{}, 0, 0, e
		}
		if e == nil && stored.ChunkLines == l.lines {
			previous = &stored
			for _, f := range stored.Files {
				key := parseKey(scope, f.Path, f.BlobHash, l.lines)
				if _, ok := w.files[key]; !ok {
					w.files[key] = indexedFile{path: f.Path, blob: f.BlobHash, chunks: f.Chunks}
				}
			}
		}
	}
	parsed, reused := 0, 0
	current := make(map[string]indexedFile, len(source))
	m := Manifest{Scope: scope, BaseCommit: commit, IndexVersion: IndexVersion, AlgorithmVersion: AlgorithmVersion, ChunkLines: l.lines, Files: []ManifestFile{}}
	for i, f := range source {
		if err := ctx.Err(); err != nil {
			return Manifest{}, 0, 0, err
		}
		key := parseKey(scope, f.path, f.blob, l.lines)
		if entry, ok := w.files[key]; ok {
			f.chunks = entry.chunks
			reused++
		} else {
			f.chunks = syntaxChunks(f.path, f.content, l.lines)
			parsed++
		}
		source[i] = f
		current[key] = f
		m.Files = append(m.Files, ManifestFile{Path: f.path, BlobHash: f.blob, Chunks: f.chunks})
	}
	w.files = current
	if previous != nil && previous.BaseCommit == commit && sameManifestSources(previous.Files, m.Files) {
		return *previous, parsed, reused, nil
	}
	if w.Store != nil {
		m, err = w.Store.Publish(scope, commit, source, l.lines)
	} else {
		b, e := json.Marshal(m)
		err = e
		m.ID = digest(string(b))
	}
	return m, parsed, reused, err
}

func sameManifestSources(a, b []ManifestFile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || a[i].BlobHash != b[i].BlobHash {
			return false
		}
	}
	return true
}

func parseKey(scope Scope, path, blob string, lines int) string {
	return digest(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", IndexVersion, scope.key(), path, blob, lines))
}

// RetrieveVersion requires explicit historical selection. Only current performs
// a full capture; history/snapshot never read live files to repair old evidence.
func (w *WorkspaceRetriever) RetrieveVersion(ctx context.Context, query, feedback string, limit int, selector VersionSelector) (ContextResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ContextResult{}, err
	}
	if len(query)+len(feedback) > 128<<10 {
		return ContextResult{}, errors.New("retrieval query exceeds 128 KiB")
	}
	if selector.Mode == "" {
		selector.Mode = "current"
	}
	if selector.Mode != "current" && selector.Mode != "snapshot" && selector.Mode != "history" {
		return ContextResult{}, errors.New("version route must be current, snapshot or history")
	}
	if selector.Mode == "current" && (selector.SnapshotID != "" || selector.Revision != "") {
		return ContextResult{}, errors.New("current route does not accept historical identifiers")
	}
	if selector.Mode == "snapshot" && selector.SnapshotID == "" {
		return ContextResult{}, errors.New("snapshot route requires snapshotId")
	}
	if selector.Mode == "history" && selector.SnapshotID != "" {
		return ContextResult{}, errors.New("history only lists generations; use snapshot with snapshotId to retrieve source")
	}
	root, err := filepath.Abs(w.Root)
	if err != nil {
		return ContextResult{}, err
	}
	root = filepath.Clean(root)
	scope := w.scope(root).SourceScope()
	l := w.limits()
	if limit <= 0 {
		limit = 8
	}
	if limit > 64 {
		limit = 64
	}
	manifests := []Manifest{}
	parsed, reused, scanned := 0, 0, 0
	if selector.Mode == "current" {
		i, e := os.Lstat(root)
		if e != nil {
			return ContextResult{}, e
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return ContextResult{}, errors.New("retrieval root must be a real directory")
		}
		m, p, r, e := w.capture(ctx, root, l)
		if e != nil {
			return ContextResult{}, e
		}
		manifests = append(manifests, m)
		parsed, reused, scanned = p, r, len(m.Files)
	} else {
		if w.Store == nil {
			return ContextResult{}, errors.New("historical retrieval requires durable context store")
		}
		if selector.SnapshotID != "" {
			m, e := w.Store.LoadManifest(scope, selector.SnapshotID)
			if e != nil {
				return ContextResult{}, e
			}
			manifests = append(manifests, m)
		} else {
			maximum := selector.MaxSnapshots
			if maximum <= 0 {
				maximum = 4
			}
			if maximum > 16 {
				return ContextResult{}, errors.New("history route allows at most 16 generations")
			}
			manifests, err = w.Store.History(scope, maximum)
			if err != nil {
				return ContextResult{}, err
			}
		}
		if selector.Revision != "" {
			if len(selector.Revision) != 40 && len(selector.Revision) != 64 {
				return ContextResult{}, errors.New("history revision must be a full Git hash")
			}
			filtered := []Manifest{}
			for _, m := range manifests {
				if m.BaseCommit == selector.Revision {
					filtered = append(filtered, m)
				}
			}
			manifests = filtered
		}
		if len(manifests) == 0 {
			return ContextResult{}, errors.New("no matching stored history generation")
		}
	}
	if selector.Mode == "history" && selector.SnapshotID == "" {
		result := ContextResult{VersionRoute: "history", AlgorithmVersion: AlgorithmVersion, IndexVersion: IndexVersion, TokenCounting: "utf8-bytes-div-4-estimate", Evidence: []Evidence{}, Manifests: []ManifestSummary{}}
		for _, m := range manifests {
			summary := ManifestSummary{SnapshotID: m.ID, BaseCommit: m.BaseCommit, IndexVersion: m.IndexVersion, Files: []ManifestPath{}}
			for _, f := range m.Files {
				summary.Files = append(summary.Files, ManifestPath{Path: f.Path, BlobHash: f.BlobHash})
			}
			result.Manifests = append(result.Manifests, summary)
			result.SnapshotIDs = append(result.SnapshotIDs, m.ID)
		}
		// Listing history is navigation, never a mixed-version prompt bundle.
		return result, nil
	}
	snapshots := make([]string, 0, len(manifests))
	for _, m := range manifests {
		snapshots = append(snapshots, m.ID)
	}
	key := digest(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%s", AlgorithmVersion, strings.Join(snapshots, ","), query, feedback, limit, l.budget, selector.Mode))
	index, err := w.generationIndex(manifests[0])
	if err != nil {
		return ContextResult{}, err
	}
	if cached, ok := w.results[key]; ok {
		// Historical objects also need checking before reusing a result. Current
		// was just recaptured above. Reference reads always independently verify.
		if selector.Mode != "current" {
			if _, err = w.manifestChunks(ctx, manifests, l, true); err != nil {
				return ContextResult{}, err
			}
		}
		result := cloneContextResult(cached)
		result.CacheHit = true
		result.FilesScanned = scanned
		result.FilesParsed = parsed
		result.FilesReused = reused
		return result, nil
	}
	chunks, err := w.manifestChunks(ctx, manifests, l, selector.Mode != "current")
	if err != nil {
		return ContextResult{}, err
	}
	text := query + "\n" + feedback
	lexical, symbol, dependency, tests, err := rankPostings(ctx, chunks, index, text)
	if err != nil {
		return ContextResult{}, err
	}
	h := Hybrid{ProjectID: scope.ProjectID, WorkspaceID: scope.WorkspaceID, MaxResults: limit * 4, Weights: map[string]float64{"lexical": 1, "symbol": 1.5, "dependency": 0.75, "test": 1.2}, Retrievers: []Retriever{rankedRoute{"lexical", lexical}, rankedRoute{"symbol", symbol}, rankedRoute{"dependency", dependency}, rankedRoute{"test", tests}}}
	hits, err := h.Search(ctx, text)
	if err != nil {
		return ContextResult{}, err
	}
	result := ContextResult{AlgorithmVersion: AlgorithmVersion, IndexVersion: IndexVersion, VersionRoute: selector.Mode, SnapshotIDs: snapshots, TokenCounting: "utf8-bytes-div-4-estimate", FilesScanned: scanned, FilesParsed: parsed, FilesReused: reused, Evidence: []Evidence{}, RetrievalRoutes: []string{"lexical", "symbol", "dependency", "test"}}
	if len(manifests) == 1 {
		result.SnapshotID = manifests[0].ID
		result.BaseCommit = manifests[0].BaseCommit
	}
	var content strings.Builder
	covered := map[string]bool{}
	paths := map[string]bool{}
	queryTerms := words(text)
	// Greedy coverage-aware selection after weighted RRF. Every route/candidate,
	// generation, and byte budget is bounded; novelty cannot select a zero hit.
	for len(hits) > 0 && len(result.Evidence) < limit {
		best, bestScore := 0, -1.0
		for i, hit := range hits {
			bonus := 1.0
			if !paths[hit.Chunk.SnapshotID+"\x00"+hit.Chunk.Path] {
				bonus += 0.25
			}
			terms := wordCounts(hit.Chunk.Content + " " + hit.Chunk.Symbol + " " + hit.Chunk.Path)
			for _, term := range queryTerms {
				if !covered[term] && terms[term] > 0 {
					bonus += 0.1
				}
			}
			if score := hit.Score * bonus; score > bestScore {
				best, bestScore = i, score
			}
		}
		hit := hits[best]
		hits = append(hits[:best], hits[best+1:]...)
		c := hit.Chunk
		refID := ""
		if w.Store != nil {
			data, e := w.Store.read("objects", c.BlobHash, l.fileBytes)
			if e != nil {
				return ContextResult{}, e
			}
			ref, e := w.Store.SaveReference(Reference{Scope: scope, Kind: "source", ObjectHash: c.BlobHash, SourceHash: c.Hash, SnapshotID: c.SnapshotID, Path: c.Path, StartByte: c.StartByte, EndByte: c.EndByte}, data)
			if e != nil {
				return ContextResult{}, e
			}
			refID = ref.ID
		}
		header := fmt.Sprintf("\n--- evidence=%s reference=%s snapshot=%s blob=%s %s:%d-%d bytes=%d:%d parser=%s routes=%s ---\n", c.ID, refID, c.SnapshotID, c.BlobHash, c.Path, c.StartLine, c.EndLine, c.StartByte, c.EndByte, c.Metadata["parser"], strings.Join(hit.Reason, ","))
		if content.Len()+len(header)+len(c.Content)+1 > l.budget {
			result.OmittedByBudget++
			continue
		}
		content.WriteString(header)
		content.WriteString(c.Content)
		content.WriteByte('\n')
		for _, term := range words(c.Content + " " + c.Symbol + " " + c.Path) {
			covered[term] = true
		}
		paths[c.SnapshotID+"\x00"+c.Path] = true
		result.Evidence = append(result.Evidence, Evidence{ID: c.ID, ReferenceID: refID, SnapshotID: c.SnapshotID, ProjectID: c.ProjectID, WorkspaceID: c.WorkspaceID, Path: c.Path, Symbol: c.Symbol, BlobHash: c.BlobHash, SourceHash: c.Hash, StartByte: c.StartByte, EndByte: c.EndByte, StartLine: c.StartLine, EndLine: c.EndLine, Content: c.Content, Score: hit.Score, Reasons: append([]string(nil), hit.Reason...), Parser: c.Metadata["parser"], RelationBasis: c.Metadata["relationBasis"]})
	}
	result.OmittedByBudget += len(hits)
	result.Content = content.String()
	result.EstimatedTokens = (len(result.Content) + 3) / 4
	if w.results == nil || len(w.results) >= 32 {
		w.results = map[string]ContextResult{}
	}
	w.results[key] = cloneContextResult(result)
	return result, nil
}

func (w *WorkspaceRetriever) manifestChunks(ctx context.Context, manifests []Manifest, l workspaceLimits, verify bool) ([]Chunk, error) {
	chunks := []Chunk{}
	total := int64(0)
	for _, m := range manifests {
		for _, f := range m.Files {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			var data []byte
			var err error
			if verify {
				data, err = w.Store.read("objects", f.BlobHash, l.fileBytes)
				if err != nil {
					return nil, err
				}
			}
			for _, original := range f.Chunks {
				c := original
				total += int64(len(c.Content))
				if total > l.bytes {
					return nil, errors.New("selected history exceeds total retrieval bytes; select fewer snapshots")
				}
				if verify && (c.StartByte < 0 || c.EndByte < c.StartByte || c.EndByte > len(data) || c.Content != string(data[c.StartByte:c.EndByte]) || c.Hash != digest(c.Content)) {
					return nil, errors.New("historical chunk source mismatch")
				}
				c.SnapshotID, c.ProjectID, c.WorkspaceID, c.BlobHash = m.ID, m.Scope.ProjectID, m.Scope.WorkspaceID, f.BlobHash
				c.ID = digest(fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", m.ID, f.Path, c.StartByte, c.EndByte, c.Hash))
				chunks = append(chunks, c)
			}
		}
	}
	return chunks, nil
}
