package experiment

import (
	"context"
	"sync"
	"time"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
)

// State is persisted alongside the durable transcript across approval pauses.
// Unknown outcomes (tests, database rollback, retrieval accuracy) stay absent.
type State struct {
	StartedAt time.Time      `json:"startedAt"`
	Values    map[string]any `json:"values"`
}

type Collector struct {
	Sink  event.Sink
	mu    sync.Mutex
	state State
}

func NewCollector(sink event.Sink, revision string) *Collector {
	return &Collector{Sink: sink, state: State{StartedAt: time.Now().UTC(), Values: map[string]any{
		"profileVersion": ProfileVersion, "profileApplied": false, "sourceRevision": revision,
		"toolCalls": 0, "toolErrors": 0, "invalidToolCalls": 0, "highRiskBlocked": 0,
		"retrievalCalls": 0, "retrievalCacheHits": 0, "jevDecisions": 0, "jevApplied": 0,
	}}}
}

func (c *Collector) Publish(ctx context.Context, value event.Event) error {
	if err := c.Sink.Publish(ctx, value); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v, p := c.state.Values, value.Payload
	inc := func(key string) { v[key] = integer(v[key]) + 1 }
	switch value.Type {
	case event.ToolRequested:
		inc("toolCalls")
	case event.ToolFailed:
		inc("toolErrors")
		if p["invalid"] == true {
			inc("invalidToolCalls")
		}
		if metadata, ok := p["metadata"].(map[string]any); ok && metadata["policyBlocked"] == true {
			inc("highRiskBlocked")
		}
	case event.ToolRouted:
		inc("jevDecisions")
		if p["applied"] == true {
			inc("jevApplied")
		}
	case event.ContextSelected:
		switch p["kind"] {
		case "retrieval":
			inc("retrievalCalls")
			if p["cacheHit"] == true {
				inc("retrievalCacheHits")
			}
			v["snapshotId"] = p["snapshotId"]
		case "compression":
			before, beforePresent := p["beforeEstimatedTokens"]
			after, afterPresent := p["afterEstimatedTokens"]
			if !beforePresent {
				before, beforePresent = p["beforeTokens"]
			}
			if !afterPresent {
				after, afterPresent = p["afterTokens"]
			}
			if beforePresent && afterPresent {
				v["tokensBeforeCompression"] = integer(v["tokensBeforeCompression"]) + integer(before)
				v["tokensAfterCompression"] = integer(v["tokensAfterCompression"]) + integer(after)
				v["compressionTokenBasis"] = "estimated"
			}
			v["deduplicatedMessages"] = integer(v["deduplicatedMessages"]) + integer(p["deduplicatedMessages"])
		}
	case event.UsageUpdated:
		v["usageReported"] = p["usageReported"] == true
		if p["usageReported"] == true {
			v["inputTokens"] = integer(p["inputTokens"])
			v["outputTokens"] = integer(p["outputTokens"])
			v["totalTokens"] = integer(p["totalTokens"])
		} else {
			// A later request with missing usage invalidates earlier complete totals.
			delete(v, "inputTokens")
			delete(v, "outputTokens")
			delete(v, "totalTokens")
		}
	}
	return nil
}

func (c *Collector) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Values[key] = copyValue(value)
}
func (c *Collector) Snapshot() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return State{StartedAt: c.state.StartedAt, Values: copyMap(c.state.Values)}
}
func (c *Collector) Restore(state State) {
	if state.StartedAt.IsZero() || state.Values == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = State{StartedAt: state.StartedAt, Values: copyMap(state.Values)}
}
func (c *Collector) Report(completed bool, failure string) map[string]any {
	s := c.Snapshot()
	v := s.Values
	v["durationMs"] = time.Since(s.StartedAt).Milliseconds()
	v["taskCompleted"] = completed
	if failure != "" {
		v["failureReason"] = failure
	}
	return v
}
func copyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for k, v := range value {
		result[k] = copyValue(v)
	}
	return result
}
func copyValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return copyMap(v)
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = copyValue(item)
		}
		return result
	case []string:
		return append([]string(nil), v...)
	default:
		return value
	}
}
func integer(value any) int {
	switch n := value.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
