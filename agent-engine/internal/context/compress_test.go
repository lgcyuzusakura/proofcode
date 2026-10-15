package context

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

func commandLog(t *testing.T, content string, failed bool) string {
	t.Helper()
	exit := 0
	if failed {
		exit = 1
	}
	encoded, err := json.Marshal(map[string]any{"content": content, "isError": failed, "metadata": map[string]any{"program": "go", "exitCode": exit}})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestCompressionKeepsLatestExactEvidenceAndDurableTranscript(t *testing.T) {
	log := commandLog(t, strings.Repeat("INFO Completed deterministic verification step\n", 50), false)
	failure := commandLog(t, strings.Repeat("INFO This output failed due to Regression\n", 30), true)
	calls := func(id string) model.Message {
		return model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: id, Name: "run_command", Arguments: json.RawMessage(`{"program":"go","args":["test","./..."]}`)}}}
	}
	messages := []model.Message{{Role: model.RoleSystem, Content: "preserve protocol"}, {Role: model.RoleUser, Content: "fix Regression"}, calls("a"), {Role: model.RoleTool, ToolCallID: "a", Content: log}, calls("b"), {Role: model.RoleTool, ToolCallID: "b", Content: log}, calls("c"), {Role: model.RoleTool, ToolCallID: "c", Content: failure}}
	before, _ := json.Marshal(messages)
	view, report := CompressMessages(messages)
	after, _ := json.Marshal(messages)
	if string(before) != string(after) {
		t.Fatal("durable input mutated")
	}
	if view[3].Content == log || view[5].Content != log || view[7].Content != failure {
		t.Fatalf("wrong evidence compressed: %+v", view)
	}
	if report.DeduplicatedMessages != 1 || !report.Lossless || report.SourceHash == "" || report.BeforeEstimatedTokens <= report.AfterEstimatedTokens || len(report.SourceReferences) != 1 {
		t.Fatalf("invalid report: %+v", report)
	}
	if len(view) != len(messages) || view[3].ToolCallID != "a" || !reflect.DeepEqual(view[2].ToolCalls, messages[2].ToolCalls) {
		t.Fatal("tool protocol changed")
	}
	view[2].ToolCalls[0].Arguments[2] = 'X'
	if string(messages[2].ToolCalls[0].Arguments) != `{"program":"go","args":["test","./..."]}` {
		t.Fatal("argument slices alias durable input")
	}
}

func TestCompressionProtectsJSONSQLCodeAndUnknownOutputs(t *testing.T) {
	contents := []string{`{"records":[{"id":1}]}`, "SELECT customer_id FROM customers;", "package p\nfunc Important() {}", "INFO SELECT id FROM users", "INFO {\"id\":1}", "def handler():\n    return 1", "[INFO] class Service {}", "INFO DROP TABLE accounts", "INFO ALTER TABLE accounts ADD value INT", "INFO const important = 42", "INFO echo $SECRET;", "INFO [1, 2, 3];"}
	for _, body := range contents {
		content := commandLog(t, strings.Repeat(body+"\n", 30), false)
		messages := []model.Message{{Role: model.RoleTool, Content: content}, {Role: model.RoleTool, Content: content}}
		view, report := CompressMessages(messages)
		if !reflect.DeepEqual(view, messages) || report.DeduplicatedMessages != 0 {
			t.Fatalf("protected content compressed: %s %+v", body, report)
		}
	}
	readFile := `{"content":"INFO source file begins here","metadata":{"path":"main.go"}}`
	view, r := CompressMessages([]model.Message{{Role: model.RoleTool, Content: readFile}, {Role: model.RoleTool, Content: readFile}})
	if view[0].Content != readFile || r.DeduplicatedMessages != 0 {
		t.Fatal("read_file source compressed")
	}
}

func TestCompressionProtectsFailureTimeoutAndTruncatedLogs(t *testing.T) {
	for _, metadata := range []map[string]any{{"exitCode": 1}, {"exitCode": 0, "timedOut": true}, {"exitCode": 0, "outputTruncated": true}, {"exitCode": 0, "truncated": true}} {
		b, _ := json.Marshal(map[string]any{"content": strings.Repeat("INFO verification done\n", 100), "metadata": metadata})
		messages := []model.Message{{Role: model.RoleTool, Content: string(b)}, {Role: model.RoleTool, Content: string(b)}}
		view, r := CompressMessages(messages)
		if !reflect.DeepEqual(view, messages) || r.DeduplicatedMessages != 0 {
			t.Fatalf("failure/truncation compressed: %+v", metadata)
		}
	}
}

func TestCompressionNeverExpandsShortLogs(t *testing.T) {
	log := commandLog(t, "PASS", false)
	messages := []model.Message{{Role: model.RoleTool, Content: log}, {Role: model.RoleTool, Content: log}}
	view, r := CompressMessages(messages)
	if !reflect.DeepEqual(view, messages) || r.AfterEstimatedTokens > r.BeforeEstimatedTokens {
		t.Fatal("compression expanded short messages")
	}
}

func TestCompressionPreservesUnserializableTranscript(t *testing.T) {
	log := commandLog(t, strings.Repeat("INFO verification done\n", 100), false)
	messages := []model.Message{
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{Arguments: json.RawMessage(`invalid json`)}}},
		{Role: model.RoleTool, Content: log}, {Role: model.RoleTool, Content: log},
	}
	view, r := CompressMessages(messages)
	if !reflect.DeepEqual(view, messages) || r.DeduplicatedMessages != 0 || r.SourceHash != "" {
		t.Fatal("invalid transcript produced an unverifiable compression reference")
	}
}
