package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/agent"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

func (r *runner) conversationHistory(ctx context.Context, task taskMessage) ([]model.Message, error) {
	if task.ConversationID == "" || task.ExperimentGroup != "" {
		return nil, nil
	}
	response, err := r.request(ctx, http.MethodGet, "/internal/tasks/"+task.TaskID+"/conversation?attempt="+strconv.Itoa(task.Attempt), nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if task.ContextSelectionHash != "" && response.Header.Get("X-Context-Selection-Hash") != task.ContextSelectionHash {
		return nil, errors.New("conversation snapshot does not match task context")
	}
	if response.StatusCode/100 != 2 {
		return nil, fmt.Errorf("conversation history returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 4<<20 {
		return nil, errors.New("conversation history exceeds protected context limit")
	}
	var values []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err = json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	messages := make([]model.Message, 0, len(values))
	for _, value := range values {
		if value.Role != "user" && value.Role != "assistant" {
			return nil, errors.New("conversation contains an unsupported message role")
		}
		messages = append(messages, model.Message{Role: model.Role(value.Role), Content: value.Content})
	}
	return messages, nil
}

func (r *runner) resolveSourceAttachments(ctx context.Context, task *taskMessage) error {
	if task.SourceAttachmentHash == "" {
		task.sourceAttachments = ""
		return nil
	}
	response, err := r.request(ctx, http.MethodGet, "/internal/tasks/"+task.TaskID+"/source-attachments?attempt="+strconv.Itoa(task.Attempt)+"&runnerId="+url.QueryEscape(r.ID), nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("source attachments returned %s", response.Status)
	}
	if response.Header.Get("X-Source-Attachment-Hash") != task.SourceAttachmentHash {
		return errors.New("source attachment snapshot does not match task")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil {
		return err
	}
	if len(data) > 64<<10 {
		return errors.New("source attachment snapshot exceeds limit")
	}
	if hashString(string(data)) != task.SourceAttachmentHash {
		return errors.New("source attachment snapshot checksum mismatch")
	}
	task.sourceAttachments = string(data)
	return nil
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validateContextResume(taskHash, savedHash string) error {
	if taskHash != savedHash {
		return errors.New("cannot resume checkpoint with a different or missing context snapshot; start a new task")
	}
	return nil
}
func (r *runner) runChat(ctx context.Context, task taskMessage, events *event.SequencedSink) error {
	history, err := r.conversationHistory(ctx, task)
	if err != nil {
		return err
	}
	systemPrompt := "You are ProofCode. Answer the user's questions using the conversation history. This is a chat session: no file, shell, database, cache or network execution tools are available. skill_read, when present, only reads this task's saved workflow instructions. Describe suggestions accurately and never claim to have run actions."
	tools := tool.NewRegistry()
	task.agent.registerSkills(tools)
	maxSteps := 1
	if len(tools.Definitions()) > 0 {
		maxSteps = 8
	}
	if task.agent != nil {
		systemPrompt += task.agent.instructions()
		if err = events.Emit(ctx, task.TaskID, "agent.configuration", map[string]any{"profileId": task.agent.ProfileID, "name": task.agent.ProfileName, "version": task.agent.ProfileVersion, "readOnly": true, "memories": task.agent.Memories, "personalization": task.agent.Personalization, "skills": task.agent.skillCatalog(), "snapshotHash": task.AgentSnapshotHash}); err != nil {
			return err
		}
	}
	chat := agent.Agent{Provider: r.modelProvider(task), Tools: tools, Approval: agent.AutomaticApproval{}, Events: events}
	if err = events.Emit(ctx, task.TaskID, event.TaskStarted, map[string]any{"model": task.Model, "executionMode": "CHAT", "tools": tools.Definitions(), "readOnly": true}); err != nil {
		return err
	}
	result, err := chat.Run(ctx, agent.RunRequest{TaskID: task.TaskID, Model: task.Model, MaxSteps: maxSteps, Temperature: task.Temperature, SystemPrompt: systemPrompt, Prompt: task.Prompt + task.sourceAttachments, HistoryMessages: history, SuppressTaskLifecycle: true})
	if err != nil {
		return err
	}
	return events.Emit(ctx, task.TaskID, event.TaskCompleted, map[string]any{"result": result.Content, "executionMode": "CHAT"})
}
