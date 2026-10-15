package model

import (
	"errors"
	"math"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/config"
)

type PriceTier struct {
	UpToInputTokens int      `json:"upToInputTokens"`
	Input           float64  `json:"input"`
	Output          float64  `json:"output"`
	CacheRead       *float64 `json:"cacheRead"`
	CacheWrite      *float64 `json:"cacheWrite"`
}
type Settings struct {
	ContextTokens   int         `json:"contextTokens"`
	MaxInputTokens  int         `json:"maxInputTokens"`
	MaxOutputTokens int         `json:"maxOutputTokens"`
	Compression     string      `json:"compression"`
	Currency        string      `json:"currency"`
	Prices          []PriceTier `json:"prices"`
}

func DefaultSettings() Settings {
	return Settings{ContextTokens: config.MaxTotalTokens, MaxInputTokens: config.DefaultMaxInputTokens, MaxOutputTokens: config.DefaultMaxOutputTokens, Compression: "balanced", Currency: "CNY"}
}
func (s Settings) Validate() error {
	if s.ContextTokens < 1024 || s.ContextTokens > 2000000 || s.MaxInputTokens < 256 || s.MaxOutputTokens < 1 || s.MaxInputTokens+s.MaxOutputTokens+256 > s.ContextTokens {
		return errors.New("invalid model context/input/output limits")
	}
	if s.Compression != "gentle" && s.Compression != "balanced" && s.Compression != "aggressive" {
		return errors.New("invalid compression strength")
	}
	if len(s.Currency) != 3 {
		return errors.New("invalid pricing currency")
	}
	for _, c := range s.Currency {
		if c < 'A' || c > 'Z' {
			return errors.New("invalid pricing currency")
		}
	}
	if len(s.Prices) > 32 {
		return errors.New("too many price tiers")
	}
	previous := 0
	for i, p := range s.Prices {
		if p.UpToInputTokens < 0 || p.UpToInputTokens > 2000000 || (p.UpToInputTokens == 0 && i != len(s.Prices)-1) || (p.UpToInputTokens != 0 && p.UpToInputTokens <= previous) {
			return errors.New("invalid price tier boundaries")
		}
		for _, v := range []*float64{&p.Input, &p.Output, p.CacheRead, p.CacheWrite} {
			if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 || *v > 1000000) {
				return errors.New("invalid token price")
			}
		}
		previous = p.UpToInputTokens
	}
	return nil
}

// Stronger compression keeps a smaller model view. Protected requirements can
// use the full configured input budget if they cannot fit the preferred target.
func (s Settings) TargetInputTokens() int {
	ratio := 1.0
	if s.Compression == "balanced" {
		ratio = .8
	}
	if s.Compression == "aggressive" {
		ratio = .6
	}
	return int(float64(s.MaxInputTokens) * ratio)
}

type SettingsProvider interface {
	ModelSettings() Settings
	HasModelSettings() bool
}

func (p *OpenAICompatible) ModelSettings() Settings {
	if p.Settings != nil {
		return *p.Settings
	}
	return DefaultSettings()
}
func (p *OpenAICompatible) HasModelSettings() bool { return p.Settings != nil }

func (s Settings) price(u *Usage) {
	u.CostCurrency = s.Currency
	if !u.Reported || u.InputTokens < 0 || u.OutputTokens < 0 {
		return
	}
	var tier *PriceTier
	for i := range s.Prices {
		p := &s.Prices[i]
		if p.UpToInputTokens == 0 || u.InputTokens <= p.UpToInputTokens {
			tier = p
			break
		}
	}
	if tier == nil {
		return
	}
	u.PriceTierUpper = tier.UpToInputTokens
	cached, written := 0, 0
	if u.CacheReported {
		cached = u.CachedInputTokens
	} else if tier.CacheRead != nil && *tier.CacheRead != tier.Input {
		return
	}
	if u.CacheWriteReported {
		written = u.CacheWriteTokens
	} else if tier.CacheWrite != nil && *tier.CacheWrite != tier.Input {
		return
	}
	if cached < 0 || written < 0 || cached+written > u.InputTokens {
		return
	}
	readRate, writeRate := tier.Input, tier.Input
	if tier.CacheRead != nil {
		readRate = *tier.CacheRead
	}
	if tier.CacheWrite != nil {
		writeRate = *tier.CacheWrite
	}
	cost := (float64(u.InputTokens-cached-written)*tier.Input + float64(cached)*readRate + float64(written)*writeRate + float64(u.OutputTokens)*tier.Output) / 1000000
	u.EstimatedCost = &cost
}
