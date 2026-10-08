package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	amqp "github.com/Azure/go-amqp"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/agent"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/experiment"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/worktree"
)

type taskMessage struct {
	Version           string              `json:"version"`
	TaskID            string              `json:"taskId"`
	Attempt           int                 `json:"attempt"`
	ProjectID         string              `json:"projectId"`
	Repository        string              `json:"repositoryUrl"`
	Branch            string              `json:"branch"`
	Prompt            string              `json:"prompt"`
	Model             string              `json:"model"`
	Resume            bool                `json:"resume"`
	ApprovalID        string              `json:"approvalId"`
	Decision          string              `json:"approvalDecision"`
	WorkspaceID       string              `json:"workspaceId"`
	ConversationID    string              `json:"conversationId"`
	SourceRevision    string              `json:"sourceRevision"`
	ExperimentID      string              `json:"experimentId"`
	ExperimentRunID   string              `json:"experimentRunId"`
	ExperimentGroup   string              `json:"experimentGroup"`
	ProfileVersion    string              `json:"profileVersion"`
	ExperimentProfile *experiment.Profile `json:"experimentProfile"`
	MaxSteps          int                 `json:"maxSteps"`
	Temperature       *float64            `json:"temperature"`
	TestCommand       string              `json:"testCommand"`
}

type workspaceMetadata struct {
	Repository          string           `json:"repository"`
	Attempt             int              `json:"attempt"`
	Handle              worktree.Handle  `json:"handle"`
	Messages            []model.Message  `json:"messages,omitempty"`
	Usage               model.Usage      `json:"usage"`
	PendingCall         *model.ToolCall  `json:"pendingCall,omitempty"`
	RemainingCalls      []model.ToolCall `json:"remainingCalls,omitempty"`
	ApprovalID          string           `json:"approvalId,omitempty"`
	MainCompleted       bool             `json:"mainCompleted,omitempty"`
	MainContent         string           `json:"mainContent,omitempty"`
	CheckpointHash      string           `json:"checkpointHash,omitempty"`
	InFlightCall        *model.ToolCall  `json:"inFlightCall,omitempty"`
	ConfigurationHash   string           `json:"configurationHash,omitempty"`
	ExperimentState     experiment.State `json:"experimentState"`
	EvaluationStarted   bool             `json:"evaluationStarted,omitempty"`
	EvaluationCompleted bool             `json:"evaluationCompleted,omitempty"`
}

type runner struct {
	ID            string
	ControlPlane  string
	Token         string
	WorkspaceRoot string
	ModelBaseURL  string
	ModelAPIKey   string
	JevBaseURL    string
	JevAPIKey     string
	JevModel      string
	JevMode       string
	JevThreshold  float64
	AllowWrite    bool
	AllowExec     bool
	CommandPolicy tool.CommandPolicy
	HTTP          *http.Client
	ModelHTTP     *http.Client
	Active        sync.Map
}

type statusResponse struct {
	Status     string     `json:"status"`
	Attempt    int        `json:"attempt"`
	LeaseUntil *time.Time `json:"leaseUntil"`
}

var taskIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

