package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

type Risk string

const (
	RiskRead  Risk = "read"
	RiskWrite Risk = "write"
	RiskExec  Risk = "exec"
)

type Result struct {
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
	IsError  bool           `json:"isError"`
}

type Tool interface {
	Definition() model.ToolDefinition
	Risk(json.RawMessage) Risk
	Execute(context.Context, json.RawMessage) Result
}

type Registry struct {
	mu    sync.RWMutex
	items map[string]Tool
}

func NewRegistry(values ...Tool) *Registry {
	r := &Registry{items: map[string]Tool{}}
	for _, value := range values {
		r.Register(value)
	}
	return r
}

func (r *Registry) Register(value Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[value.Definition().Name] = value
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.items[name]
	return value, ok
}

func (r *Registry) Definitions() []model.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.items))
	for name := range r.items {
		names = append(names, name)
	}
	sort.Strings(names)
	values := make([]model.ToolDefinition, 0, len(names))
	for _, name := range names {
		values = append(values, r.items[name].Definition())
	}
	return values
}

func Decode[T any](raw json.RawMessage) (T, error) {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("invalid tool arguments: %w", err)
	}
	return value, nil
}
