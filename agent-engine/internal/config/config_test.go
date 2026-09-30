package config

import "testing"

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("12345678") != 2 {
		t.Fatal("unexpected estimate")
	}
}

func TestRedactedNeverReturnsKey(t *testing.T) {
	value := ModelConfig{APIKey: "secret-key-1234", MaxOutputTokens: DefaultMaxOutputTokens}
	result := value.Redacted(nil)
	if result.KeyLast4 != "****1234" || result.Configured != true {
		t.Fatalf("unexpected redaction: %+v", result)
	}
}
