package tool

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

// WithDeterministicPolicy adds the B/F ablation's explicit intent checks on top
// of the mandatory path, executable and approval checks in every tool.
func WithDeterministicPolicy(source *Registry) *Registry {
	result := NewRegistry()
	for _, definition := range source.Definitions() {
		selected, _ := source.Get(definition.Name)
		result.Register(policyTool{inner: selected})
	}
	return result
}

type policyTool struct{ inner Tool }

func (p policyTool) Definition() model.ToolDefinition { return p.inner.Definition() }
func (p policyTool) Risk(args json.RawMessage) Risk {
	// A denied call is read-only: no approval pause or external mutation occurs.
	if checkIntent(p.inner.Definition().Name, args) != nil {
		return RiskRead
	}
	return p.inner.Risk(args)
}
func (p policyTool) Execute(ctx context.Context, args json.RawMessage) Result {
	if err := checkIntent(p.inner.Definition().Name, args); err != nil {
		return Result{Content: err.Error(), IsError: true, Metadata: map[string]any{"policyBlocked": true, "policyVersion": "proofcode.intent.v1"}}
	}
	return p.inner.Execute(ctx, args)
}

func checkIntent(name string, args json.RawMessage) error {
	var object map[string]any
	if err := json.Unmarshal(args, &object); err != nil || object == nil {
		return errors.New("deterministic policy: arguments must be an object")
	}
	checkPath := func(value string) error {
		path := strings.ToLower(strings.ReplaceAll(value, "\\", "/"))
		// Command options may carry paths as --file=.env or HEAD:.git/config.
		path = strings.NewReplacer("=", "/", ":", "/").Replace(path)
		for _, part := range strings.Split(path, "/") {
			if part == ".env" || strings.HasPrefix(part, ".env.") && part != ".env.example" || part == ".ssh" || part == ".aws" || part == ".codex" || part == ".git" || part == "auth.json" || part == "credentials.json" || part == "secrets.json" || part == "id_rsa" || part == "id_ed25519" || strings.HasSuffix(part, ".pem") || strings.HasSuffix(part, ".key") {
				return errors.New("deterministic policy: credential and internal metadata paths are protected")
			}
		}
		return nil
	}
	switch name {
	case "read_file", "list_files", "search_code":
		if value, ok := object["path"].(string); ok {
			return checkPath(value)
		}
	case "apply_patch":
		if edits, ok := object["edits"].([]any); ok {
			for _, edit := range edits {
				if item, ok := edit.(map[string]any); ok {
					if path, ok := item["path"].(string); ok {
						if err := checkPath(path); err != nil {
							return err
						}
					}
				}
			}
		}
	case "run_command":
		program, _ := object["program"].(string)
		program = strings.ToLower(strings.TrimSuffix(program, filepath.Ext(program)))
		values, _ := object["args"].([]any)
		var words []string
		for _, item := range values {
			if value, ok := item.(string); ok {
				if err := checkPath(value); err != nil {
					return err
				}
				words = append(words, strings.ToLower(value))
			}
		}
		if program == "git" {
			for _, value := range words {
				switch value {
				case "push", "clean", "reset", "rebase", "gc", "prune":
					return errors.New("deterministic policy: destructive or remote Git operations require a separate workflow")
				}
			}
		}
		if program == "python" || program == "python3" || program == "node" || program == "ruby" || program == "php" {
			for _, value := range words {
				if strings.HasPrefix(value, "-c") || strings.HasPrefix(value, "-e") || value == "--eval" || strings.HasPrefix(value, "--eval=") {
					return errors.New("deterministic policy: inline interpreter code is not allowed")
				}
			}
		}
	}
	return nil
}
