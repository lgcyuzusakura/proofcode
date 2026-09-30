package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	amqp "github.com/Azure/go-amqp"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/agent"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/worktree"
)

type taskMessage struct {
	Version    string `json:"version"`
	TaskID     string `json:"taskId"`
	ProjectID  string `json:"projectId"`
	Repository string `json:"repositoryUrl"`
	Branch     string `json:"branch"`
	Prompt     string `json:"prompt"`
	Model      string `json:"model"`
}

type runner struct {
	ID            string
	ControlPlane  string
	Token         string
	WorkspaceRoot string
	ModelBaseURL  string
	ModelAPIKey   string
	AllowWrite    bool
	AllowExec     bool
	HTTP          *http.Client
	Active        sync.Map
}

type statusResponse struct {
	Status string `json:"status"`
}

func main() {
	r := &runner{
		ID: os.Getenv("RUNNER_ID"), ControlPlane: strings.TrimRight(env("CONTROL_PLANE_URL", "http://localhost:8080"), "/"),
		Token: os.Getenv("RUNNER_TOKEN"), WorkspaceRoot: env("WORKSPACE_ROOT", filepath.Join(os.TempDir(), "proofcode-workspaces")),
		ModelBaseURL: env("MODEL_BASE_URL", "https://api.openai.com/v1"), ModelAPIKey: os.Getenv("MODEL_API_KEY"),
		AllowWrite: envBool("RUNNER_ALLOW_WRITE", false), AllowExec: envBool("RUNNER_ALLOW_EXEC", false), HTTP: &http.Client{Timeout: 15 * time.Second},
	}
	if r.ID == "" {
		r.ID = fmt.Sprintf("runner-%d", time.Now().UnixNano())
	}
	if r.Token == "" {
		log.Fatal("RUNNER_TOKEN is required")
	}
	if err := os.MkdirAll(r.WorkspaceRoot, 0750); err != nil {
		log.Fatal(err)
	}
	address := env("ARTEMIS_URL", "amqp://localhost:5672")
	ctx := context.Background()
	conn, err := amqp.Dial(ctx, address, nil)
	if err != nil {
		log.Fatalf("connect Artemis: %v", err)
	}
	defer conn.Close()
	session, err := conn.NewSession(ctx, nil)
	if err != nil {
		log.Fatalf("create AMQP session: %v", err)
	}
	queue := env("AGENT_QUEUE", "agent.task.requested")
	receiver, err := session.NewReceiver(ctx, queue, &amqp.ReceiverOptions{Credit: 1})
	if err != nil {
		log.Fatalf("open task queue %q: %v", queue, err)
	}
	defer receiver.Close(ctx)
	log.Printf("runner %s listening on %s", r.ID, queue)
	for {
		message, receiveErr := receiver.Receive(ctx, nil)
		if receiveErr != nil {
			log.Fatalf("receive task: %v", receiveErr)
		}
		var task taskMessage
		if err := json.Unmarshal(message.GetData(), &task); err != nil || task.TaskID == "" {
			_ = receiver.RejectMessage(ctx, message, nil)
			log.Printf("discard invalid task message: %v", err)
			continue
		}
		if err := r.runTask(ctx, task); err != nil {
			log.Printf("task %s failed: %v", task.TaskID, err)
			_ = receiver.ReleaseMessage(ctx, message)
			continue
		}
		if err := receiver.AcceptMessage(ctx, message); err != nil {
			log.Printf("ack task %s: %v", task.TaskID, err)
		}
	}
}

