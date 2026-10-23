package main

import (
	"context"
	"errors"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/worktree"
)

type previewFailSink struct{ calls int }

func (s *previewFailSink) Publish(context.Context, event.Event) error {
	s.calls++
	return errors.New("offline")
}

func TestWorkspacePreviewIsAdvisoryAndRetriesFailedPublication(t *testing.T) {
	sink := &previewFailSink{}
	publish := workspacePreviewPublisher("task", "base", event.NewSequencedSink(sink), func(context.Context) (string, error) { return "patch", nil })
	for i := 0; i < 2; i++ {
		if err := publish(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if sink.calls != 2 {
		t.Fatal("failed previews must be retried on the next checkpoint")
	}
}

func TestWorkspacePreviewStatesAndDeduplication(t *testing.T) {
	sink := &event.MemorySink{}
	patch, captureErr := "patch", error(nil)
	publish := workspacePreviewPublisher("task", "base", event.NewSequencedSink(sink), func(context.Context) (string, error) { return patch, captureErr })
	for _, err := range []error{nil, nil, worktree.ErrPreviewTooLarge, errors.New("private capture failure")} {
		captureErr = err
		if err := publish(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(sink.Events) != 3 {
		t.Fatalf("events: %#v", sink.Events)
	}
	for i, state := range []string{"ready", "oversized", "unavailable"} {
		payload := sink.Events[i].Payload
		if payload["state"] != state {
			t.Fatalf("state: %#v", payload)
		}
		if i > 0 && (payload["patch"] != nil || payload["sha256"] != nil) {
			t.Fatal("failed preview leaked a patch")
		}
	}
}
