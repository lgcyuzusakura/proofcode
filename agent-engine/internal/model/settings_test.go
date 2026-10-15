package model

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func pricePointer(v float64) *float64 { return &v }

func TestPricingSelectsWholeRequestTierAndHandlesMissingCache(t *testing.T) {
	s := DefaultSettings()
	s.Prices = []PriceTier{{UpToInputTokens: 10000, Input: 2, Output: 8, CacheRead: pricePointer(.5)}, {UpToInputTokens: 24000, Input: 4, Output: 12}, {Input: 6, Output: 16}}
	for _, tc := range []struct {
		input int
		upper int
		rate  float64
	}{{10000, 10000, 2}, {10001, 24000, 4}, {24000, 24000, 4}, {24001, 0, 6}} {
		u := Usage{Reported: true, InputTokens: tc.input, OutputTokens: 100, CacheReported: true}
		s.price(&u)
		outputRate := 8.0
		if tc.upper == 24000 {
			outputRate = 12
		}
		if tc.upper == 0 {
			outputRate = 16
		}
		want := (float64(tc.input)*tc.rate + 100*outputRate) / 1e6
		if u.EstimatedCost == nil || math.Abs(*u.EstimatedCost-want) > 1e-12 || u.PriceTierUpper != tc.upper {
			t.Fatalf("input %d: %+v want %f", tc.input, u, want)
		}
	}
	u := Usage{Reported: true, InputTokens: 10000, OutputTokens: 100, CacheReported: true, CachedInputTokens: 8000, CacheWriteReported: true, CacheWriteTokens: 1000}
	s.Prices[0].CacheWrite = pricePointer(3)
	s.price(&u)
	if u.EstimatedCost == nil || math.Abs(*u.EstimatedCost-.0098) > 1e-12 {
		t.Fatalf("cache pricing = %+v", u)
	}
	for _, u := range []Usage{{Reported: true, InputTokens: 1000}, {Reported: false, InputTokens: 1000}, {Reported: true, InputTokens: 1000, CacheReported: true, CachedInputTokens: 1001}, {Reported: true, InputTokens: 1000, CacheReported: true}} {
		s.price(&u)
		if u.EstimatedCost != nil {
			t.Fatalf("incomplete/invalid usage was priced: %+v", u)
		}
	}
	free := DefaultSettings()
	free.Prices = []PriceTier{{}}
	u = Usage{Reported: true}
	free.price(&u)
	if u.EstimatedCost == nil || *u.EstimatedCost != 0 {
		t.Fatal("zero cost must be reported")
	}
}

func TestSettingsRejectInvalidBudgetsAndPriceOrder(t *testing.T) {
	for _, mutate := range []func(*Settings){
		func(s *Settings) { s.MaxInputTokens = s.ContextTokens }, func(s *Settings) { s.Compression = "unknown" }, func(s *Settings) { s.Currency = "usd" },
		func(s *Settings) { s.Prices = []PriceTier{{UpToInputTokens: 0}, {UpToInputTokens: 10000}} },
		func(s *Settings) { s.Prices = []PriceTier{{UpToInputTokens: 24000}, {UpToInputTokens: 10000}} },
		func(s *Settings) { s.Prices = []PriceTier{{Input: math.NaN()}} },
	} {
		s := DefaultSettings()
		mutate(&s)
		if s.Validate() == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
	s := DefaultSettings()
	for strength, want := range map[string]int{"gentle": 16000, "balanced": 12800, "aggressive": 9600} {
		s.Compression = strength
		if s.TargetInputTokens() != want {
			t.Fatal("wrong compression target")
		}
	}
}

func TestInputLimitRejectsBeforeAnyModelConnection(t *testing.T) {
	s := DefaultSettings()
	s.MaxInputTokens = 256
	for _, wire := range []string{"chat", "responses"} {
		_, err := (&OpenAICompatible{BaseURL: "http://127.0.0.1:1", WireAPI: wire, Settings: &s}).Chat(context.Background(), Request{Model: "fixture", Messages: []Message{{Role: RoleUser, Content: strings.Repeat("x", 4000)}}}, nil)
		if err == nil || !strings.Contains(err.Error(), "input token limit") {
			t.Fatalf("%s: %v", wire, err)
		}
	}
}

func TestConfiguredStreamMeasuresTimingCacheAndOutputLimit(t *testing.T) {
	s := DefaultSettings()
	s.MaxOutputTokens = 32
	s.Prices = []PriceTier{{Input: 2, Output: 8, CacheRead: pricePointer(.5), CacheWrite: pricePointer(3)}}
	for _, wire := range []string{"chat", "responses"} {
		t.Run(wire, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				key := "max_tokens"
				if wire == "responses" {
					key = "max_output_tokens"
				}
				if payload[key] != float64(32) {
					t.Errorf("output limit=%v", payload[key])
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				time.Sleep(20 * time.Millisecond)
				if wire == "chat" {
					fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hello"}}]}`)
				} else {
					fmt.Fprintln(w, `data: {"type":"response.output_text.delta","delta":"hello"}`)
				}
				w.(http.Flusher).Flush()
				time.Sleep(20 * time.Millisecond)
				if wire == "chat" {
					fmt.Fprintln(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_cache_hit_tokens":50,"cache_creation_input_tokens":20}}`)
					fmt.Fprintln(w, "data: [DONE]")
				} else {
					fmt.Fprintln(w, `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":100,"output_tokens":10,"input_tokens_details":{"cached_tokens":50,"cache_creation_tokens":20}}}}`)
				}
			}))
			defer server.Close()
			result, err := (&OpenAICompatible{BaseURL: server.URL, WireAPI: wire, Settings: &s}).Chat(context.Background(), Request{Model: "fixture", MaxTokens: 999, Messages: []Message{{Role: RoleUser, Content: "hello"}}}, nil)
			u := result.Usage
			if err != nil || u.FirstTokenMs == nil || *u.FirstTokenMs < 10 || u.DurationMs <= *u.FirstTokenMs || u.TokensPerSecond == nil || *u.TokensPerSecond <= 0 || u.CachedInputTokens != 50 || u.CacheWriteTokens != 20 || u.EstimatedCost == nil {
				t.Fatalf("usage=%+v error=%v", u, err)
			}
			if math.Abs(*u.EstimatedCost-.000225) > 1e-12 {
				t.Fatalf("cost=%f", *u.EstimatedCost)
			}
		})
	}
}
