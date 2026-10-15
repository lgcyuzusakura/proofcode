package event

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type failingSink struct {
	mu     sync.Mutex
	fail   bool
	values []Event
}

func (s *failingSink) Publish(_ context.Context, value Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		s.fail = false
		return errors.New("temporary failure")
	}
	s.values = append(s.values, value)
	return nil
}

func TestSequencedSinkDoesNotReuseIdentityAfterFailedPublish(t *testing.T) {
	sink := &failingSink{fail: true}
	sequence := NewSequencedSinkAt(sink, 42)
	if err := sequence.Emit(context.Background(), "task", TaskStarted, nil); err == nil {
		t.Fatal("expected publish failure")
	}
	if err := sequence.Emit(context.Background(), "task", TaskStarted, nil); err != nil {
		t.Fatal(err)
	}
	if err := sequence.Emit(context.Background(), "task", TaskCompleted, nil); err != nil {
		t.Fatal(err)
	}
	if len(sink.values) != 2 || sink.values[0].Sequence != 43 || sink.values[1].Sequence != 44 {
		t.Fatalf("unexpected sequence after retry: %+v", sink.values)
	}
}

func TestRunSinkKeepsTaskAttempt(t *testing.T) {
	memory := &MemorySink{}
	sink := NewRunnerSink(memory, "run-1", "runner-1", 3)
	if err := sink.Emit(context.Background(), "task", TaskStarted, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if len(memory.Events) != 1 || memory.Events[0].Attempt != 3 || memory.Events[0].RunID != "run-1" || memory.Events[0].RunnerID != "runner-1" {
		t.Fatalf("attempt identity was lost: %+v", memory.Events)
	}
}
