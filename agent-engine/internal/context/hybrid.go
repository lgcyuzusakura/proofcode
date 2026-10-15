package context

import (
	"context"
	"fmt"
	"sort"
)

type Chunk struct {
	ID        string            `json:"id"`
	Path      string            `json:"path"`
	Symbol    string            `json:"symbol,omitempty"`
	StartLine int               `json:"startLine"`
	EndLine   int               `json:"endLine"`
	Content   string            `json:"content"`
	Hash      string            `json:"hash"`
	Metadata  map[string]string `json:"metadata,omitempty"`
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
		for rank, hit := range hits {
			id := hit.Chunk.ID
			if id == "" {
				id = fmt.Sprintf("%s:%d:%d", hit.Chunk.Path, hit.Chunk.StartLine, hit.Chunk.EndLine)
			}
			item := values[id]
			if item == nil {
				item = &aggregate{hit: hit, reasons: map[string]bool{}}
				values[id] = item
			}
			item.score += 1.0 / (h.RankK + float64(rank+1))
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
			return result[i].Chunk.Path < result[j].Chunk.Path
		}
		return result[i].Score > result[j].Score
	})
	if len(result) > h.MaxResults {
		result = result[:h.MaxResults]
	}
	return result, nil
}
