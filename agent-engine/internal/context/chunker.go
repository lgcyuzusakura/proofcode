package context

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type LineChunker struct {
	Lines        int
	Overlap      int
	MaxFileBytes int64
}

func (c LineChunker) ChunkFile(root, path string) ([]Chunk, error) {
	if c.Lines <= 0 {
		c.Lines = 120
	}
	if c.Overlap < 0 || c.Overlap >= c.Lines {
		c.Overlap = 20
	}
	if c.MaxFileBytes <= 0 {
		c.MaxFileBytes = 2 << 20
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > c.MaxFileBytes {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	lines := []string{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), int(c.MaxFileBytes))
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	step := c.Lines - c.Overlap
	chunks := []Chunk{}
	for start := 0; start < len(lines); start += step {
		end := start + c.Lines
		if end > len(lines) {
			end = len(lines)
		}
		content := strings.Join(lines[start:end], "\n")
		sum := sha256.Sum256([]byte(rel + "\x00" + content))
		hash := hex.EncodeToString(sum[:])
		chunks = append(chunks, Chunk{ID: hash, Path: rel, StartLine: start + 1, EndLine: end, Content: content, Hash: hash})
		if end == len(lines) {
			break
		}
	}
	return chunks, nil
}

func FormatForPrompt(hits []Hit, byteBudget int) string {
	if byteBudget <= 0 {
		byteBudget = 64 << 10
	}
	var out strings.Builder
	for _, hit := range hits {
		header := fmt.Sprintf("\n--- %s:%d-%d [%s] ---\n", hit.Chunk.Path, hit.Chunk.StartLine, hit.Chunk.EndLine, strings.Join(hit.Reason, ","))
		if out.Len()+len(header)+len(hit.Chunk.Content) > byteBudget {
			break
		}
		out.WriteString(header)
		out.WriteString(hit.Chunk.Content)
		out.WriteByte('\n')
	}
	return out.String()
}
