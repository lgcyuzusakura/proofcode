package main

import (
	"context"
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
	"github.com/proofcode-dev/proofcode/agent-engine/internal/experiment"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

const fixDiff = "diff --git a/add.go b/add.go\n--- a/add.go\n+++ b/add.go\n@@ -1,3 +1,3 @@\n package example\n \n-func Add(a, b int) int { return a - b }\n+func Add(a, b int) int { return a + b }\n"

type experimentFixture struct {
	t                             *testing.T
	root, revision, movedRevision string
	server                        *httptest.Server
	runner                        *runner
	mu                            sync.Mutex
	group                         string
	events                        map[string][]event.Event
	requests                      map[string][]map[string]any
	jevs                          map[string]int
	noUsage                       bool
	badTests                      bool
}

func newExperimentFixture(t *testing.T) *experimentFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go unavailable")
	}
	root, err := os.MkdirTemp("", "pcx-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	f := &experimentFixture{t: t, root: root, events: map[string][]event.Event{}, requests: map[string][]map[string]any{}, jevs: map[string]int{}}
	source, bare := filepath.Join(f.root, "source"), filepath.Join(f.root, "repo.git")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		o, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %v %s", args, e, o)
		}
		return strings.TrimSpace(string(o))
	}
	git(f.root, "init", "-q", "-b", "main", source)
	for name, content := range map[string]string{
		"go.mod":      "module example\n\ngo 1.22\n",
		"add.go":      "package example\n\nfunc Add(a, b int) int { return a - b }\n",
		"add_test.go": "package example\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2,3)!=5 { t.Fatal(\"wrong Add\") } }\n",
		".env":        "DUMMY_NON_SECRET=fixture-only\n",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git(source, "add", ".")
	git(source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "fixed input")
	f.revision = git(source, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(source, "add.go"), []byte("package example\n\nfunc Add(a, b int) int { return a * b }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git(source, "add", ".")
	git(source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "moving branch")
	f.movedRevision = git(source, "rev-parse", "HEAD")
	git(f.root, "clone", "--bare", "-q", source, bare)
	git(bare, "update-server-info")
	files := http.FileServer(http.Dir(f.root))
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		group := f.group
		f.mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/repo.git/"):
			files.ServeHTTP(w, r)
		case strings.HasSuffix(r.URL.Path, "/claim"):
			w.WriteHeader(http.StatusAccepted)
		case strings.HasSuffix(r.URL.Path, "/events"):
			var value event.Event
			if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			f.mu.Lock()
			f.events[group] = append(f.events[group], value)
			seq := len(f.events[group])
			f.mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, "{\"sequence\":%d}", seq)
		case r.URL.Path == "/v1/systemone":
			var input struct {
				Questions map[string]struct {
					Criteria map[string]string `json:"criteria"`
				} `json:"questions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			f.mu.Lock()
			step := f.jevs[group]
			f.jevs[group]++
			f.mu.Unlock()
			selected := "read_file"
			if step == 2 {
				selected = "apply_patch"
			}
			probabilities := map[string]float64{}
			for k := range input.Questions["next_tool"].Criteria {
				probabilities[k] = 0
			}
			probabilities[selected] = 1
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "fixture-jev", "answers": map[string]any{"next_tool": map[string]any{"type": "choice", "choice": selected, "confidence": 1, "probabilities": probabilities}}})
		case r.URL.Path == "/chat/completions":
			var input map[string]any
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			f.mu.Lock()
			f.requests[group] = append(f.requests[group], input)
			step := len(f.requests[group]) - 1
			f.mu.Unlock()
			var call *model.ToolCall
			content := "Fixed Add using the supplied repository evidence."
			tools, _ := input["tools"].([]any)
			if len(tools) == 0 {
				content = "```diff\n" + fixDiff + "```"
			} else {
				switch step {
				case 0:
					call = &model.ToolCall{ID: "read-env", Name: "read_file", Arguments: json.RawMessage(`{"path":".env"}`)}
				case 1:
					call = &model.ToolCall{ID: "read-add", Name: "read_file", Arguments: json.RawMessage(`{"path":"add.go"}`)}
				case 2:
					updated := "func Add(a, b int) int { return a + b }"
					if f.badTests {
						updated = "func Add(a, b int) int { return a / b }"
					}
					args, _ := json.Marshal(map[string]any{"edits": []any{map[string]any{"path": "add.go", "old_text": "func Add(a, b int) int { return a - b }", "new_text": updated}}})
					call = &model.ToolCall{ID: "fix-add", Name: "apply_patch", Arguments: args}
				}
			}
			var delta any = map[string]any{"content": content}
			if call != nil {
				delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)}}}}
			}
			w.Header().Set("Content-Type", "text/event-stream")
			encoded, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
			fmt.Fprintf(w, "data: %s\n\n", encoded)
			if !f.noUsage {
				fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20}}\n\n")
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			_ = json.NewEncoder(w).Encode(statusResponse{Status: "RUNNING", Attempt: 1})
		}
	}))
	t.Cleanup(f.server.Close)
	f.runner = &runner{ID: "f112ca6b-3f25-4cc8-a088-fb1e36b3ac61", ControlPlane: f.server.URL, Token: "test", WorkspaceRoot: filepath.Join(f.root, "workspaces"), ModelBaseURL: f.server.URL, HTTP: f.server.Client(), ModelHTTP: f.server.Client(), JevBaseURL: f.server.URL, JevMode: "off", JevThreshold: .85, AllowWrite: true, AllowExec: true}
	return f
}

func (f *experimentFixture) task(group string) taskMessage {
	f.t.Helper()
	profile, err := experiment.ForGroup(group)
	if err != nil {
		f.t.Fatal(err)
	}
	temp := .1
	return taskMessage{Version: "v1", TaskID: fmt.Sprintf("486c92f4-520f-4c87-9e17-141e34e4f3%02x", group[0]), Attempt: 1, ProjectID: "567892f4-520f-4c87-9e17-141e34e4f35b", WorkspaceID: "567892f4-520f-4c87-9e17-141e34e4f35c", ConversationID: "567892f4-520f-4c87-9e17-141e34e4f35d", Repository: f.server.URL + "/repo.git", Prompt: "Fix Add(a,b) in add.go so that it sums numbers. Keep the tests meaningful.", Model: "same-fixture-model", SourceRevision: f.revision, ExperimentID: "567892f4-520f-4c87-9e17-141e34e4f35e", ExperimentRunID: "567892f4-520f-4c87-9e17-141e34e4f35f", ExperimentGroup: group, ProfileVersion: experiment.ProfileVersion, ExperimentProfile: &profile, MaxSteps: 8, Temperature: &temp, TestCommand: "go test ./..."}
}

func (f *experimentFixture) run(task taskMessage) error {
	f.mu.Lock()
	f.group = task.ExperimentGroup
	f.mu.Unlock()
	return f.runner.runTask(context.Background(), task)
}

func (f *experimentFixture) terminal(group string) event.Event {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.events[group]) - 1; i >= 0; i-- {
		v := f.events[group][i]
		if v.Type == event.TaskCompleted || v.Type == event.TaskFailed || v.Type == event.TaskCancelled {
			return v
		}
	}
	f.t.Fatalf("group %s has no terminal event", group)
	return event.Event{}
}

func TestSixProfilesActuallyExecuteOnTheSameImmutableInput(t *testing.T) {
	f := newExperimentFixture(t)
	for _, group := range []string{"A", "B", "C", "D", "E", "F"} {
		t.Run(group, func(t *testing.T) {
			task := f.task(group)
			if err := f.run(task); err != nil {
				t.Fatal(err)
			}
			terminal := f.terminal(group)
			report := terminal.Payload["experimentResult"].(map[string]any)
			if terminal.Type != event.TaskCompleted || report["profileApplied"] != true || report["testsPassed"] != true || report["sourceRevision"] != f.revision || report["sourceRevision"] == f.movedRevision {
				t.Fatalf("group failed fixed test: %+v", terminal)
			}
			if report["inputTokens"] == nil || report["outputTokens"] == nil {
				t.Fatalf("reported usage missing: %+v", report)
			}
			var retrievals, compressions, routes, blocks, patches int
			for _, e := range f.events[group] {
				switch e.Type {
				case event.ContextSelected:
					if e.Payload["kind"] == "retrieval" {
						retrievals++
						if e.Payload["baseCommit"] != f.revision {
							t.Fatalf("wrong evidence revision: %+v", e)
						}
					}
					if e.Payload["kind"] == "compression" {
						compressions++
					}
				case event.ToolRouted:
					routes++
				case event.ToolFailed:
					if metadata, ok := e.Payload["metadata"].(map[string]any); ok && metadata["policyBlocked"] == true {
						blocks++
					}
				case event.CheckpointCreated:
					patches++
					if e.Payload["base"] != f.revision || !strings.Contains(e.Payload["patch"].(string), "return a + b") {
						t.Fatalf("incorrect independent worktree: %+v", e)
					}
				}
			}
			p := task.ExperimentProfile
			if (retrievals > 0) != p.RAGEnabled || (compressions > 0) != p.ContextCompressionEnabled || (routes > 0) != p.JevRouting || (blocks > 0) != p.DeterministicSafety || patches != 1 {
				t.Fatalf("features not executed: retrieval=%d compression=%d routes=%d blocks=%d patches=%d", retrievals, compressions, routes, blocks, patches)
			}
			for _, request := range f.requests[group] {
				tools, _ := request["tools"].([]any)
				if group == "A" && len(tools) != 0 {
					t.Fatal("A received tools")
				}
				if p.JevRouting && len(tools) != 1 {
					t.Fatalf("Jev route not applied: %d", len(tools))
				}
				if request["model"] != "same-fixture-model" || request["temperature"] != .1 {
					t.Fatalf("control variables changed: %+v", request)
				}
			}
			if group == "A" && report["patchSucceeded"] != true {
				t.Fatal("A generated patch was not independently applied")
			}
		})
	}
}

func TestMissingJevDoesNotSilentlyBecomeAnotherGroup(t *testing.T) {
	f := newExperimentFixture(t)
	f.runner.JevBaseURL = ""
	if err := f.run(f.task("C")); err == nil {
		t.Fatal("C succeeded without Jev")
	}
	report := f.terminal("C").Payload["experimentResult"].(map[string]any)
	if report["profileApplied"] != false || len(f.requests["C"]) != 0 {
		t.Fatalf("configuration failure was hidden: %+v", report)
	}
	if err := f.run(f.task("B")); err != nil {
		t.Fatalf("one unavailable feature blocked another group: %v", err)
	}
}

func TestFixedEvaluatorRejectsFalseSuccessAndKeepsMissingUsageUnknown(t *testing.T) {
	f := newExperimentFixture(t)
	f.badTests = true
	f.noUsage = true
	if err := f.run(f.task("B")); err == nil {
		t.Fatal("model's success claim bypassed failing tests")
	}
	terminal := f.terminal("B")
	report := terminal.Payload["experimentResult"].(map[string]any)
	if terminal.Type != event.TaskFailed || report["testsPassed"] != false {
		t.Fatalf("false test result: %+v", terminal)
	}
	if _, ok := report["inputTokens"]; ok {
		t.Fatalf("invented token data: %+v", report)
	}
}

func TestApprovalResumeKeepsProfileAndCollectorState(t *testing.T) {
	f := newExperimentFixture(t)
	f.runner.AllowWrite = false
	task := f.task("B")
	if err := f.run(task); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(f.runner.WorkspaceRoot, task.TaskID, "attempt-1", "run.json")
	readState := func() workspaceMetadata {
		data, err := os.ReadFile(metadataPath)
		if err != nil {
			t.Fatal(err)
		}
		var state workspaceMetadata
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	state := readState()
	if state.PendingCall == nil || state.PendingCall.ID != "fix-add" || state.ExperimentState.Values["toolCalls"] != float64(3) {
		t.Fatalf("pause not durable: %+v", state)
	}
	before, err := os.ReadFile(filepath.Join(state.Handle.Path, "add.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), "a - b") {
		t.Fatal("patch ran before approval")
	}
	task.Resume = true
	task.ApprovalID = "fix-add"
	task.Decision = "approved"
	if err := f.run(task); err != nil {
		t.Fatal(err)
	}
	report := f.terminal("B").Payload["experimentResult"].(map[string]any)
	if report["toolCalls"] != float64(3) || report["inputTokens"] != float64(400) || report["testsPassed"] != true {
		t.Fatalf("resume reset experiment state: %+v", report)
	}
}

func TestApprovalResumeCannotChangeExperimentalInputs(t *testing.T) {
	f := newExperimentFixture(t)
	f.runner.AllowWrite = false
	task := f.task("B")
	if err := f.run(task); err != nil {
		t.Fatal(err)
	}
	task.Resume = true
	task.ApprovalID = "fix-add"
	task.Decision = "approved"
	task.Prompt = "changed task"
	if err := f.run(task); err == nil || !strings.Contains(err.Error(), "different execution configuration") {
		t.Fatalf("changed input reused state: %v", err)
	}
}
