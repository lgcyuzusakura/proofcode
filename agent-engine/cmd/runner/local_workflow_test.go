package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/source"
)

func localArchive(path, content string) source.Archive {
	data := []byte(content)
	hash := sha256.Sum256(data)
	files := []source.File{{Path: path, SHA256: hex.EncodeToString(hash[:]), Content: base64.StdEncoding.EncodeToString(data)}}
	return source.Archive{Files: files, ManifestHash: source.Hash(files)}
}

func scopedLocalTask() taskMessage {
	return taskMessage{Version: "v1", TaskID: "486c92f4-520f-4c87-9e17-141e34e4f35b", Attempt: 1,
		ProjectID: "567892f4-520f-4c87-9e17-141e34e4f35b", WorkspaceID: "567892f4-520f-4c87-9e17-141e34e4f35c",
		ConversationID: "567892f4-520f-4c87-9e17-141e34e4f35d", SourceKind: "SCRATCH", Model: "fixture-only"}
}

func TestChatRestoresHistoryWithoutCreatingAnExecutionWorkspace(t *testing.T) {
	var mu sync.Mutex
	var input map[string]any
	var emitted []event.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/claim"):
			w.WriteHeader(http.StatusAccepted)
		case strings.HasSuffix(request.URL.Path, "/conversation"):
			_ = json.NewEncoder(w).Encode([]map[string]string{{"role": "user", "content": "Use Java 17"}, {"role": "assistant", "content": "Agreed"}})
		case strings.HasSuffix(request.URL.Path, "/events"):
			var value event.Event
			if err := json.NewDecoder(request.Body).Decode(&value); err != nil {
				t.Error(err)
			}
			mu.Lock()
			emitted = append(emitted, value)
			sequence := len(emitted)
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]int{"sequence": sequence})
		case request.URL.Path == "/chat/completions":
			var value map[string]any
			if err := json.NewDecoder(request.Body).Decode(&value); err != nil {
				t.Error(err)
			}
			mu.Lock()
			input = value
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Java 17 plan\"}}]}\n\ndata: [DONE]\n\n")
		default:
			_ = json.NewEncoder(w).Encode(statusResponse{Status: "RUNNING", Attempt: 1})
		}
	}))
	defer server.Close()
	root := filepath.Join(t.TempDir(), "unused-workspaces")
	r := &runner{ID: "f112ca6b-3f25-4cc8-a088-fb1e36b3ac61", ControlPlane: server.URL, HTTP: server.Client(),
		ModelBaseURL: server.URL, ModelHTTP: server.Client(), WorkspaceRoot: root}
	task := scopedLocalTask()
	task.ExecutionMode, task.Prompt = "CHAT", "Continue the plan"
	if err := r.runTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("chat created a code execution workspace: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if tools, ok := input["tools"].([]any); ok && len(tools) != 0 {
		t.Fatalf("chat received execution tools: %+v", tools)
	}
	encoded, _ := json.Marshal(input["messages"])
	for _, required := range []string{"Use Java 17", "Agreed", "Continue the plan"} {
		if !strings.Contains(string(encoded), required) {
			t.Fatalf("history omitted %s: %s", required, encoded)
		}
	}
	completed := 0
	for _, value := range emitted {
		if value.Type == event.TaskCompleted {
			completed++
			if value.Payload["result"] != "Java 17 plan" {
				t.Fatalf("chat completion lost the final answer: %+v", value.Payload)
			}
		}
	}
	if completed != 1 {
		t.Fatalf("chat emitted %d terminal completions", completed)
	}
}

func TestLocalSourceUsesQueuedDirtyBytesAndRejectsAChangedManifest(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git required")
	}
	archive := localArchive("hello.txt", "uncommitted current bytes\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(w).Encode(archive)
	}))
	defer server.Close()
	r := &runner{ControlPlane: server.URL, HTTP: server.Client()}
	task := scopedLocalTask()
	task.SourceKind = "LOCAL_FOLDER"
	task.SourceSnapshotID = "567892f4-520f-4c87-9e17-141e34e4f35e"
	task.SourceManifestHash = archive.ManifestHash
	destination := filepath.Join(t.TempDir(), "private-repository")
	if err := r.prepareLocalSource(context.Background(), task, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "hello.txt"))
	if err != nil || string(data) != "uncommitted current bytes\n" {
		t.Fatalf("snapshot bytes changed: %q %v", data, err)
	}
	command := exec.Command("git", "show", "HEAD:hello.txt")
	command.Dir = destination
	committed, err := command.Output()
	if err != nil || string(committed) != string(data) {
		t.Fatalf("private baseline did not preserve exact bytes: %q %v", committed, err)
	}
	task.SourceManifestHash = strings.Repeat("0", 64)
	other := filepath.Join(t.TempDir(), "rejected")
	if err := r.prepareLocalSource(context.Background(), task, other); err == nil {
		t.Fatal("accepted a downloaded archive different from the queued manifest")
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatal("materialized untrusted mismatched source")
	}
}
