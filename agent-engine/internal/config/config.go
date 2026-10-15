package config

import shared "github.com/proofcode-dev/proofcode/agent-engine/pkg/config"

const MaxTotalTokens = shared.MaxTotalTokens
const DefaultMaxInputTokens = shared.DefaultMaxInputTokens
const DefaultMaxOutputTokens = shared.DefaultMaxOutputTokens

type ModelConfig = shared.ModelConfig
type RedactedModelConfig = shared.RedactedModelConfig

var Load = shared.Load
var EstimateTokens = shared.EstimateTokens
