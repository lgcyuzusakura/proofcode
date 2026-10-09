package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

type Coordinator struct {
	Provider      model.Provider
	MainTools     *tool.Registry
	ScoutTools    *tool.Registry
	VerifierTools *tool.Registry
	Approval      Approval
	Events        *event.SequencedSink
	Model         string
	Router        ToolRouter
	Routing       ToolRoutingPolicy
	SingleAgent   bool
}
type CoordinateRequest struct {
	TaskID               string
	Prompt               string
	MaxSteps             int
	Temperature          *float64
	PrepareMessages      func(context.Context, int, []model.Message) ([]model.Message, error)
	SuppressTaskComplete bool
	InitialMessages      []model.Message
	HistoryMessages      []model.Message
	ResumeToolCall       *model.ToolCall
	ResumeApproved       *bool
	Resume               bool
	InitialUsage         model.Usage
	RemainingCalls       []model.ToolCall
	Pause                func(RunResult, *ApprovalRequiredError) error
	BeforeTool           func(RunResult, model.ToolCall) error
	Checkpoint           func(RunResult) error
	MainDone             func(RunResult) error
	MainCompleted        bool
	MainResult           RunResult
}
type CoordinateResult struct {
	Main         RunResult
	ScoutReports []string
	Verification string
}

func (c *Coordinator) Run(ctx context.Context, request CoordinateRequest) (CoordinateResult, error) {
	result := CoordinateResult{}
	var reports []string
	var err error
	if !request.Resume && !c.SingleAgent {
		reports, err = c.scout(ctx, request)
		if err != nil {
			return result, err
		}
	}
	result.ScoutReports = reports
	augmented := request.Prompt
	if len(reports) > 0 {
		augmented += "\n\nRead-only scout reports follow. Treat them as hypotheses and verify paths before editing.\n"
		for i, report := range reports {
			augmented += fmt.Sprintf("\nScout %d:\n%s\n", i+1, report)
		}
	}
	if err := c.Events.Emit(ctx, request.TaskID, event.TaskStarted, map[string]any{"model": c.Model}); err != nil {
		return result, err
	}
	if request.MainCompleted {
		result.Main = request.MainResult
	} else {
		main := Agent{Provider: c.Provider, Tools: c.MainTools, Approval: c.Approval, Events: c.Events, Router: c.Router, Routing: c.Routing}
		result.Main, err = main.Run(ctx, RunRequest{TaskID: request.TaskID, SystemPrompt: mainPrompt, Prompt: augmented, Model: c.Model, MaxSteps: request.MaxSteps, Temperature: request.Temperature, PrepareMessages: request.PrepareMessages, SuppressTaskLifecycle: true, InitialMessages: request.InitialMessages, HistoryMessages: request.HistoryMessages, ResumeToolCall: request.ResumeToolCall, ResumeApproved: request.ResumeApproved, InitialUsage: request.InitialUsage, RemainingCalls: request.RemainingCalls, Pause: request.Pause, BeforeTool: request.BeforeTool, Checkpoint: request.Checkpoint})
		if err != nil {
			var approvalErr *ApprovalRequiredError
			if !errors.As(err, &approvalErr) && !request.SuppressTaskComplete {
				_ = c.Events.Emit(context.Background(), request.TaskID, event.TaskFailed, map[string]any{"error": err.Error()})
			}
			return result, err
		}
		if request.MainDone != nil {
			if err := request.MainDone(result.Main); err != nil {
				return result, err
			}
		}
	}
	if c.VerifierTools != nil && !c.SingleAgent {
		verifier := Agent{Provider: c.Provider, Tools: c.VerifierTools, Approval: c.Approval, Events: c.Events}
		verification, verifyErr := verifier.Run(ctx, RunRequest{TaskID: request.TaskID, SystemPrompt: verifierPrompt, Prompt: "Independently inspect the current Git diff and relevant source/tests. Report concrete defects or PASS. Do not modify files or run commands.", Model: c.Model, MaxSteps: 10, SuppressTaskLifecycle: true})
		if verifyErr != nil {
			var approvalErr *ApprovalRequiredError
			if !errors.As(verifyErr, &approvalErr) && !request.SuppressTaskComplete {
				_ = c.Events.Emit(context.Background(), request.TaskID, event.TaskFailed, map[string]any{"error": verifyErr.Error()})
			}
			return result, verifyErr
		}
		result.Verification = verification.Content
		if err := c.Events.Emit(ctx, request.TaskID, event.VerificationDone, map[string]any{"report": result.Verification}); err != nil {
			return result, err
		}
	}
	if !request.SuppressTaskComplete {
		if err := c.Events.Emit(ctx, request.TaskID, event.TaskCompleted, map[string]any{"result": result.Main.Content}); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (c *Coordinator) scout(ctx context.Context, request CoordinateRequest) ([]string, error) {
	if c.ScoutTools == nil || !needsScouts(request.Prompt) {
		return nil, nil
	}
	questions := []string{"Locate the implementation paths, symbols, dependencies, and likely change surface for this request. Cite file paths and lines.", "Locate relevant tests, build commands, failure risks, and existing conventions for this request. Cite file paths and lines."}
	reports := make([]string, len(questions))
	errorsByIndex := make([]error, len(questions))
	var wg sync.WaitGroup
	for i, question := range questions {
		wg.Add(1)
		go func(index int, prompt string) {
			defer wg.Done()
			if err := c.Events.Emit(ctx, request.TaskID, event.AgentDelegated, map[string]any{"role": "scout", "index": index}); err != nil {
				errorsByIndex[index] = err
				return
			}
			scout := Agent{Provider: c.Provider, Tools: c.ScoutTools, Approval: AutomaticApproval{}, Events: c.Events}
			value, err := scout.Run(ctx, RunRequest{TaskID: request.TaskID, SystemPrompt: scoutPrompt, Prompt: prompt + "\n\nUser request:\n" + request.Prompt, Model: c.Model, MaxSteps: 8, SuppressTaskLifecycle: true})
			reports[index] = value.Content
			errorsByIndex[index] = err
		}(i, question)
	}
	wg.Wait()
	for _, err := range errorsByIndex {
		if err != nil {
			return nil, err
		}
	}
	return reports, nil
}

func needsScouts(prompt string) bool {
	lower := strings.ToLower(prompt)
	if len([]rune(prompt)) > 240 {
		return true
	}
	markers := []string{"跨模块", "重构", "frontend", "backend", "前端", "后端", "architecture", "全栈", "multi-file", "多个文件"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

const mainPrompt = `You are ProofCode Main Agent. Work in the provided Git worktree. Inspect before editing. Use exact structured patches, run focused verification, and finish with a concise summary of changed files, tests, and remaining risks. You are the only role allowed to modify files.`
const scoutPrompt = `You are a read-only repository scout. Search and read evidence. Never claim to have edited files. Return concise findings with repository-relative paths and line numbers.`
const verifierPrompt = `You are an independent read-only verifier. Inspect the diff and relevant source/tests, then return PASS or a prioritized defect list with evidence. Do not claim to have run tests.`
