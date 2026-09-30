package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/config"
	"io"
	"net/http"
	"strings"
)

type OpenAICompatible struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

func (p *OpenAICompatible) Chat(ctx context.Context, request Request, onDelta func(string)) (Response, error) {
	if p.APIKey == "" {
		return Response{}, errors.New("model API key is not configured")
	}
	messages := make([]openAIMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		converted := openAIMessage{Role: string(message.Role), Content: message.Content, ToolCallID: message.ToolCallID}
		for _, call := range message.ToolCalls {
			item := openAIToolCall{ID: call.ID, Type: "function"}
			item.Function.Name = call.Name
			item.Function.Arguments = string(call.Arguments)
			converted.ToolCalls = append(converted.ToolCalls, item)
		}
		messages = append(messages, converted)
	}
	tools := make([]map[string]any, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, map[string]any{"type": "function", "function": tool})
	}
	payload := map[string]any{
		"model": request.Model, "messages": messages, "tools": tools,
		"temperature": request.Temperature, "stream": true,
		"stream_options": map[string]any{"include_usage": true},
	}
	inputEstimate := 0
	for _, message := range request.Messages {
		toolData, _ := json.Marshal(message.ToolCalls)
		inputEstimate += config.EstimateTokens(message.Content, string(toolData))
	}
	if inputEstimate > config.MaxTotalTokens-config.DefaultMaxOutputTokens {
		return Response{}, fmt.Errorf("当前请求预计需要 %d token，超过 %d token 总预算，请减少上下文或拆分任务。", inputEstimate, config.MaxTotalTokens)
	}
	maxTokens := request.MaxTokens
	if maxTokens <= 0 || maxTokens > config.DefaultMaxOutputTokens {
		maxTokens = config.DefaultMaxOutputTokens
	}
	remaining := config.MaxTotalTokens - inputEstimate
	if maxTokens > remaining {
		maxTokens = remaining
	}
	if maxTokens < 1 {
		return Response{}, errors.New("输入上下文已占满 200000 token 总预算")
	}
	payload["max_tokens"] = maxTokens
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}
	base := strings.TrimRight(p.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		return Response{}, fmt.Errorf("model API returned %s: %s", resp.Status, strings.TrimSpace(string(limited)))
	}

	var result Response
	toolFragments := map[int]*openAIToolCall{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string           `json:"content"`
					ToolCalls []openAIToolCall `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return Response{}, fmt.Errorf("decode model stream: %w", err)
		}
		result.Usage = Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				result.Content += choice.Delta.Content
				if onDelta != nil {
					onDelta(choice.Delta.Content)
				}
			}
			for _, fragment := range choice.Delta.ToolCalls {
				item := toolFragments[fragment.Index]
				if item == nil {
					item = &openAIToolCall{Index: fragment.Index}
					toolFragments[fragment.Index] = item
				}
				if fragment.ID != "" {
					item.ID = fragment.ID
				}
				if fragment.Function.Name != "" {
					item.Function.Name += fragment.Function.Name
				}
				item.Function.Arguments += fragment.Function.Arguments
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Response{}, err
	}
	for index := 0; index < len(toolFragments); index++ {
		item := toolFragments[index]
		if item == nil {
			continue
		}
		arguments := json.RawMessage(item.Function.Arguments)
		if !json.Valid(arguments) {
			return Response{}, fmt.Errorf("tool %s returned invalid JSON arguments", item.Function.Name)
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{ID: item.ID, Name: item.Function.Name, Arguments: arguments})
	}
	return result, nil
}