func main() {
	r := &runner{
		ID: os.Getenv("RUNNER_ID"), ControlPlane: strings.TrimRight(env("CONTROL_PLANE_URL", "http://localhost:8080"), "/"),
		Token: os.Getenv("RUNNER_TOKEN"), WorkspaceRoot: env("WORKSPACE_ROOT", filepath.Join(os.TempDir(), "proofcode-workspaces")),
		ModelBaseURL: env("MODEL_BASE_URL", "https://api.openai.com/v1"), ModelAPIKey: os.Getenv("MODEL_API_KEY"),
		JevBaseURL: os.Getenv("JEV_BASE_URL"), JevAPIKey: os.Getenv("JEV_API_KEY"), JevModel: env("JEV_MODEL", "jev-latest"), JevMode: strings.ToLower(env("JEV_MODE", "off")), JevThreshold: envFloat("JEV_MIN_CONFIDENCE", 0.85),
		AllowWrite: envBool("RUNNER_ALLOW_WRITE", false), AllowExec: envBool("RUNNER_ALLOW_EXEC", false), CommandPolicy: commandPolicyFromEnv(), HTTP: &http.Client{Timeout: 15 * time.Second}, ModelHTTP: &http.Client{},
	}
	if r.ID == "" {
		r.ID = randomRunnerID()
	}
	if r.Token == "" {
		log.Fatal("RUNNER_TOKEN is required")
	}
	if r.JevMode != "off" && r.JevMode != "observe" && r.JevMode != "route" {
		log.Fatal("JEV_MODE must be off, observe, or route")
	}
	if r.JevMode != "off" && r.JevBaseURL == "" {
		log.Fatal("JEV_BASE_URL is required when JEV_MODE is enabled")
	}
	if r.JevThreshold <= 0 || r.JevThreshold > 1 {
		log.Fatal("JEV_MIN_CONFIDENCE must be in (0, 1]")
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
		task, err := decodeTaskMessage(message)
		if err != nil || task.TaskID == "" {
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

func decodeTaskMessage(message *amqp.Message) (taskMessage, error) {
	if message == nil {
		return taskMessage{}, errors.New("task message is nil")
	}
	data := message.GetData()
	if len(data) == 0 {
		switch value := message.Value.(type) {
		case string:
			data = []byte(value)
		case []byte:
			data = value
		default:
			return taskMessage{}, fmt.Errorf("unsupported task message body %T", message.Value)
		}
	}
	var task taskMessage
	if err := json.Unmarshal(data, &task); err != nil {
		return taskMessage{}, err
	}
	if task.TaskID == "" {
		return taskMessage{}, errors.New("task ID is missing")
	}
	return task, nil
}

func (r *runner) runTask(parent context.Context, task taskMessage) (runErr error) {
	if !taskIDPattern.MatchString(task.TaskID) {
		return errors.New("task ID must be a UUID")
	}
	if !validRepositoryURL(task.Repository) {
		return errors.New("repositoryUrl must be http or https")
	}
	if task.Attempt < 1 {
		return errors.New("task attempt must be positive")
	}
	claimed, err := r.claim(parent, task.TaskID, task.Attempt)
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
	go r.watchLease(ctx, task.TaskID, task.Attempt, cancel)
	collector := experiment.NewCollector(&httpEventSink{client: r.HTTP, baseURL: r.ControlPlane, token: r.Token}, task.SourceRevision)
	collector.Set("experimentGroup", task.ExperimentGroup)
	events := event.NewRunnerSink(collector, randomRunnerID(), r.ID, task.Attempt)
	defer func() {
		if runErr == nil {
			return
		}
		kind := event.TaskFailed
		if errors.Is(ctx.Err(), context.Canceled) {
			kind = event.TaskCancelled
		}
		payload := map[string]any{"error": runErr.Error()}
		if task.ExperimentGroup != "" {
			payload["experimentResult"] = collector.Report(false, runErr.Error())
		}
		if emitErr := events.Emit(context.Background(), task.TaskID, kind, payload); emitErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("publish terminal event: %w", emitErr))
		}
	}()
	if err := validateExperimentTask(task); err != nil {
		return err
	}
	configurationHash := r.configurationHash(task)

	base := filepath.Join(r.WorkspaceRoot, task.TaskID, fmt.Sprintf("attempt-%d", task.Attempt))
	metadataPath := filepath.Join(base, "run.json")
	keepWorkspace := true
	if err := os.MkdirAll(base, 0750); err != nil {
		return err
	}
	repository := filepath.Join(base, "repository")
	manager := worktree.Manager{Repository: repository, Root: filepath.Join(base, "worktrees")}
	var handle worktree.Handle
	var saved workspaceMetadata
	resumed := false
	if data, readErr := os.ReadFile(metadataPath); readErr == nil {
		if json.Unmarshal(data, &saved) == nil && saved.Repository == task.Repository && saved.Attempt == task.Attempt && existingHandle(base, saved.Handle) {
			if saved.ConfigurationHash != configurationHash {
				return errors.New("cannot reuse persisted task with a different execution configuration")
			}
			handle = saved.Handle
			resumed = true
			collector.Restore(saved.ExperimentState)
		}
	}
	if task.Resume && !resumed {
		return errors.New("cannot resume task: persisted worktree is unavailable")
	}
	if !resumed {
		if err := os.RemoveAll(base); err != nil {
			return err
		}
		if err := os.MkdirAll(base, 0750); err != nil {
			return err
		}
		if err := clone(ctx, task.Repository, task.Branch, repository); err != nil {
			return err
		}
		if err := checkoutRevision(ctx, repository, task.SourceRevision); err != nil {
			return err
		}
		manager = worktree.Manager{Repository: repository, Root: filepath.Join(base, "worktrees")}
		handle, err = manager.Create(ctx, task.TaskID)
		if err != nil {
			return err
		}
		saved = workspaceMetadata{Repository: task.Repository, Attempt: task.Attempt, Handle: handle, ConfigurationHash: configurationHash, ExperimentState: collector.Snapshot()}
		data, marshalErr := json.Marshal(saved)
		if marshalErr != nil {
			return marshalErr
		}
		if err := os.WriteFile(metadataPath, data, 0600); err != nil {
			return err
		}
	}
	if task.SourceRevision != "" && !strings.EqualFold(handle.Base, task.SourceRevision) {
		return errors.New("persisted worktree does not match the fixed source revision")
	}
	defer func() {
		if keepWorkspace {
			return
		}
		_ = manager.Remove(context.Background(), handle)
		_ = os.RemoveAll(base)
	}()
	defer func() {
		if runErr == nil {
			return
		}
		recoveryCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		patch, err := manager.WorkingPatch(recoveryCtx, handle)
		if err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("capture recovery patch: %w", err))
			return
		}
		if patch == "" {
			return
		}
		if len(patch) > 8<<20 {
			runErr = errors.Join(runErr, errors.New("recovery patch exceeds 8 MiB; workspace retained on runner volume"))
			return
		}
		if err := events.Emit(recoveryCtx, task.TaskID, event.RecoveryCreated, map[string]any{"branch": handle.Branch, "base": handle.Base, "patch": patch}); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("publish recovery patch: %w", err))
		}
	}()
	ws, err := workspace.Open(handle.Path)
	if err != nil {
		return err
	}
	coordinator, request, err := r.execution(task, ws, events)
	if err != nil {
		return err
	}
	if task.ExperimentGroup != "" {
		collector.Set("profileApplied", true)
	}
	writeState := func() error {
		saved.ExperimentState = collector.Snapshot()
		data, err := json.Marshal(saved)
		if err != nil {
			return err
		}
		tmp := metadataPath + ".tmp"
		file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		if _, err = file.Write(data); err != nil {
			_ = file.Close()
			return err
		}
		if err = file.Sync(); err != nil {
			_ = file.Close()
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, metadataPath)
	}
	if saved.InFlightCall != nil {
		return fmt.Errorf("tool call %s (%s) was interrupted with an unknown outcome; workspace was preserved for review", saved.InFlightCall.ID, saved.InFlightCall.Name)
	}
	request.Checkpoint = func(state agent.RunResult) error {
		saved.Messages = state.Messages
		saved.Usage = state.Usage
		saved.InFlightCall = nil
		saved.PendingCall = nil
		saved.RemainingCalls = state.RemainingCalls
		saved.ApprovalID = ""
		return writeState()
	}
	request.MainDone = func(state agent.RunResult) error {
		saved.MainCompleted = true
		saved.MainContent = state.Content
		saved.Messages = state.Messages
		saved.Usage = state.Usage
		return writeState()
	}
	request.Pause = func(state agent.RunResult, pause *agent.ApprovalRequiredError) error {
		saved.Messages = state.Messages
		saved.Usage = state.Usage
		saved.PendingCall = &model.ToolCall{ID: pause.CallID, Name: pause.Tool, Arguments: pause.Arguments}
		saved.RemainingCalls = pause.Remaining
		saved.ApprovalID = pause.CallID
		return writeState()
	}
	request.BeforeTool = func(state agent.RunResult, call model.ToolCall) error {
		saved.Messages = state.Messages
		saved.Usage = state.Usage
		saved.InFlightCall = &call
		return writeState()
	}
	if task.Resume || (resumed && len(saved.Messages) > 0 && saved.PendingCall == nil) {
		if !resumed || len(saved.Messages) == 0 {
			return errors.New("resume state is missing")
		}
		request.Resume = true
		request.InitialMessages = saved.Messages
		request.InitialUsage = saved.Usage
		request.RemainingCalls = saved.RemainingCalls
		request.MainCompleted = saved.MainCompleted
		request.MainResult = agent.RunResult{Content: saved.MainContent, Messages: saved.Messages, Usage: saved.Usage}
		if saved.PendingCall != nil {
			if !task.Resume || saved.ApprovalID != task.ApprovalID {
				return errors.New("approval decision does not match the pending call")
			}
			if task.Decision != "approved" && task.Decision != "denied" {
				return errors.New("resume decision is invalid")
			}
			approved := task.Decision == "approved"
			request.ResumeToolCall = saved.PendingCall
			request.ResumeApproved = &approved
		}
	} else if resumed && saved.PendingCall != nil {
		return errors.New("pending approval must be resolved before restarting")
	}
	result, err := coordinator.Run(ctx, request)
	if err != nil {
		var approvalErr *agent.ApprovalRequiredError
		if errors.As(err, &approvalErr) {
			keepWorkspace = true
			return nil
		}
		return err
	}
	if task.ExperimentGroup != "" {
		// A crash during evaluation leaves an unknown outcome. Never repeat a
		// command or patch whose outcome was not durably recorded.
		if saved.EvaluationStarted && !saved.EvaluationCompleted {
			return errors.New("experimental evaluator was interrupted with an unknown outcome")
		}
		if !saved.EvaluationCompleted {
			saved.EvaluationStarted = true
			if err := writeState(); err != nil {
				return err
			}
			applied, patchErr := applyGeneratedPatch(ctx, ws, result.Main.Content)
			if patchErr != nil {
				collector.Set("patchSucceeded", false)
				return patchErr
			}
			if applied {
				collector.Set("patchSucceeded", true)
			}
			testResult, testErr := evaluateTests(ctx, ws, task.TestCommand, r.CommandPolicy)
			// A real nonzero exit is a measured failure. A rejected command or
			// failure to start the executable has no measured test outcome.
			var passed any
			if code, ok := testResult.Metadata["exitCode"].(int); ok && (code >= 0 || testResult.Metadata["timedOut"] == true) {
				passed = testErr == nil && !testResult.IsError
				collector.Set("testsPassed", passed)
			}
			verification := map[string]any{"evaluator": "fixed-test-command", "command": task.TestCommand, "passed": passed, "output": testResult.Content, "metadata": testResult.Metadata}
			if testErr != nil {
				verification["error"] = testErr.Error()
			}
			if err := events.Emit(ctx, task.TaskID, event.VerificationDone, verification); err != nil {
				return err
			}
			saved.EvaluationCompleted = true
			if err := writeState(); err != nil {
				return err
			}
			if testErr != nil {
				return testErr
			}
		} else if saved.ExperimentState.Values["testsPassed"] != true {
			return errors.New("fixed experiment test command failed")
		}
	}
	completedPayload := func(payload map[string]any) map[string]any {
		if task.ExperimentGroup != "" {
			payload["experimentResult"] = collector.Report(true, "")
		}
		return payload
	}
	hash := saved.CheckpointHash
	if hash == "" {
		workingPatch, err := manager.WorkingPatch(ctx, handle)
		if err != nil {
			return err
		}
		if workingPatch == "" {
			if err := events.Emit(ctx, task.TaskID, event.TaskCompleted, completedPayload(map[string]any{"result": result.Main.Content, "branch": handle.Branch, "unchanged": true})); err != nil {
				return err
			}
			keepWorkspace = false
			return nil
		}
		hash, err = manager.Checkpoint(ctx, handle, "ProofCode checkpoint: task completed")
		if err != nil {
			return err
		}
		saved.CheckpointHash = hash
		if err := writeState(); err != nil {
			return err
		}
	}
	patch, err := manager.Patch(ctx, handle, hash)
	if err != nil {
		return err
	}
	if len(patch) > 8<<20 {
		return fmt.Errorf("checkpoint patch exceeds 8 MiB limit")
	}
	if patch != "" {
		if err := events.Emit(ctx, task.TaskID, event.CheckpointCreated, map[string]any{"commit": hash, "branch": handle.Branch, "patch": patch, "base": handle.Base}); err != nil {
			return fmt.Errorf("publish checkpoint: %w", err)
		}
	}
	if err := events.Emit(ctx, task.TaskID, event.TaskCompleted, completedPayload(map[string]any{"result": result.Main.Content, "commit": hash, "branch": handle.Branch, "unchanged": patch == ""})); err != nil {
		return err
	}
	keepWorkspace = false
	return nil
}

