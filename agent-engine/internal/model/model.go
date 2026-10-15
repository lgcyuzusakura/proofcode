package model

import (
	"context"
	"encoding/json"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"toolCallId,omitempty"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Request struct {
	Model       string
	Messages    []Message
	Tools       []ToolDefinition
	Temperature float64
	MaxTokens   int
}

type Usage struct {
	InputTokens        int      `json:"inputTokens"`
	OutputTokens       int      `json:"outputTokens"`
	Reported           bool     `json:"reported,omitempty"`
	CachedInputTokens  int      `json:"cachedInputTokens"`
	CacheReported      bool     `json:"cacheReported,omitempty"`
	CacheWriteTokens   int      `json:"cacheWriteTokens"`
	CacheWriteReported bool     `json:"cacheWriteReported,omitempty"`
	DurationMs         float64  `json:"durationMs"`
	FirstTokenMs       *float64 `json:"firstTokenMs,omitempty"`
	TokensPerSecond    *float64 `json:"tokensPerSecond,omitempty"`
	EstimatedCost      *float64 `json:"estimatedCost,omitempty"`
	CostCurrency       string   `json:"costCurrency,omitempty"`
	PriceTierUpper     int      `json:"priceTierUpper"`
}

type Response struct {
	Content   string
	ToolCalls []ToolCall
	Usage     Usage
}

type Provider interface {
	Chat(context.Context, Request, func(delta string)) (Response, error)
}
