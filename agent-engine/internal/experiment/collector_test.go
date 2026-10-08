package experiment

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	codecontext "github.com/proofcode-dev/proofcode/agent-engine/internal/context"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
)

func TestCollectorRestoresPersistedCountersAndCompressionJSON(t *testing.T) {
	collector := NewCollector(&event.MemorySink{}, "fixed-revision")
	emit := func(kind event.Type, payload map[string]any) {
		t.Helper()
		if err := collector.Publish(context.Background(), event.Event{Type: kind, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	emit(event.ToolRequested, nil)
	emit(event.ToolFailed, map[string]any{"metadata": map[string]any{"policyBlocked": true}})
	emit(event.ToolRouted, map[string]any{"applied": true})
	emit(event.ContextSelected, map[string]any{"kind": "retrieval", "cacheHit": true, "snapshotId": "exact-snapshot"})
	report := codecontext.CompressionReport{BeforeEstimatedTokens: 100, AfterEstimatedTokens: 40, DeduplicatedMessages: 2}
	encoded, _ := json.Marshal(report)
	var payload map[string]any
	_ = json.Unmarshal(encoded, &payload)
	payload["kind"] = "compression"
	emit(event.ContextSelected, payload)
	emit(event.UsageUpdated, map[string]any{"usageReported": true, "inputTokens": 120, "outputTokens": 30, "totalTokens": 150})
	persisted, _ := json.Marshal(collector.Snapshot())
	var state State
	if err := json.Unmarshal(persisted, &state); err != nil {
		t.Fatal(err)
	}
	resumed := NewCollector(&event.MemorySink{}, "ignored-revision")
	resumed.Restore(state)
	if err := resumed.Publish(context.Background(), event.Event{Type: event.ToolRequested}); err != nil {
		t.Fatal(err)
	}
	metrics := resumed.Report(true, "")
	for key, want := range map[string]int{"toolCalls": 2, "toolErrors": 1, "highRiskBlocked": 1, "jevDecisions": 1, "jevApplied": 1, "retrievalCalls": 1, "retrievalCacheHits": 1, "tokensBeforeCompression": 100, "tokensAfterCompression": 40, "deduplicatedMessages": 2, "totalTokens": 150} {
		if integer(metrics[key]) != want {
			t.Fatalf("restored %s=%v want %d", key, metrics[key], want)
		}
	}
	if metrics["sourceRevision"] != "fixed-revision" || metrics["snapshotId"] != "exact-snapshot" || metrics["compressionTokenBasis"] != "estimated" {
		t.Fatalf("state provenance lost: %+v", metrics)
	}
	for _, key := range []string{"testsPassed", "retrievalAccuracy", "rollbackSuccess"} {
		if _, ok := metrics[key]; ok {
			t.Fatalf("invented outcome: %s", key)
		}
	}
	_ = resumed.Publish(context.Background(), event.Event{Type: event.UsageUpdated, Payload: map[string]any{"usageReported": false, "inputTokens": 0, "outputTokens": 0, "totalTokens": 0}})
	unknown := resumed.Report(false, "usage missing")
	if _, ok := unknown["totalTokens"]; ok || unknown["usageReported"] != false {
		t.Fatalf("missing usage became stale complete total: %+v", unknown)
	}
}

type failedSink struct{}

func (failedSink) Publish(context.Context, event.Event) error { return errors.New("not persisted") }
func TestCollectorCountsOnlyDurableEventsAndIsolatesSnapshots(t *testing.T) {
	c := NewCollector(failedSink{}, "revision")
	before := c.Snapshot()
	if err := c.Publish(context.Background(), event.Event{Type: event.ToolRequested}); err == nil || !reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatal("failed durable publish changed metrics")
	}
	value := map[string]any{"nested": []any{map[string]any{"key": "original"}}}
	c.Set("proof", value)
	value["nested"].([]any)[0].(map[string]any)["key"] = "caller mutation"
	snapshot := c.Snapshot()
	snapshot.Values["proof"].(map[string]any)["nested"].([]any)[0].(map[string]any)["key"] = "snapshot mutation"
	restored := NewCollector(&event.MemorySink{}, "revision")
	restored.Restore(c.Snapshot())
	if restored.Snapshot().Values["proof"].(map[string]any)["nested"].([]any)[0].(map[string]any)["key"] != "original" {
		t.Fatal("snapshots alias external nested state")
	}
}
