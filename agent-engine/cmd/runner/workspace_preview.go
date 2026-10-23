package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/worktree"
)

// The UI preview is bounded independently of the final review artifact.
const workspacePreviewLimit = 128 << 10

func workspacePreviewPublisher(taskID, base string, events *event.SequencedSink, capture func(context.Context) (string, error)) func(context.Context) error {
	last := ""
	return func(ctx context.Context) error {
		call, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		patch, err := capture(call)
		state := "ready"
		if errors.Is(err, worktree.ErrPreviewTooLarge) {
			state = "oversized"
		} else if err != nil {
			state = "unavailable"
		}
		if err != nil {
			patch = ""
		}
		sum := sha256.Sum256([]byte(patch))
		hash := hex.EncodeToString(sum[:])
		identity := state + ":" + hash
		if identity == last {
			return nil
		}
		payload := map[string]any{"state": state, "base": base, "limitBytes": workspacePreviewLimit}
		if state == "ready" {
			payload["patch"], payload["sha256"] = patch, hash
		}
		// A preview is advisory. Durable state has already been saved, so a
		// failed notification must not abort or replay the completed tool.
		publish, done := context.WithTimeout(ctx, 3*time.Second)
		defer done()
		if err := events.Emit(publish, taskID, event.WorkspaceUpdated, payload); err != nil {
			return nil
		}
		last = identity
		return nil
	}
}
