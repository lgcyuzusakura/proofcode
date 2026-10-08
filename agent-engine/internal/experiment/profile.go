// Package experiment defines the executable ablations used by the thesis.
// Mandatory workspace isolation and data approvals are never experimental flags.
package experiment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
)

const ProfileVersion = "proofcode.experiment.v1"

type Profile struct {
	Version                   string `json:"version"`
	Group                     string `json:"group"`
	MandatorySafety           bool   `json:"mandatorySafety"`
	ToolsEnabled              bool   `json:"toolsEnabled"`
	DeterministicSafety       bool   `json:"deterministicSafety"`
	JevRouting                bool   `json:"jevRouting"`
	JevRequired               bool   `json:"jevRequired"`
	RAGEnabled                bool   `json:"ragEnabled"`
	ContextCompressionEnabled bool   `json:"contextCompressionEnabled"`
	FeedbackRetrieval         bool   `json:"feedbackRetrieval"`
}

func ForGroup(group string) (Profile, error) {
	p := Profile{Version: ProfileVersion, Group: group, MandatorySafety: true, ToolsEnabled: group != "A"}
	switch group {
	case "A":
	case "B":
		p.DeterministicSafety = true
	case "C":
		p.JevRouting, p.JevRequired = true, true
	case "D":
		p.RAGEnabled = true
	case "E":
		p.RAGEnabled, p.ContextCompressionEnabled = true, true
	case "F":
		p.DeterministicSafety, p.JevRouting, p.JevRequired = true, true, true
		p.RAGEnabled, p.ContextCompressionEnabled, p.FeedbackRetrieval = true, true, true
	default:
		return Profile{}, fmt.Errorf("unknown experiment group %q: expected A through F", group)
	}
	return p, nil
}

// Validate prevents a queue payload from silently redefining an ablation.
func (p Profile) Validate(group, version string) error {
	expected, err := ForGroup(group)
	if err != nil {
		return err
	}
	if version != ProfileVersion || !reflect.DeepEqual(p, expected) {
		return fmt.Errorf("experiment profile does not match %s group %s", ProfileVersion, group)
	}
	return nil
}

func Fingerprint(value any) string {
	encoded, _ := json.Marshal(value)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}
