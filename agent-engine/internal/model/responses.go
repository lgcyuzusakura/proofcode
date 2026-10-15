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

type responseItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}
type responseEnvelope struct {
	Status string         `json:"status"`
	Output []responseItem `json:"output"`
	Usage  *struct {
		Input   int `json:"input_tokens"`
		Output  int `json:"output_tokens"`
		Details *struct {
			Cached *int `json:"cached_tokens"`
			Write  *int `json:"cache_creation_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
}

func responseResult(v responseEnvelope) (Response, error) {
	if v.Status != "completed" {
		return Response{}, errors.New("Responses API returned an incomplete or failed response")
	}
	r := Response{}
	for _, item := range v.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				if c.Type == "output_text" {
					r.Content += c.Text
				}
			}
		case "function_call":
			if item.CallID == "" || item.Name == "" || !json.Valid([]byte(item.Arguments)) {
				return Response{}, errors.New("Responses API returned an invalid tool call")
			}
			r.ToolCalls = append(r.ToolCalls, ToolCall{ID: item.CallID, Name: item.Name, Arguments: json.RawMessage(item.Arguments)})
		}
	}
	if r.Content == "" && len(r.ToolCalls) == 0 {
		return r, errors.New("Responses API returned no content or tool calls")
	}
	if v.Usage != nil {
		r.Usage = Usage{InputTokens: v.Usage.Input, OutputTokens: v.Usage.Output, Reported: true}
		if v.Usage.Details != nil {
			if v.Usage.Details.Cached != nil {
				r.Usage.CacheReported = true
				r.Usage.CachedInputTokens = *v.Usage.Details.Cached
			}
			if v.Usage.Details.Write != nil {
				r.Usage.CacheWriteReported = true
				r.Usage.CacheWriteTokens = *v.Usage.Details.Write
			}
		}
	}
	return r, nil
}
func (p *OpenAICompatible) responses(ctx context.Context, request Request, onDelta func(string)) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	input := []map[string]any{}
	for _, m := range request.Messages {
		if m.Role == RoleTool {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content})
			continue
		}
		if m.Content != "" {
			input = append(input, map[string]any{"role": string(m.Role), "content": m.Content})
		}
		for _, c := range m.ToolCalls {
			input = append(input, map[string]any{"type": "function_call", "call_id": c.ID, "name": c.Name, "arguments": string(c.Arguments)})
		}
	}
	tools := []map[string]any{}
	for _, t := range request.Tools {
		tools = append(tools, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.Parameters, "strict": false})
	}
	settings := p.ModelSettings()
	max := request.MaxTokens
	if max <= 0 || max > settings.MaxOutputTokens {
		max = settings.MaxOutputTokens
	}
	payload := map[string]any{"model": request.Model, "input": input, "stream": true, "store": false, "max_output_tokens": max}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}
	if p.Settings != nil && config.EstimateTokens(string(body)) > settings.MaxInputTokens {
		return Response{}, fmt.Errorf("request exceeds estimated %d input token limit", settings.MaxInputTokens)
	}
	// This is an estimate, not a tokenizer guarantee. Leave a large default
	// margin; a provider context-length rejection remains an explicit error.
	if config.EstimateTokens(string(body))+max+256 > settings.ContextTokens {
		return Response{}, fmt.Errorf("request exceeds estimated %d token ceiling", settings.ContextTokens)
	}
	// UTF-8 bytes provide a deliberately conservative additional bound for
	// byte-based tokenizers. The UI continues to label budget counts as estimates.
	if p.Settings == nil && len(body)+max+4096 > settings.ContextTokens {
		return Response{}, errors.New("request byte size exceeds the context safety margin; retrieve smaller excerpts")
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, "POST", strings.TrimRight(p.BaseURL, "/")+"/responses", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(req)
	if err != nil {
		return Response{}, errors.New("Responses API connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return Response{}, fmt.Errorf("Responses API returned HTTP %d", resp.StatusCode)
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		b, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
		if err != nil || len(b) > 8<<20 {
			return Response{}, errors.New("Responses API response exceeded limit")
		}
		var v responseEnvelope
		if json.Unmarshal(b, &v) != nil {
			return Response{}, errors.New("invalid Responses API JSON")
		}
		r, err := responseResult(v)
		if err == nil && onDelta != nil {
			onDelta(r.Content)
		}
		return r, err
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	streamed := false
	events := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		events++
		if events > 20000 {
			return Response{}, errors.New("Responses API event limit exceeded")
		}
		var e struct {
			Type     string           `json:"type"`
			Delta    string           `json:"delta"`
			Response responseEnvelope `json:"response"`
		}
		if json.Unmarshal([]byte(data), &e) != nil {
			return Response{}, errors.New("invalid Responses API stream")
		}
		switch e.Type {
		case "response.output_text.delta":
			streamed = true
			if onDelta != nil {
				onDelta(e.Delta)
			}
		case "response.completed":
			r, err := responseResult(e.Response)
			if err == nil && !streamed && onDelta != nil {
				onDelta(r.Content)
			}
			return r, err
		case "response.failed", "response.incomplete", "error":
			return Response{}, errors.New("Responses API failed or hit its output limit")
		}
	}
	if scanner.Err() != nil {
		return Response{}, errors.New("Responses API stream interrupted")
	}
	return Response{}, errors.New("Responses API stream ended before completion")
}
