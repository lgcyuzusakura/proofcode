package browser

import (
	"context"
	"encoding/json"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestLiveTaskBrowserSurvivesCallsAndHonorsCancellation(t *testing.T) {
	executable := os.Getenv("PROOFCODE_BROWSER_TEST_EXECUTABLE")
	if executable == "" {
		t.Skip("opt-in: real Chromium executable required")
	}
	ws, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<title>Task browser fixture</title><body><button id="run" onclick="document.getElementById('result').innerText='clicked'">Run</button><p id="result"></p><script>console.log('task-browser-ok')</script></body>`))
	}))
	defer server.Close()
	tool := NewTaskTool(context.Background(), executable, ws)
	defer tool.Close()
	open, _ := json.Marshal(map[string]any{"action": "open", "url": server.URL})
	if result := tool.Execute(context.Background(), open); result.IsError {
		t.Fatal(result.Content)
	}
	if result := tool.Execute(context.Background(), json.RawMessage(`{"action":"click","selector":"#run"}`)); result.IsError {
		t.Fatal(result.Content)
	}
	if result := tool.Execute(context.Background(), json.RawMessage(`{"action":"snapshot"}`)); result.IsError || !strings.Contains(result.Content, "clicked") {
		t.Fatalf("snapshot: %+v", result)
	}
	if result := tool.Execute(context.Background(), json.RawMessage(`{"action":"console"}`)); result.IsError || !strings.Contains(result.Content, "task-browser-ok") {
		t.Fatalf("console: %+v", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := tool.Execute(ctx, json.RawMessage(`{"action":"snapshot"}`)); !result.IsError {
		t.Fatal("canceled request executed")
	}
}
