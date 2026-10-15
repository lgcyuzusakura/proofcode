package context

import (
	"context"
	"fmt"
	"sort"
)

type Chunk struct {
	ID          string            `json:"id"`
	Path        string            `json:"path"`
	Symbol      string            `json:"symbol,omitempty"`
	StartLine   int               `json:"startLine"`
	EndLine     int               `json:"endLine"`
	Content     string            `json:"content"`
	Hash        string            `json:"hash"`
	SnapshotID  string            `json:"snapshotId,omitempty"`
	ProjectID   string            `json:"projectId,omitempty"`
	WorkspaceID string            `json:"workspaceId,omitempty"`
	BlobHash    string            `json:"blobHash,omitempty"`
	StartByte   int               `json:"startByte,omitempty"`
	EndByte     int               `json:"endByte,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type Hit struct {
	Chunk  Chunk    `json:"chunk"`
	Score  float64  `json:"score"`
	Reason []string `json:"reason"`
}

type Retriever interface {
	Name() string
	Search(context.Context, string, int) ([]Hit, error)
}

type Hybrid struct {
	Retrievers []Retriever
	RankK      float64
	MaxResults int
	// SnapshotID, when set, rejects unscoped and obsolete evidence before fusion.
	SnapshotID  string
	ProjectID   string
	WorkspaceID string
	Weights     map[string]float64
}

func (h Hybrid) Search(ctx context.Context, query string) ([]Hit, error) {
	if h.RankK <= 0 {
		h.RankK = 60
	}
	if h.MaxResults <= 0 {
		h.MaxResults = 12
	}
	type aggregate struct {
		hit     Hit
		score   float64
		reasons map[string]bool
	}
	values := map[string]*aggregate{}
	for _, retriever := range h.Retrievers {
		hits, err := retriever.Search(ctx, query, h.MaxResults*3)
		if err != nil {
			return nil, fmt.Errorf("%s retrieval: %w", retriever.Name(), err)
		}
		seen := map[string]bool{}
		weight := 1.0
		if configured, ok := h.Weights[retriever.Name()]; ok {
			weight = configured
		}
		if weight <= 0 {
			continue
		}
		for rank, hit := range hits {
			if h.SnapshotID != "" && hit.Chunk.SnapshotID != h.SnapshotID {
				continue
			}
			if h.ProjectID != "" && hit.Chunk.ProjectID != h.ProjectID {
				continue
			}
			if h.WorkspaceID != "" && hit.Chunk.WorkspaceID != h.WorkspaceID {
				continue
			}
			id := hit.Chunk.ID
			if id == "" {
				id = fmt.Sprintf("%s:%d:%d:%s", hit.Chunk.Path, hit.Chunk.StartLine, hit.Chunk.EndLine, hit.Chunk.Hash)
			}
			id = hit.Chunk.ProjectID + "\x00" + hit.Chunk.WorkspaceID + "\x00" + hit.Chunk.SnapshotID + "\x00" + id
			if seen[id] {
				continue
			}
			seen[id] = true
			item := values[id]
			if item == nil {
				item = &aggregate{hit: hit, reasons: map[string]bool{}}
				values[id] = item
			}
			item.score += weight / (h.RankK + float64(rank+1))
			item.reasons[retriever.Name()] = true
		}
	}
	result := make([]Hit, 0, len(values))
	for _, item := range values {
		item.hit.Score = item.score
		item.hit.Reason = nil
		for reason := range item.reasons {
			item.hit.Reason = append(item.hit.Reason, reason)
		}
		sort.Strings(item.hit.Reason)
		result = append(result, item.hit)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			if result[i].Chunk.Path != result[j].Chunk.Path {
				return result[i].Chunk.Path < result[j].Chunk.Path
			}
			if result[i].Chunk.StartLine != result[j].Chunk.StartLine {
				return result[i].Chunk.StartLine < result[j].Chunk.StartLine
			}
			return result[i].Chunk.ID < result[j].Chunk.ID
		}
		return result[i].Score > result[j].Score
	})
	if len(result) > h.MaxResults {
		result = result[:h.MaxResults]
	}
	return result, nil
}
