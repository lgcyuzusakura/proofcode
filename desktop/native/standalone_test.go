package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStandalonePrivateDirectoryHasExclusiveInstanceLock(t *testing.T) {
	directory := t.TempDir()
	if err := protectStandaloneDirectory(directory); err != nil {
		t.Fatal(err)
	}
	first, err := lockStandaloneDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockStandaloneDirectory(directory); err == nil {
		second.Close()
		t.Fatal("second instance acquired the database directory")
	}
	first.Close()
	next, err := lockStandaloneDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
}

func TestStandaloneChildEnvironmentDoesNotReuseExternalDeploymentSecrets(t *testing.T) {
	t.Setenv("SPRING_DATASOURCE_URL", "private-external-database")
	t.Setenv("JAVA_TOOL_OPTIONS", "untrusted-java-flags")
	t.Setenv("MODEL_API_KEY", "external-key")
	t.Setenv("RUNNER_TOKEN", "external-runner")
	environment := strings.Join(standaloneEnvironment(), "\n")
	for _, blocked := range []string{"private-external-database", "untrusted-java-flags", "external-key", "external-runner"} {
		if strings.Contains(environment, blocked) {
			t.Fatal("inherited external deployment configuration")
		}
	}
}

// Runs the shipped Java runtime, real file database, API and Agent worker.
// The model below is a protocol fixture; this test makes no model-quality claim.
func TestStandalonePackagedWorkflowAndRestart(t *testing.T) {
	bundle := os.Getenv("PROOFCODE_STANDALONE_TEST_BUNDLE")
	if bundle == "" {
		t.Skip("set PROOFCODE_STANDALONE_TEST_BUNDLE to a prepared Windows sidecar")
	}
	t.Setenv("PATH", filepath.Join(os.Getenv("SystemRoot"), "System32"))
	var mu sync.Mutex
	var prompts []string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer local-fixture-key" {
			http.Error(w, "bad fixture credential", 401)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/models") {
			fmt.Fprint(w, `{"data":[{"id":"fixture-model"}]}`)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad body", 400)
			return
		}
		raw, _ := json.Marshal(body)
		mu.Lock()
		prompts = append(prompts, string(raw))
		mu.Unlock()
		if strings.Contains(string(raw), "cancel-fixture") {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(string(raw), "create-fixture-file") && !hasToolResult(body) {
			args, _ := json.Marshal(map[string]any{"edits": []any{map[string]any{"path": "hello.txt", "new_text": "standalone durable bytes\n", "create": true}}})
			delta := map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "write-fixture", "type": "function", "function": map[string]any{"name": "apply_patch", "arguments": string(args)}}}}
			response, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", response)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"local-fixture-complete\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer model.Close()
	registry := filepath.Join(t.TempDir(), "profile")
	start := func() *App {
		app := &App{registryDir: registry, ctx: context.Background()}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		run, err := app.startStandalone(ctx, bundle)
		if err != nil {
			if data, e := os.ReadFile(filepath.Join(registry, "standalone", "runtime.log")); e == nil {
				t.Log(string(data))
			}
			t.Fatal(err)
		}
		app.standalone = run
		return app
	}
	app := start()
	defer func() { app.closeStandalone() }()
	login, err := app.GetStandaloneSession()
	if err != nil {
		t.Fatal(err)
	}
	session := login["token"].(string)
	call := func(method, path string, body any) map[string]any {
		t.Helper()
		data, _ := json.Marshal(body)
		response, err := app.ProxyRequest(path, method, string(data), "Bearer "+session, "")
		if err != nil || response.Status/100 != 2 {
			t.Fatalf("%s %s: %d %s %v", method, path, response.Status, response.Body, err)
		}
		var value map[string]any
		if err = json.Unmarshal([]byte(response.Body), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	provider := call("POST", "/api/model-providers", map[string]any{"name": "Local fixture", "baseUrl": model.URL, "apiKey": "local-fixture-key", "wireApi": "chat"})
	providerID := provider["id"].(string)
	call("POST", "/api/model-providers/"+providerID+"/models/refresh", nil)
	project := call("POST", "/api/projects", map[string]any{"name": "Standalone QA", "sourceKind": "SCRATCH", "defaultBranch": "main"})
	projectID := project["id"].(string)
	create := func(mode, prompt string) map[string]any {
		return call("POST", "/api/tasks", map[string]any{"projectId": projectID, "prompt": prompt, "model": "fixture-model", "providerId": providerID, "executionMode": mode})
	}
	await := func(id, status string) map[string]any {
		t.Helper()
		deadline := time.Now().Add(40 * time.Second)
		var last map[string]any
		for time.Now().Before(deadline) {
			value := call("GET", "/api/tasks/"+id, nil)
			last = value
			if value["status"] == status {
				return value
			}
			if value["status"] == "FAILED" {
				t.Fatalf("task failed: %+v", value)
			}
			time.Sleep(100 * time.Millisecond)
		}
		if data, err := os.ReadFile(filepath.Join(registry, "standalone", "runtime.log")); err == nil {
			t.Log(string(data))
		}
		t.Fatalf("task %s never reached %s; last state: %+v", id, status, last)
		return nil
	}
	chat := create("CHAT", "remember-fixture-history")
	chatID := chat["id"].(string)
	await(chatID, "SUCCEEDED")
	code := create("CODE", "create-fixture-file")
	codeID := code["id"].(string)
	await(codeID, "WAITING_APPROVAL")
	// Reopen a real on-disk database and resume the same checkpoint/approval.
	app.closeStandalone()
	app = start()
	login, err = app.GetStandaloneSession()
	if err != nil {
		t.Fatal(err)
	}
	session = login["token"].(string)
	await(chatID, "SUCCEEDED")
	await(codeID, "WAITING_APPROVAL")
	response, err := app.ProxyRequest("/api/model-providers", "GET", "", "Bearer "+session, "")
	if err != nil || !strings.Contains(response.Body, providerID) || strings.Contains(response.Body, "local-fixture-key") {
		t.Fatalf("provider persistence/redaction: %v", err)
	}
	response, err = app.ProxyRequest("/api/tasks/"+codeID+"/approvals", "GET", "", "Bearer "+session, "")
	if err != nil {
		t.Fatal(err)
	}
	var approvals []map[string]any
	if json.Unmarshal([]byte(response.Body), &approvals) != nil || len(approvals) != 1 {
		t.Fatalf("missing persisted approval: %s", response.Body)
	}
	call("POST", "/api/tasks/"+codeID+"/approvals/"+approvals[0]["id"].(string), map[string]any{"approved": true})
	result := await(codeID, "SUCCEEDED")
	if result["resultSourceSnapshotId"] == nil {
		t.Fatal("code result snapshot missing")
	}
	response, err = app.ProxyRequest("/api/tasks/"+codeID+"/artifacts/checkpoint", "GET", "", "Bearer "+session, "")
	if err != nil || !strings.Contains(response.Body, "hello.txt") {
		t.Fatalf("code patch missing: %s %v", response.Body, err)
	}
	cancelTask := create("CHAT", "cancel-fixture")
	cancelID := cancelTask["id"].(string)
	await(cancelID, "RUNNING")
	call("POST", "/api/tasks/"+cancelID+"/cancel", nil)
	await(cancelID, "CANCELLED")
	report := app.CheckRuntime()
	if len(report.Checks) != 2 {
		t.Fatalf("standalone probed external deployment dependencies: %+v", report)
	}
	for _, check := range report.Checks {
		if check.State != "ready" {
			t.Fatalf("bundled runtime dependency unavailable: %+v", check)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(prompts, "\n"), "remember-fixture-history") {
		t.Fatal("history never reached worker")
	}
}

func hasToolResult(body map[string]any) bool {
	messages, _ := body["messages"].([]any)
	for _, message := range messages {
		value, _ := message.(map[string]any)
		if value["role"] == "tool" {
			return true
		}
	}
	return false
}
