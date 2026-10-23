package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	amqp "github.com/Azure/go-amqp"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

func TestDecodeTaskMessageAcceptsAMQPDataAndJMSValue(t *testing.T) {
	const payload = `{"taskId":"abc","attempt":2}`
	for name, message := range map[string]*amqp.Message{
		"data":       amqp.NewMessage([]byte(payload)),
		"jms text":   {Value: payload},
		"byte value": {Value: []byte(payload)},
	} {
		t.Run(name, func(t *testing.T) {
			task, err := decodeTaskMessage(message)
			if err != nil || task.TaskID != "abc" || task.Attempt != 2 {
				t.Fatalf("decode task message: task=%+v err=%v", task, err)
			}
		})
	}
}

func TestHTTPEventSinkRetriesWithSameIdentity(t *testing.T) {
	var received []event.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value event.Event
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			t.Errorf("decode event: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received = append(received, value)
		if len(received) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"sequence":7}`))
	}))
	defer server.Close()

	sink := &httpEventSink{client: &http.Client{Timeout: time.Second}, baseURL: server.URL, token: "test"}
	value := event.Event{Version: "v1", TaskID: "task-1", Attempt: 2, RunID: "run-1", RunnerID: "runner-1", Sequence: 4, Type: event.ToolRequested, Timestamp: time.Now().UTC(), Payload: map[string]any{"tool": "read_file"}}
	if err := sink.Publish(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 || !reflect.DeepEqual(received[0], received[1]) || received[1].RunnerID != value.RunnerID {
		t.Fatalf("event identity changed across retry: %+v", received)
	}
}

func TestClaimWaitsForBusyLeaseInsteadOfAcknowledgingDelivery(t *testing.T) {
	claims := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/claim") {
			claims++
			if claims == 1 {
				w.WriteHeader(http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_ = json.NewEncoder(w).Encode(statusResponse{Status: "RUNNING", Attempt: 1, LeaseUntil: pointerTime(time.Now().Add(-time.Second))})
	}))
	defer server.Close()
	r := &runner{ID: "runner-1", ControlPlane: server.URL, Token: "test", HTTP: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	claimed, err := r.claim(ctx, "task-1", 1)
	if err != nil || !claimed || claims != 2 {
		t.Fatalf("busy delivery was not retried: claimed=%v err=%v claims=%d", claimed, err, claims)
	}
}

func TestClaimAcknowledgesTerminalDelivery(t *testing.T) {
	claims := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/claim") {
			claims++
			w.WriteHeader(http.StatusConflict)
			return
		}
		_ = json.NewEncoder(w).Encode(statusResponse{Status: "SUCCEEDED", Attempt: 1})
	}))
	defer server.Close()
	r := &runner{ID: "runner-1", ControlPlane: server.URL, Token: "test", HTTP: server.Client()}
	claimed, err := r.claim(context.Background(), "task-1", 1)
	if err != nil || claimed || claims != 1 {
		t.Fatalf("terminal delivery was not acknowledged: claimed=%v err=%v claims=%d", claimed, err, claims)
	}
}

func pointerTime(value time.Time) *time.Time { return &value }

func TestRunnerCompletesReadOnlyTaskWithoutCheckpoint(t *testing.T) {
	testRunnerReadOnlyTask(t, "CODE")
}

func TestRunnerPlanBlocksWritesAndCommandsButPersistsPlan(t *testing.T) {
	testRunnerReadOnlyTask(t, "PLAN")
}

func testRunnerReadOnlyTask(t *testing.T, mode string) {
	t.Setenv("RUNNER_ALLOW_WRITE", "true")
	t.Setenv("RUNNER_ALLOW_EXEC", "true")
	t.Setenv("RUNNER_BROWSER_ENABLED", "false")
	t.Setenv("RUNNER_MCP_SERVERS", "")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	bare := filepath.Join(root, "repo.git")
	runGit := func(dir string, args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
		}
	}
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(root, "init", "-q", "-b", "main", source)
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(source, "add", "README.md")
	runGit(source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "initial")
	runGit(root, "clone", "--bare", "-q", source, bare)
	runGit(bare, "update-server-info")

	var mu sync.Mutex
	var emitted []event.Event
	modelCalls := 0
	snapshot, _ := json.Marshal(agentSnapshot{Skills: []tool.Skill{{ID: "skill-one", Name: "review", Description: "Review source and tests", Content: "immutable-review-step", Version: 2, Hash: "review-hash"}}})
	fileServer := http.FileServer(http.Dir(root))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasPrefix(request.URL.Path, "/repo.git/"):
			fileServer.ServeHTTP(w, request)
		case strings.HasSuffix(request.URL.Path, "/claim"):
			w.WriteHeader(http.StatusAccepted)
		case strings.HasSuffix(request.URL.Path, "/conversation"), strings.HasSuffix(request.URL.Path, "/instructions"):
			_ = json.NewEncoder(w).Encode([]any{})
		case strings.HasSuffix(request.URL.Path, "/agent-snapshot"):
			_, _ = w.Write(snapshot)
		case strings.HasSuffix(request.URL.Path, "/events"):
			var value event.Event
			if err := json.NewDecoder(request.Body).Decode(&value); err != nil {
				t.Errorf("decode event: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			emitted = append(emitted, value)
			sequence := len(emitted)
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"sequence":` + fmt.Sprint(sequence) + `}`))
		case request.URL.Path == "/chat/completions":
			modelCalls++
			body, _ := io.ReadAll(request.Body)
			if modelCalls == 1 && strings.Contains(string(body), "immutable-review-step") {
				t.Error("skill body was injected before read")
			}
			if ((mode == "PLAN" && modelCalls == 3) || (mode == "CODE" && modelCalls == 2)) && !strings.Contains(string(body), "immutable-review-step") {
				t.Error("saved skill missing after tool read")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			if mode == "CODE" && modelCalls == 1 {
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"load-skill\",\"type\":\"function\",\"function\":{\"name\":\"skill_read\",\"arguments\":\"{\\\"name\\\":\\\"review\\\"}\"}}]}}]}\n\ndata: [DONE]\n\n")
				return
			}
			if mode == "PLAN" && modelCalls <= 2 {
				calls := []map[string]any{}
				if modelCalls == 1 {
					calls = append(calls, map[string]any{"index": 0, "id": "patch", "type": "function", "function": map[string]any{"name": "apply_patch", "arguments": `{"edits":[{"path":"README.md","old_text":"hello","new_text":"changed"}]}`}}, map[string]any{"index": 1, "id": "command", "type": "function", "function": map[string]any{"name": "run_command", "arguments": `{"program":"git","args":["status"]}`}})
				} else {
					calls = append(calls, map[string]any{"index": 0, "id": "plan", "type": "function", "function": map[string]any{"name": "update_plan", "arguments": `{"action":"set","expectedVersion":0,"summary":"Inspect before implementing","steps":[{"id":"inspect","text":"Inspect README","status":"in_progress"}]}`}})
					calls = append(calls, map[string]any{"index": 1, "id": "load-skill", "type": "function", "function": map[string]any{"name": "skill_read", "arguments": `{"name":"review"}`}})
				}
				data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": calls}}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
				return
			}
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\ndata: [DONE]\n\n"))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	r := &runner{ID: "f112ca6b-3f25-4cc8-a088-fb1e36b3ac61", ControlPlane: server.URL, Token: "test", WorkspaceRoot: filepath.Join(root, "workspaces"), ModelBaseURL: server.URL, ModelAPIKey: "test", HTTP: server.Client(), ModelHTTP: server.Client()}
	task := taskMessage{TaskID: "486c92f4-520f-4c87-9e17-141e34e4f35b", Attempt: 1, ProjectID: "567892f4-520f-4c87-9e17-141e34e4f35b", WorkspaceID: "567892f4-520f-4c87-9e17-141e34e4f35c", ConversationID: "567892f4-520f-4c87-9e17-141e34e4f35d", Repository: server.URL + "/repo.git", Branch: "main", Prompt: "Summarize the project", Model: "mock", ExecutionMode: mode}
	task.AgentSnapshotHash = snapshotHash(string(snapshot))
	if err := r.runTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	var completed, checkpoints int
	var blocked, plans, skills int
	for _, value := range emitted {
		switch value.Type {
		case event.TaskCompleted:
			completed++
		case event.CheckpointCreated:
			checkpoints++
		case event.ToolFailed:
			if strings.Contains(fmt.Sprint(value.Payload), "Read-only agent cannot execute") {
				blocked++
			}
		case event.ToolCompleted:
			if value.Payload["tool"] == "skill_read" {
				skills++
			}
		case "plan.updated":
			plans++
		}
	}
	if completed != 1 || checkpoints != 0 {
		t.Fatalf("read-only task emitted completed=%d checkpoints=%d events=%+v", completed, checkpoints, emitted)
	}
	if mode == "PLAN" && (blocked != 2 || plans != 1) {
		t.Fatalf("PLAN did not enforce read-only tools and publish its plan: blocked=%d plans=%d events=%+v", blocked, plans, emitted)
	}
	if skills != 1 {
		t.Fatalf("saved skill not loaded in %s mode: %+v", mode, emitted)
	}
	if data, err := os.ReadFile(filepath.Join(source, "README.md")); err != nil || string(data) != "hello\n" {
		t.Fatalf("source changed: %q, %v", data, err)
	}
}
