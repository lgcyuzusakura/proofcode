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
	ToolRouted         Type = "decision.tool_routed"
	ToolRouteFallback  Type = "decision.tool_route_fallback"
	ContextSelected    Type = "context.selected"
	CheckpointCreated  Type = "checkpoint.created"
	RecoveryCreated    Type = "recovery.created"
	VerificationDone   Type = "verification.completed"
	UsageUpdated       Type = "usage.updated"
)

type Event struct {
	Version   string         `json:"version"`
	TaskID    string         `json:"taskId"`
	Attempt   int            `json:"attempt"`
	RunID     string         `json:"runId,omitempty"`
	RunnerID  string         `json:"runnerId,omitempty"`
	Sequence  int64          `json:"sequence"`
	Type      Type           `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload"`
}

type Sink interface {
	Publish(context.Context, Event) error
}

type SequencedSink struct {
	mu       sync.Mutex
	next     int64
	runID    string
	runnerID string
	attempt  int
	sink     Sink
}

func NewSequencedSink(sink Sink) *SequencedSink {
	return NewRunSink(sink, "local")
}

// NewSequencedSinkAt starts at an externally chosen cursor for local sinks.
func NewSequencedSinkAt(sink Sink, start int64) *SequencedSink {
	if start < 1 {
		start = 1
	}
	return &SequencedSink{next: start, runID: "local", attempt: 1, sink: sink}
}

func NewRunSink(sink Sink, runID string) *SequencedSink {
	return NewRunSinkForAttempt(sink, runID, 1)
}

func NewRunSinkForAttempt(sink Sink, runID string, attempt int) *SequencedSink {
	if attempt < 1 {
		attempt = 1
	}
	return &SequencedSink{next: 1, runID: runID, attempt: attempt, sink: sink}
}

func NewRunnerSink(sink Sink, runID, runnerID string, attempt int) *SequencedSink {
	sequenced := NewRunSinkForAttempt(sink, runID, attempt)
	sequenced.runnerID = runnerID
	return sequenced
}

func (s *SequencedSink) Emit(ctx context.Context, taskID string, kind Type, payload map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sequence := s.next
	s.next++
	if err := s.sink.Publish(ctx, Event{
		Version: "v1", TaskID: taskID, Attempt: s.attempt, RunID: s.runID, RunnerID: s.runnerID, Sequence: sequence,
		Type: kind, Timestamp: time.Now().UTC(), Payload: payload,
	}); err != nil {
		return err
	}
	return nil
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