func existingHandle(base string, handle worktree.Handle) bool {
	path, err := filepath.Abs(handle.Path)
	if err != nil {
		return false
	}
	root, err := filepath.Abs(filepath.Join(base, "worktrees"))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if _, err := os.Stat(filepath.Join(base, "repository")); err != nil {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return true
}

func (r *runner) claim(ctx context.Context, taskID string, attempt int) (bool, error) {
	for {
		response, err := r.request(ctx, http.MethodPost, "/internal/tasks/"+taskID+"/claim", map[string]any{"runnerId": r.ID, "attempt": attempt})
		if err != nil {
			return false, err
		}
		if response.StatusCode/100 == 2 {
			response.Body.Close()
			return true, nil
		}
		if response.StatusCode != http.StatusConflict {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			response.Body.Close()
			return false, fmt.Errorf("claim task: %s: %s", response.Status, strings.TrimSpace(string(body)))
		}
		response.Body.Close()
		status, err := r.taskStatus(ctx, taskID)
		if err != nil {
			return false, fmt.Errorf("inspect claim conflict: %w", err)
		}
		if status.Attempt > attempt || (status.Attempt == attempt && terminalForDelivery(status.Status)) {
			return false, nil
		}
		if status.Attempt < attempt {
			return false, fmt.Errorf("task attempt %d is newer than control-plane attempt %d", attempt, status.Attempt)
		}
		wait := 2 * time.Second
		if status.LeaseUntil != nil && (status.Status == "RUNNING" || status.Status == "VERIFYING") {
			untilExpiry := time.Until(*status.LeaseUntil) + 100*time.Millisecond
			wait = min(max(untilExpiry, 100*time.Millisecond), 30*time.Second)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
}

func terminalForDelivery(status string) bool {
	switch status {
	case "WAITING_APPROVAL", "SUCCEEDED", "FAILED", "CANCELLED":
		return true
	default:
		return false
	}
}

func (r *runner) taskStatus(ctx context.Context, taskID string) (statusResponse, error) {
	response, err := r.request(ctx, http.MethodGet, "/internal/tasks/"+taskID, nil)
	if err != nil {
		return statusResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return statusResponse{}, fmt.Errorf("task status returned %s", response.Status)
	}
	var status statusResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&status); err != nil {
		return statusResponse{}, err
	}
	if status.Attempt < 1 || status.Status == "" {
		return statusResponse{}, errors.New("task status is incomplete")
	}
	return status, nil
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

func (r *runner) watchLease(ctx context.Context, taskID string, attempt int, cancel context.CancelFunc) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			response, err := r.request(ctx, http.MethodPost, "/internal/tasks/"+taskID+"/renew", map[string]any{"runnerId": r.ID, "attempt": attempt})
			if err != nil {
				failures++
				if failures >= 3 {
					cancel()
					return
				}
				continue
			}
			response.Body.Close()
			if response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusNotFound {
				cancel()
				return
			}
			if response.StatusCode/100 != 2 {
				failures++
				if failures >= 3 {
					cancel()
					return
				}
				continue
			}
			failures = 0
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
				if response.StatusCode == http.StatusAccepted {
					var accepted struct {
						Sequence int64 `json:"sequence"`
					}
					if json.Unmarshal(body, &accepted) != nil || accepted.Sequence < 1 {
						return errors.New("event ingest returned no durable sequence")
					}
				}
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
	if err != nil && strings.Contains(string(output), "dumb http transport does not support shallow capabilities") {
		args = []string{"clone"}
		if branch != "" {
			args = append(args, "--branch", branch)
		}
		args = append(args, repository, destination)
		command = exec.CommandContext(ctx, "git", args...)
		output, err = command.CombinedOutput()
	}
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

func envFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		log.Fatalf("%s must be a number: %v", key, err)
	}
	return parsed
}

func randomRunnerID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		log.Fatalf("generate runner ID: %v", err)
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[0:4], data[4:6], data[6:8], data[8:10], data[10:16])
}

func commandPolicyFromEnv() tool.CommandPolicy {
	policy := tool.DefaultCommandPolicy()
	if value := strings.TrimSpace(os.Getenv("RUNNER_ALLOWED_PROGRAMS")); value != "" {
		allowed := make(map[string]bool)
		for _, item := range strings.Split(value, ",") {
			name := strings.ToLower(strings.TrimSpace(item))
			if name != "" {
				allowed[name] = true
			}
		}
		if len(allowed) > 0 {
			policy.AllowedPrograms = allowed
		}
	}
	if seconds := strings.TrimSpace(os.Getenv("RUNNER_MAX_COMMAND_SECONDS")); seconds != "" {
		if value, err := time.ParseDuration(seconds + "s"); err == nil && value > 0 {
			policy.MaxDuration = value
		}
	}
	return policy
}