func (r *runner) runTask(parent context.Context, task taskMessage) (runErr error) {
	if !validRepositoryURL(task.Repository) {
		return errors.New("repositoryUrl must be http or https")
	}
	claimed, err := r.claim(parent, task.TaskID)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	r.Active.Store(task.TaskID, cancel)
	defer func() { cancel(); r.Active.Delete(task.TaskID) }()
	go r.watchCancellation(ctx, task.TaskID, cancel)
	go r.watchLease(ctx, task.TaskID)
	events := event.NewSequencedSink(&httpEventSink{client: r.HTTP, baseURL: r.ControlPlane, token: r.Token})
	defer func() {
		if runErr == nil {
			return
		}
		kind := event.TaskFailed
		if errors.Is(ctx.Err(), context.Canceled) {
			kind = event.TaskCancelled
		}
		_ = events.Emit(context.Background(), task.TaskID, kind, map[string]any{"error": runErr.Error()})
	}()

	base := filepath.Join(r.WorkspaceRoot, task.TaskID)
	if err := os.RemoveAll(base); err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0750); err != nil {
		return err
	}
	repository := filepath.Join(base, "repository")
	if err := clone(ctx, task.Repository, task.Branch, repository); err != nil {
		return err
	}
	manager := worktree.Manager{Repository: repository, Root: filepath.Join(base, "worktrees")}
	handle, err := manager.Create(ctx, task.TaskID)
	if err != nil {
		return err
	}
	defer func() { _ = manager.Remove(context.Background(), handle); _ = os.RemoveAll(base) }()
	ws, err := workspace.Open(handle.Path)
	if err != nil {
		return err
	}
	provider := &model.OpenAICompatible{BaseURL: r.ModelBaseURL, APIKey: r.ModelAPIKey, Client: r.HTTP}
	mainTools := tool.NewRegistry(tool.ReadFile{Workspace: ws}, tool.ListFiles{Workspace: ws}, tool.SearchCode{Workspace: ws}, tool.ApplyPatch{Workspace: ws}, tool.RunCommand{Workspace: ws}, tool.GitDiff{Workspace: ws})
	readTools := tool.NewRegistry(tool.ReadFile{Workspace: ws}, tool.ListFiles{Workspace: ws}, tool.SearchCode{Workspace: ws}, tool.GitDiff{Workspace: ws})
	verifyTools := tool.NewRegistry(tool.ReadFile{Workspace: ws}, tool.ListFiles{Workspace: ws}, tool.SearchCode{Workspace: ws}, tool.RunCommand{Workspace: ws}, tool.GitDiff{Workspace: ws})
	coordinator := &agent.Coordinator{Provider: provider, MainTools: mainTools, ScoutTools: readTools, VerifierTools: verifyTools, Approval: agent.AutomaticApproval{AllowWrite: r.AllowWrite, AllowExec: r.AllowExec}, Events: events, Model: task.Model}
	if _, err := coordinator.Run(ctx, agent.CoordinateRequest{TaskID: task.TaskID, Prompt: task.Prompt}); err != nil {
		return err
	}
	hash, err := manager.Checkpoint(ctx, handle, "ProofCode checkpoint: task completed")
	if err != nil {
		return err
	}
	return events.Emit(ctx, task.TaskID, event.CheckpointCreated, map[string]any{"commit": hash, "path": handle.Path})
}

func (r *runner) claim(ctx context.Context, taskID string) (bool, error) {
	request, err := r.request(ctx, http.MethodPost, "/internal/tasks/"+taskID+"/claim", map[string]string{"runnerId": r.ID})
	if err != nil {
		return false, err
	}
	defer request.Body.Close()
	if request.StatusCode == http.StatusConflict {
		return false, nil
	}
	if request.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(request.Body, 4096))
		return false, fmt.Errorf("claim task: %s: %s", request.Status, strings.TrimSpace(string(body)))
	}
	return true, nil
}

func (r *runner) watchCancellation(ctx context.Context, taskID string, cancel context.CancelFunc) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			response, err := r.request(ctx, http.MethodGet, "/internal/tasks/"+taskID, nil)
			if err != nil {
				continue
			}
			var status statusResponse
			_ = json.NewDecoder(response.Body).Decode(&status)
			response.Body.Close()
			if status.Status == "CANCELLED" {
				cancel()
				return
			}
		}
	}
}

func (r *runner) watchLease(ctx context.Context, taskID string) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			response, err := r.request(ctx, http.MethodPost, "/internal/tasks/"+taskID+"/renew", map[string]string{"runnerId": r.ID})
			if err != nil {
				continue
			}
			response.Body.Close()
			if response.StatusCode == http.StatusConflict {
				return
			}
		}
	}
}

func (r *runner) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, r.ControlPlane+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return r.HTTP.Do(req)
}

type httpEventSink struct {
	client         *http.Client
	baseURL, token string
}

func (s *httpEventSink) Publish(ctx context.Context, value event.Event) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/internal/tasks/"+value.TaskID+"/events", strings.NewReader(string(data)))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+s.token)
		req.Header.Set("Content-Type", "application/json")
		response, err := s.client.Do(req)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
			response.Body.Close()
			if response.StatusCode/100 == 2 {
				return nil
			}
			lastErr = fmt.Errorf("event ingest: %s: %s", response.Status, strings.TrimSpace(string(body)))
			if readErr != nil {
				lastErr = readErr
			}
			if response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests {
				return lastErr
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * 150 * time.Millisecond):
		}
	}
	return lastErr
}

func clone(ctx context.Context, repository, branch, destination string) error {
	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repository, destination)
	command := exec.CommandContext(ctx, "git", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("clone repository: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
func validRepositoryURL(value string) bool {
	return strings.HasPrefix(strings.ToLower(value), "https://") || strings.HasPrefix(strings.ToLower(value), "http://")
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envBool(key string, fallback bool) bool {
	value := strings.ToLower(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes"
}
