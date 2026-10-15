package context

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
)

// GenerationIndex is immutable and tied to manifest.IndexHash. Positions refer
// to the manifest's exact flattened chunk order; no live bytes enter queries.
type GenerationIndex struct {
	IndexVersion string           `json:"indexVersion"`
	ChunkCount   int              `json:"chunkCount"`
	Lexical      map[string][]int `json:"lexical"`
	SymbolTerms  map[string][]int `json:"symbolTerms"`
	SymbolExact  map[string][]int `json:"symbolExact"`
	References   map[string][]int `json:"references"`
	TestChunks   map[int]bool     `json:"testChunks"`
	Stems        map[string][]int `json:"stems"`
}

func buildGenerationIndex(files []ManifestFile) GenerationIndex {
	index := GenerationIndex{IndexVersion: IndexVersion, Lexical: map[string][]int{}, SymbolTerms: map[string][]int{}, SymbolExact: map[string][]int{}, References: map[string][]int{}, TestChunks: map[int]bool{}, Stems: map[string][]int{}}
	for _, f := range files {
		for _, c := range f.Chunks {
			i := index.ChunkCount
			index.ChunkCount++
			for _, term := range words(c.Path + "\n" + c.Content) {
				index.Lexical[term] = append(index.Lexical[term], i)
			}
			terms := map[string]bool{}
			exact := map[string]bool{}
			for _, name := range chunkSymbols(c) {
				exact[strings.ToLower(name)] = true
				for _, term := range words(name) {
					terms[term] = true
				}
			}
			for term := range terms {
				index.SymbolTerms[term] = append(index.SymbolTerms[term], i)
			}
			for name := range exact {
				index.SymbolExact[name] = append(index.SymbolExact[name], i)
			}
			for term := range chunkReferences(c) {
				index.References[term] = append(index.References[term], i)
			}
			index.Stems[fileStem(c.Path)] = append(index.Stems[fileStem(c.Path)], i)
			lower := strings.ToLower(c.Path)
			if strings.Contains(lower, "test") || strings.Contains(lower, "spec") {
				index.TestChunks[i] = true
			}
		}
	}
	return index
}

func (w *WorkspaceRetriever) generationIndex(m Manifest) (GenerationIndex, error) {
	if w.Store == nil {
		return buildGenerationIndex(m.Files), nil
	}
	b, err := w.Store.read("indexes", m.IndexHash, 64<<20)
	if err != nil {
		return GenerationIndex{}, err
	}
	var index GenerationIndex
	if err = json.Unmarshal(b, &index); err != nil {
		return index, err
	}
	count := 0
	for _, f := range m.Files {
		count += len(f.Chunks)
	}
	if index.IndexVersion != IndexVersion || index.ChunkCount != count {
		return index, errors.New("context postings use incompatible generation")
	}
	return index, nil
}

func rankPostings(ctx context.Context, chunks []Chunk, index GenerationIndex, query string) ([]Hit, []Hit, []Hit, []Hit, error) {
	if index.ChunkCount != len(chunks) {
		return nil, nil, nil, nil, errors.New("context postings and chunk generation mismatch")
	}
	lex, symbol, dependency, tests := map[int]float64{}, map[int]float64{}, map[int]float64{}, map[int]float64{}
	apply := func(target map[int]float64, ids []int, weight float64) error {
		for _, i := range ids {
			if i < 0 || i >= len(chunks) {
				return errors.New("context posting outside manifest bounds")
			}
			target[i] += weight
		}
		return nil
	}
	for _, term := range words(query) {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, nil, err
		}
		posting := index.Lexical[term]
		weight := math.Log(1 + float64(len(chunks)+1)/float64(len(posting)+1))
		if err := apply(lex, posting, weight); err != nil {
			return nil, nil, nil, nil, err
		}
		for _, i := range posting {
			if strings.Contains(strings.ToLower(chunks[i].Path), term) {
				lex[i] += 2
			}
		}
		if err := apply(symbol, index.SymbolTerms[term], 3); err != nil {
			return nil, nil, nil, nil, err
		}
		if err := apply(symbol, index.SymbolExact[term], 5); err != nil {
			return nil, nil, nil, nil, err
		}
	}
	toHits := func(scores map[int]float64) []Hit {
		hits := make([]Hit, 0, len(scores))
		for i, score := range scores {
			if score > 0 {
				hits = append(hits, Hit{Chunk: chunks[i], Score: score})
			}
		}
		return sortedHits(hits)
	}
	lexical, symbols := toHits(lex), toHits(symbol)
	seeds := append(append([]Hit{}, symbols...), lexical...)
	if len(seeds) > 8 {
		seeds = seeds[:8]
	}
	names := map[string]bool{}
	stems := map[string]bool{}
	for _, seed := range seeds {
		stems[fileStem(seed.Chunk.Path)] = true
		for _, name := range chunkSymbols(seed.Chunk) {
			for _, term := range words(name) {
				names[term] = true
			}
		}
	}
	for _, term := range words(query) {
		names[term] = true
	}
	// Static call references are parser-derived for Go and labelled heuristic for
	// other languages. No type resolution/dynamic dispatch is claimed.
	terms := make([]string, 0, len(names))
	for term := range names {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	for _, term := range terms {
		if err := apply(dependency, index.References[term], 1); err != nil {
			return nil, nil, nil, nil, err
		}
	}
	for i, score := range dependency {
		if index.TestChunks[i] {
			tests[i] = score
		}
	}
	for stem := range stems {
		for _, i := range index.Stems[stem] {
			if i < 0 || i >= len(chunks) {
				return nil, nil, nil, nil, errors.New("test posting outside manifest bounds")
			}
			if index.TestChunks[i] {
				tests[i] += 5
			}
		}
	}
	return lexical, symbols, toHits(dependency), toHits(tests), nil
}
