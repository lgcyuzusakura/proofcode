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
	"strings"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/config"
)

type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}
type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}
type anthropicUsage struct {
	Input  *int `json:"input_tokens"`
	Output *int `json:"output_tokens"`
	Read   *int `json:"cache_read_input_tokens"`
	Write  *int `json:"cache_creation_input_tokens"`
}
type anthropicEnvelope struct {
	Type       string           `json:"type"`
	Content    []anthropicBlock `json:"content"`
	StopReason string           `json:"stop_reason"`
	Usage      anthropicUsage   `json:"usage"`
}

func anthropicHistory(messages []Message) ([]anthropicMessage, string, error) {
	var result []anthropicMessage
	var system []string
	for _, message := range messages {
		if message.Role == RoleSystem {
			system = append(system, message.Content)
			continue
		}
		role := "user"
		var blocks []anthropicBlock
		switch message.Role {
		case RoleUser:
			if message.Content != "" {
				blocks = append(blocks, anthropicBlock{Type: "text", Text: message.Content})
			}
		case RoleAssistant:
			role = "assistant"
			if message.Content != "" {
				blocks = append(blocks, anthropicBlock{Type: "text", Text: message.Content})
			}
			for _, call := range message.ToolCalls {
				var input map[string]any
				if call.ID == "" || call.Name == "" || json.Unmarshal(call.Arguments, &input) != nil || input == nil {
					return nil, "", errors.New("invalid Anthropic tool history")
				}
				blocks = append(blocks, anthropicBlock{Type: "tool_use", ID: call.ID, Name: call.Name, Input: call.Arguments})
			}
		case RoleTool:
			if message.ToolCallID == "" {
				return nil, "", errors.New("missing Anthropic tool result ID")
			}
			var status struct {
				IsError bool `json:"isError"`
			}
			_ = json.Unmarshal([]byte(message.Content), &status)
			blocks = append(blocks, anthropicBlock{Type: "tool_result", ToolUseID: message.ToolCallID, Content: message.Content, IsError: status.IsError})
		default:
			return nil, "", errors.New("unsupported Anthropic message role")
		}
		if len(blocks) == 0 {
			continue
		}
		if len(result) > 0 && result[len(result)-1].Role == role {
			result[len(result)-1].Content = append(result[len(result)-1].Content, blocks...)
		} else {
			result = append(result, anthropicMessage{Role: role, Content: blocks})
		}
	}
	if len(result) == 0 || result[0].Role != "user" {
		return nil, "", errors.New("Anthropic history must begin with a user message")
	}
	return result, strings.Join(system, "\n\n"), nil
}

