package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
func (r *runner) runChat(ctx context.Context, task taskMessage, events *event.SequencedSink) error {
	history, err := r.conversationHistory(ctx, task)
	if err != nil {
		return err
	}
	systemPrompt := "You are ProofCode. Answer the user's questions using the conversation history. This is a chat session: no file, shell, database, cache or network execution tools are available. Describe suggestions accurately and never claim to have run actions."
	client := r.ModelHTTP
	if client == nil {
		client = &http.Client{}
	}
	chat := agent.Agent{Provider: &model.OpenAICompatible{BaseURL: r.ModelBaseURL, APIKey: r.ModelAPIKey, Client: client}, Tools: tool.NewRegistry(), Approval: agent.AutomaticApproval{}, Events: events}
	if err = events.Emit(ctx, task.TaskID, event.TaskStarted, map[string]any{"model": task.Model, "executionMode": "CHAT"}); err != nil {
		return err
	}
	result, err := chat.Run(ctx, agent.RunRequest{TaskID: task.TaskID, Model: task.Model, MaxSteps: 1, Temperature: task.Temperature, SystemPrompt: systemPrompt, Prompt: task.Prompt, HistoryMessages: history, SuppressTaskLifecycle: true})
	if err != nil {
		return err
	}
	return events.Emit(ctx, task.TaskID, event.TaskCompleted, map[string]any{"result": result.Content, "executionMode": "CHAT"})
}
