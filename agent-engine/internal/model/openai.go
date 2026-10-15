package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/config"
)

const defaultRequestTimeout = 5 * time.Minute

type OpenAICompatible struct {
	BaseURL  string
	APIKey   string
	Client   *http.Client
	Timeout  time.Duration
	WireAPI  string
	Settings *Settings
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

func (p *OpenAICompatible) Chat(ctx context.Context, request Request, onDelta func(string)) (result Response, err error) {
	settings := p.ModelSettings()
	if err = settings.Validate(); err != nil {
		return result, err
	}
	started := time.Now()
	var first *float64
	delta := func(text string) {
		if text != "" && first == nil {
			v := float64(time.Since(started)) / float64(time.Millisecond)
			first = &v
		}
		if onDelta != nil {
			onDelta(text)
		}
	}
	defer func() {
		result.Usage.DurationMs = float64(time.Since(started)) / float64(time.Millisecond)
		result.Usage.FirstTokenMs = first
		if result.Usage.Reported && first != nil && result.Usage.DurationMs > *first && result.Usage.OutputTokens > 0 {
			v := float64(result.Usage.OutputTokens) / ((result.Usage.DurationMs - *first) / 1000)
			result.Usage.TokensPerSecond = &v
		}
		settings.price(&result.Usage)
	}()
	if p.WireAPI == "responses" {
		return p.responses(ctx, request, delta)
	}
	return p.chat(ctx, request, delta)
}
func (p *OpenAICompatible) chat(ctx context.Context, request Request, onDelta func(string)) (Response, error) {
	if p.WireAPI == "responses" {
		return p.responses(ctx, request, onDelta)
	}
	if p.WireAPI != "" && p.WireAPI != "chat" {
		return Response{}, errors.New("unsupported model wire API")
	}
	if ctx == nil {
		ctx = context.Background()
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
	if len(tools) == 0 {
		delete(payload, "tools")
	}
	inputEstimate := 0
	for _, message := range request.Messages {
		toolData, _ := json.Marshal(message.ToolCalls)
		inputEstimate += config.EstimateTokens(message.Content, string(toolData))
	}
	toolDefinitions, err := json.Marshal(tools)
	if err != nil {
		return Response{}, fmt.Errorf("encode tool definitions: %w", err)
	}
	inputEstimate += config.EstimateTokens(string(toolDefinitions))
	settings := p.ModelSettings()
	if p.Settings != nil && inputEstimate > settings.MaxInputTokens {
		return Response{}, fmt.Errorf("request exceeds estimated %d input token limit", settings.MaxInputTokens)
	}
	if inputEstimate > settings.ContextTokens-settings.MaxOutputTokens {
		return Response{}, fmt.Errorf("当前请求预计需要 %d token，超过 %d token 总预算，请减少上下文或拆分任务。", inputEstimate, settings.ContextTokens)
	}
	maxTokens := request.MaxTokens
	if maxTokens <= 0 || maxTokens > settings.MaxOutputTokens {
		maxTokens = settings.MaxOutputTokens
	}
	remaining := settings.ContextTokens - inputEstimate
	if maxTokens > remaining {
		maxTokens = remaining
	}
	if maxTokens < 1 {
		return Response{}, fmt.Errorf("输入上下文已占满 %d token 总预算", settings.ContextTokens)
	}
	payload["max_tokens"] = maxTokens
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}
	if p.Settings == nil && len(body)+maxTokens+4096 > config.MaxTotalTokens {
		return Response{}, errors.New("request byte size exceeds the context safety margin; retrieve smaller excerpts")
	}
	base := strings.TrimRight(p.BaseURL, "/")
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<10))
		return Response{}, fmt.Errorf("model API returned HTTP %d", resp.StatusCode)
	}

	var result Response
	toolFragments := map[int]*openAIToolCall{}
	sawChoice, sawFinish, sawDone := false, false, false
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			sawDone = true
			break
		}
		var chunk struct {
			Choices []struct {
				FinishReason *string `json:"finish_reason"`
				Delta        struct {
					Content   string           `json:"content"`
					ToolCalls []openAIToolCall `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int  `json:"prompt_tokens"`
				CacheHit         *int `json:"prompt_cache_hit_tokens"`
				CacheWrite       *int `json:"cache_creation_input_tokens"`
				CompletionTokens int  `json:"completion_tokens"`
				Details          *struct {
					Cached *int `json:"cached_tokens"`
					Write  *int `json:"cache_creation_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return result, fmt.Errorf("decode model stream: %w", err)
		}
		if chunk.Error != nil {
			message := chunk.Error.Message
			if p.APIKey != "" {
				message = strings.ReplaceAll(message, p.APIKey, "[redacted]")
			}
			return result, fmt.Errorf("model stream error: %s", message)
		}
		if chunk.Usage != nil {
			result.Usage = Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens, Reported: true}
			if chunk.Usage.CacheHit != nil {
				result.Usage.CachedInputTokens = *chunk.Usage.CacheHit
				result.Usage.CacheReported = true
			}
			if chunk.Usage.CacheWrite != nil {
				result.Usage.CacheWriteTokens = *chunk.Usage.CacheWrite
				result.Usage.CacheWriteReported = true
			}
			if chunk.Usage.Details != nil {
				if chunk.Usage.Details.Cached != nil {
					result.Usage.CachedInputTokens = *chunk.Usage.Details.Cached
					result.Usage.CacheReported = true
				}
				if chunk.Usage.Details.Write != nil {
					result.Usage.CacheWriteTokens = *chunk.Usage.Details.Write
					result.Usage.CacheWriteReported = true
				}
			}
		}
		for _, choice := range chunk.Choices {
			sawChoice = true
			if choice.FinishReason != nil {
				sawFinish = true
				if *choice.FinishReason == "length" || *choice.FinishReason == "content_filter" {
					return result, fmt.Errorf("model stream stopped with finish_reason %q", *choice.FinishReason)
				}
			}
			if choice.Delta.Content != "" {
				result.Content += choice.Delta.Content
				if onDelta != nil {
					onDelta(choice.Delta.Content)
				}
			}
			for _, fragment := range choice.Delta.ToolCalls {
				if fragment.Index < 0 {
					return result, errors.New("model stream returned a negative tool call index")
				}
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
		return result, err
	}
	if !sawChoice || (!sawFinish && !sawDone) {
		return result, errors.New("model stream ended before a complete response")
	}
	if result.Content == "" && len(toolFragments) == 0 {
		return result, errors.New("model stream returned no content or tool calls")
	}
	indices := make([]int, 0, len(toolFragments))
	for index := range toolFragments {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		item := toolFragments[index]
		if item.ID == "" || item.Function.Name == "" {
			return result, fmt.Errorf("model returned incomplete tool call at index %d", index)
		}
		arguments := json.RawMessage(item.Function.Arguments)
		if !json.Valid(arguments) {
			return result, fmt.Errorf("tool %s returned invalid JSON arguments", item.Function.Name)
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{ID: item.ID, Name: item.Function.Name, Arguments: arguments})
	}
	return result, nil
}