func (p *OpenAICompatible) anthropic(ctx context.Context, request Request, onDelta func(string)) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	messages, system, err := anthropicHistory(request.Messages)
	if err != nil {
		return Response{}, err
	}
	settings := p.ModelSettings()
	max := request.MaxTokens
	if max <= 0 || max > settings.MaxOutputTokens {
		max = settings.MaxOutputTokens
	}
	payload := map[string]any{"model": request.Model, "messages": messages, "max_tokens": max, "stream": true}
	if system != "" {
		payload["system"] = system
	}
	// Temperature is deliberately omitted: some Anthropic models prohibit it.
	if len(request.Tools) > 0 {
		tools := make([]map[string]any, 0, len(request.Tools))
		for _, definition := range request.Tools {
			tools = append(tools, map[string]any{"name": definition.Name, "description": definition.Description, "input_schema": definition.Parameters})
		}
		payload["tools"] = tools
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, errors.New("invalid Anthropic request")
	}
	estimate := config.EstimateTokens(string(body))
	if p.Settings != nil && estimate > settings.MaxInputTokens {
		return Response{}, errors.New("Anthropic request exceeds configured input budget")
	}
	if estimate+max+256 > settings.ContextTokens || (p.Settings == nil && len(body)+max+4096 > settings.ContextTokens) {
		return Response{}, errors.New("Anthropic request exceeds configured context budget")
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/messages", bytes.NewReader(body))
	if err != nil {
		return Response{}, errors.New("invalid Anthropic endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("anthropic-version", "2023-06-01")
	if p.APIKey != "" {
		req.Header.Set("x-api-key", p.APIKey)
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(req)
	if err != nil {
		if callCtx.Err() != nil {
			return Response{}, callCtx.Err()
		}
		return Response{}, errors.New("Anthropic connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return Response{}, anthropicHTTPError(resp.StatusCode)
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
		if err != nil || len(raw) > 8<<20 {
			return Response{}, errors.New("invalid or oversized Anthropic response")
		}
		var envelope anthropicEnvelope
		if json.Unmarshal(raw, &envelope) != nil || envelope.Type != "message" {
			return Response{}, errors.New("invalid Anthropic response")
		}
		result, err := anthropicResult(envelope)
		if err == nil && onDelta != nil && result.Content != "" {
			onDelta(result.Content)
		}
		return result, err
	}
	return readAnthropicStream(callCtx, resp.Body, onDelta)
}

func anthropicHTTPError(status int) error {
	kind := "provider"
	switch status {
	case 401:
		kind = "authentication"
	case 403:
		kind = "permission"
	case 429, 529:
		kind = "rate/overload"
	case 400, 413, 422:
		kind = "request/context"
	}
	return fmt.Errorf("Anthropic %s error (HTTP %d)", kind, status)
}

func mergeAnthropicUsage(target *anthropicUsage, delta anthropicUsage) {
	if delta.Input != nil {
		target.Input = delta.Input
	}
	if delta.Output != nil {
		target.Output = delta.Output
	}
	if delta.Read != nil {
		target.Read = delta.Read
	}
	if delta.Write != nil {
		target.Write = delta.Write
	}
}

func anthropicResult(envelope anthropicEnvelope) (Response, error) {
	var result Response
	if envelope.StopReason != "end_turn" && envelope.StopReason != "tool_use" && envelope.StopReason != "stop_sequence" {
		return result, errors.New("Anthropic response incomplete or refused")
	}
	ids := map[string]bool{}
	for _, block := range envelope.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "tool_use":
			var input map[string]any
			if block.ID == "" || ids[block.ID] || block.Name == "" || json.Unmarshal(block.Input, &input) != nil || input == nil {
				return Response{}, errors.New("invalid Anthropic tool call")
			}
			ids[block.ID] = true
			result.ToolCalls = append(result.ToolCalls, ToolCall{ID: block.ID, Name: block.Name, Arguments: block.Input})
		default:
			return Response{}, errors.New("unsupported Anthropic content block")
		}
	}
	if (envelope.StopReason == "tool_use") != (len(result.ToolCalls) > 0) || (result.Content == "" && len(result.ToolCalls) == 0) {
		return Response{}, errors.New("Anthropic response has inconsistent completion state")
	}
	for _, count := range []*int{envelope.Usage.Input, envelope.Usage.Output, envelope.Usage.Read, envelope.Usage.Write} {
		if count != nil && (*count < 0 || *count > 1000000000) {
			return Response{}, errors.New("invalid Anthropic token usage")
		}
	}
	u := envelope.Usage
	if u.Input != nil {
		result.Usage.InputTokens = *u.Input
	}
	if u.Output != nil {
		result.Usage.OutputTokens = *u.Output
	}
	result.Usage.Reported = u.Input != nil && u.Output != nil
	// Anthropic input_tokens excludes cache hits and cache creation; our common
	// input count includes both so aggregate usage and existing pricing stay correct.
	if u.Read != nil {
		result.Usage.CachedInputTokens = *u.Read
		result.Usage.CacheReported = true
		result.Usage.InputTokens += *u.Read
	}
	if u.Write != nil {
		result.Usage.CacheWriteTokens = *u.Write
		result.Usage.CacheWriteReported = true
		result.Usage.InputTokens += *u.Write
	}
	return result, nil
}

func readAnthropicStream(ctx context.Context, reader io.Reader, onDelta func(string)) (Response, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	var envelope anthropicEnvelope
	var data strings.Builder
	var partial strings.Builder
	started, open, deltaReceived := false, false, false
	events, total := 0, 0
	consume := func() (bool, error) {
		if data.Len() == 0 {
			return false, nil
		}
		raw := data.String()
		data.Reset()
		events++
		if events > 20000 {
			return false, errors.New("Anthropic event limit exceeded")
		}
		var e struct {
			Type    string            `json:"type"`
			Index   int               `json:"index"`
			Message anthropicEnvelope `json:"message"`
			Block   anthropicBlock    `json:"content_block"`
			Usage   anthropicUsage    `json:"usage"`
			Delta   struct {
				Type       string `json:"type"`
				Text       string `json:"text"`
				Partial    string `json:"partial_json"`
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
		}
		if json.Unmarshal([]byte(raw), &e) != nil {
			return false, errors.New("invalid Anthropic stream event")
		}
		switch e.Type {
		case "ping":
		case "message_start":
			if started || e.Message.Type != "message" || len(e.Message.Content) > 0 {
				return false, errors.New("invalid Anthropic message start")
			}
			started = true
			envelope = e.Message
		case "content_block_start":
			if !started || open || deltaReceived || e.Index != len(envelope.Content) || e.Index >= 256 {
				return false, errors.New("invalid Anthropic block start")
			}
			if e.Block.Type != "text" && e.Block.Type != "tool_use" {
				return false, errors.New("unsupported Anthropic streamed block")
			}
			envelope.Content = append(envelope.Content, e.Block)
			open = true
			partial.Reset()
			if e.Block.Type == "text" && e.Block.Text != "" && onDelta != nil {
				onDelta(e.Block.Text)
			}
		case "content_block_delta":
			if !open || e.Index != len(envelope.Content)-1 {
				return false, errors.New("invalid Anthropic block delta")
			}
			block := &envelope.Content[e.Index]
			if e.Delta.Type == "text_delta" && block.Type == "text" {
				block.Text += e.Delta.Text
				if onDelta != nil {
					onDelta(e.Delta.Text)
				}
			} else if e.Delta.Type == "input_json_delta" && block.Type == "tool_use" {
				partial.WriteString(e.Delta.Partial)
			} else {
				return false, errors.New("invalid Anthropic delta type")
			}
		case "content_block_stop":
			if !open || e.Index != len(envelope.Content)-1 {
				return false, errors.New("invalid Anthropic block stop")
			}
			if partial.Len() > 0 {
				envelope.Content[e.Index].Input = json.RawMessage(partial.String())
			}
			open = false
		case "message_delta":
			if !started || open || deltaReceived || e.Delta.StopReason == "" {
				return false, errors.New("invalid Anthropic completion")
			}
			deltaReceived = true
			envelope.StopReason = e.Delta.StopReason
			// Start usage only describes the initial fragment; a missing final
			// output count must not masquerade as a fully reported zero.
			envelope.Usage.Output = e.Usage.Output
			mergeAnthropicUsage(&envelope.Usage, e.Usage)
		case "message_stop":
			if !started || open || !deltaReceived {
				return false, errors.New("incomplete Anthropic message")
			}
			return true, nil
		case "error":
			return false, errors.New("Anthropic stream reported an error")
		default:
			return false, errors.New("unsupported Anthropic stream event")
		}
		return false, nil
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return Response{}, err
		}
		line := scanner.Text()
		total += len(line)
		if total > 8<<20 {
			return Response{}, errors.New("Anthropic stream exceeded size limit")
		}
		if line == "" {
			done, err := consume()
			if err != nil {
				return Response{}, err
			}
			if done {
				return anthropicResult(envelope)
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if ctx.Err() != nil {
		return Response{}, ctx.Err()
	}
	if scanner.Err() != nil {
		return Response{}, errors.New("Anthropic stream read failed")
	}
	if done, err := consume(); err != nil {
		return Response{}, err
	} else if done {
		return anthropicResult(envelope)
	}
	return Response{}, errors.New("Anthropic stream ended before message_stop")
}
