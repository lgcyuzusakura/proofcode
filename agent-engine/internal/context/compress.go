package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

type CompressionReport struct {
	SourceHash            string              `json:"sourceHash"`
	BeforeEstimatedTokens int                 `json:"beforeEstimatedTokens"`
	AfterEstimatedTokens  int                 `json:"afterEstimatedTokens"`
	SourceReferences      []string            `json:"sourceReferences"`
	Lossless              bool                `json:"lossless"`
	DeduplicatedMessages  int                 `json:"deduplicatedMessages"`
	CompressedMessages    int                 `json:"compressedMessages"`
	AlgorithmVersion      string              `json:"algorithmVersion"`
	TokenCounting         string              `json:"tokenCounting"`
	LedgerHash            string              `json:"ledgerHash,omitempty"`
	ViewHash              string              `json:"viewHash,omitempty"`
	Changes               []CompressionChange `json:"changes,omitempty"`
	Budget                BudgetReport        `json:"budget"`
	Error                 string              `json:"error,omitempty"`
	TranscriptReferenceID string              `json:"transcriptReferenceId,omitempty"`
}

// CompressMessages creates a model-only view. It never mutates durable input.
// Only repeated exact tool outputs explicitly marked successful are removed;
// one full copy remains and later copies become verifiable references.
func CompressMessages(messages []model.Message) ([]model.Message, CompressionReport) {
	original, marshalErr := json.Marshal(messages)
	report := CompressionReport{Lossless: true}
	if marshalErr == nil {
		h := sha256.Sum256(original)
		report.SourceHash = hex.EncodeToString(h[:])
	}
	view := append([]model.Message(nil), messages...)
	for i := range view {
		view[i].ToolCalls = append([]model.ToolCall(nil), messages[i].ToolCalls...)
		for j := range view[i].ToolCalls {
			view[i].ToolCalls[j].Arguments = append(json.RawMessage(nil), messages[i].ToolCalls[j].Arguments...)
		}
	}
	seen := map[string]bool{}
	for _, m := range messages {
		report.BeforeEstimatedTokens += tokenEstimate(m.Content)
		encoded, err := json.Marshal(m.ToolCalls)
		if err == nil {
			report.BeforeEstimatedTokens += tokenEstimate(string(encoded))
		}
	}
	for i := len(view) - 1; i >= 0; i-- {
		m := view[i]
		if marshalErr != nil || m.Role != model.RoleTool || m.Content == "" || !isSuccessfulOutput(m.Content) {
			continue
		}
		contentHash := sha256.Sum256([]byte(m.Content))
		hash := hex.EncodeToString(contentHash[:])
		key := hash
		if seen[key] {
			replacement := fmt.Sprintf("[duplicate successful log; full output retained later; sha256=%s; sourceHash=%s]", hash, report.SourceHash)
			if len(replacement) >= len(m.Content) {
				continue
			}
			view[i].Content = replacement
			report.DeduplicatedMessages++
			report.SourceReferences = append(report.SourceReferences, fmt.Sprintf("%s/message/%d/tool/%s/sha256/%s", report.SourceHash, i, m.ToolCallID, hash))
		} else {
			seen[key] = true
		}
	}
	for _, m := range view {
		report.AfterEstimatedTokens += tokenEstimate(m.Content)
		encoded, err := json.Marshal(m.ToolCalls)
		if err == nil {
			report.AfterEstimatedTokens += tokenEstimate(string(encoded))
		}
	}
	return view, report
}

func tokenEstimate(s string) int {
	if s == "" {
		return 0
	}
	return (len([]byte(s)) + 3) / 4
}

func isSuccessfulOutput(content string) bool {
	var payload struct {
		IsError  bool           `json:"isError"`
		Metadata map[string]any `json:"metadata"`
		Content  string         `json:"content"`
	}
	if json.Unmarshal([]byte(content), &payload) != nil || payload.IsError {
		return false
	}
	if payload.Metadata == nil {
		return false
	}
	// Read-file results, arbitrary JSON/SQL/code and unknown tool formats stay exact.
	if _, ok := payload.Metadata["exitCode"]; !ok {
		return false
	}
	if !plainLog(payload.Content) {
		return false
	}
	if value, ok := payload.Metadata["exitCode"]; ok {
		switch n := value.(type) {
		case float64:
			if n != 0 {
				return false
			}
		case int:
			if n != 0 {
				return false
			}
		default:
			return false
		}
	}
	if value, ok := payload.Metadata["timedOut"]; ok {
		if timed, ok := value.(bool); !ok || timed {
			return false
		}
	}
	if value, ok := payload.Metadata["truncated"]; ok {
		if truncated, ok := value.(bool); !ok || truncated {
			return false
		}
	}
	if value, ok := payload.Metadata["outputTruncated"]; ok {
		if truncated, ok := value.(bool); !ok || truncated {
			return false
		}
	}
	if strings.Contains(strings.ToLower(payload.Content), "failed") || strings.Contains(strings.ToLower(payload.Content), "error:") {
		return false
	}
	return true
}

func plainLog(content string) bool {
	if json.Valid([]byte(strings.TrimSpace(content))) {
		return false
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		allowed := false
		for _, prefix := range []string{"INFO ", "INFO:", "[INFO]", "DEBUG ", "DEBUG:", "[DEBUG]", "PASS", "ok \t", "ok  ", "=== RUN ", "--- PASS:", "?   ", "collected ", "=================", "Progress "} {
			if strings.HasPrefix(line, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
		// Log lines containing executable or structured payloads remain intact.
		lower := strings.ToLower(line)
		lower = strings.TrimPrefix(strings.TrimPrefix(lower, "[info]"), "[debug]")
		for _, token := range []string{"{", "}", "[", "]", "(", ")", "=", "$", "<", ">", "&", "|", "select ", "insert ", "update ", "delete ", "create ", "drop ", "alter ", "truncate ", "grant ", "revoke ", "merge ", "begin ", "commit;", "rollback;", "with ", "func ", "def ", "class ", "function ", "package ", "import ", "return ", "const ", "let ", "var ", "fn ", "interface ", "struct ", "enum ", "#include", "using ", "echo ", "export ", "print ", "```", "=>", "<?", ";", "\"", "'", "`"} {
			if strings.Contains(lower, token) {
				return false
			}
		}
	}
	return strings.TrimSpace(content) != ""
}
