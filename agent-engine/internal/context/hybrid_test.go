package context

import (
	"context"
	"testing"
)

type staticRetriever struct {
	name string
	hits []Hit
}

func (s staticRetriever) Name() string                                       { return s.name }
func (s staticRetriever) Search(context.Context, string, int) ([]Hit, error) { return s.hits, nil }

func TestHybridRewardsAgreement(t *testing.T) {
	common := Hit{Chunk: Chunk{ID: "common", Path: "common.go"}}
	hybrid := Hybrid{RankK: 60, MaxResults: 3, Retrievers: []Retriever{staticRetriever{"lexical", []Hit{{Chunk: Chunk{ID: "lex", Path: "lex.go"}}, common}}, staticRetriever{"vector", []Hit{common, {Chunk: Chunk{ID: "vec", Path: "vec.go"}}}}}}
	hits, err := hybrid.Search(context.Background(), "query")
	if err != nil {
		t.Fatal(err)
	}
	if hits[0].Chunk.ID != "common" {
		t.Fatalf("agreement should rank first: %+v", hits)
	}
	if len(hits[0].Reason) != 2 {
		t.Fatalf("expected two reasons: %+v", hits[0])
	}
}
