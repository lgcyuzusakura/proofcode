package event

import (
	"context"
	"sync"
	"time"
)

type Type string

const (
	TaskStarted        Type = "task.started"
	TaskCompleted      Type = "task.completed"
	TaskFailed         Type = "task.failed"
	TaskCancelled      Type = "task.cancelled"
	MessageDelta       Type = "agent.message.delta"
	MessageCompleted   Type = "agent.message.completed"
	AgentDelegated     Type = "agent.delegated"
	ToolRequested      Type = "tool.requested"
	ToolApprovalNeeded Type = "tool.approval_required"
	ToolStarted        Type = "tool.started"
	ToolCompleted      Type = "tool.completed"
	ToolFailed         Type = "tool.failed"
	ContextSelected    Type = "context.selected"
	CheckpointCreated  Type = "checkpoint.created"
	VerificationDone   Type = "verification.completed"
	UsageUpdated       Type = "usage.updated"
)

type Event struct {
	Version   string         `json:"version"`
	TaskID    string         `json:"taskId"`
	Sequence  int64          `json:"sequence"`
	Type      Type           `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload"`
}

type Sink interface {
	Publish(context.Context, Event) error
}

type SequencedSink struct {
	mu   sync.Mutex
	next int64
	sink Sink
}

func NewSequencedSink(sink Sink) *SequencedSink {
	return &SequencedSink{next: 1, sink: sink}
}

func (s *SequencedSink) Emit(ctx context.Context, taskID string, kind Type, payload map[string]any) error {
	s.mu.Lock()
	sequence := s.next
	s.next++
	s.mu.Unlock()
	return s.sink.Publish(ctx, Event{
		Version: "v1", TaskID: taskID, Sequence: sequence,
		Type: kind, Timestamp: time.Now().UTC(), Payload: payload,
	})
}

type MemorySink struct {
	mu     sync.Mutex
	Events []Event
}

func (m *MemorySink) Publish(_ context.Context, value Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = append(m.Events, value)
	return nil
}
