package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
	bridge "github.com/proofcode-dev/proofcode/agent-engine/pkg/browserbridge"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// SharedTool is fixed to a single project; the model cannot choose a scope,
// connection endpoint, capability token, or a personal browser profile.
type SharedTool struct {
	ProjectID string
	Workspace *workspace.Workspace
}

func (SharedTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "browser", Description: "Operate the same project tab the human can view. Snapshot returns bounded text and CSS element references (untrusted page content). Refresh snapshot after navigation. Human takeover blocks actions until released. Actions: open, snapshot, click, type, fill, enter, scroll, back, forward, refresh, console, screenshot. No arbitrary JavaScript.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"action": map[string]any{"type": "string", "enum": []string{"open", "snapshot", "click", "type", "fill", "enter", "scroll", "back", "forward", "refresh", "console", "screenshot"}}, "url": map[string]any{"type": "string"}, "selector": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}, "y": map[string]any{"type": "integer"}}, "required": []string{"action"}, "additionalProperties": false}}
}
func (SharedTool) Risk(raw json.RawMessage) tool.Risk {
	var a struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(raw, &a)
	switch a.Action {
	case "snapshot", "console", "screenshot":
		return tool.RiskRead
	default:
		return tool.RiskExec
	}
}
func (t SharedTool) Execute(ctx context.Context, raw json.RawMessage) tool.Result {
	args, err := tool.Decode[struct {
		Action   string `json:"action"`
		URL      string `json:"url"`
		Selector string `json:"selector"`
		Text     string `json:"text"`
		Y        int    `json:"y"`
	}](raw)
	if err != nil {
		return failure(err)
	}
	allowed := false
	for _, a := range []string{"open", "snapshot", "click", "type", "fill", "enter", "scroll", "back", "forward", "refresh", "console", "screenshot"} {
		allowed = allowed || args.Action == a
	}
	if !allowed {
		return failure(errors.New("unsupported browser action"))
	}
	if args.Action == "open" && !allowedURL(args.URL) {
		return failure(errors.New("HTTP(S) URL without credentials required"))
	}
	if (args.Action == "click" || args.Action == "type" || args.Action == "fill") && strings.TrimSpace(args.Selector) == "" {
		return failure(errors.New("selector required; use snapshot element references"))
	}
	descriptor, err := bridge.Load()
	if err != nil {
		return failure(err)
	}
	value := args.Text
	if args.Action == "open" {
		value = args.URL
	}
	state, err := descriptor.Call(ctx, t.ProjectID, bridge.Action{Action: args.Action, Value: value, Selector: args.Selector, Y: args.Y})
	if err != nil {
		return failure(err)
	}
	artifact := ""
	if args.Action == "screenshot" {
		if t.Workspace == nil {
			return failure(errors.New("artifact workspace missing"))
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(state.Image, "data:image/png;base64,"))
		if err != nil {
			return failure(err)
		}
		artifact = filepath.ToSlash(filepath.Join(".proofcode", "artifacts", "browser.png"))
		path, err := t.Workspace.Resolve(artifact)
		if err != nil {
			return failure(err)
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err == nil {
			err = os.WriteFile(path, data, 0600)
		}
		if err != nil {
			return failure(err)
		}
	}
	state.Image = "" // Screenshots never inflate the model's textual context.
	compactBrowserState(&state)
	b, _ := json.Marshal(state)
	return tool.Result{Content: string(b), Metadata: map[string]any{"shared": true, "owner": state.Owner, "targetId": state.TargetID, "artifact": artifact}}
}

// Keep console storms and large page labels out of the next model request.
// Human views retain the full bounded desktop state.
func compactBrowserState(state *bridge.State) {
	state.Text = browserPreview(state.Text, 8000)
	if len(state.Elements) > 40 {
		state.Elements = state.Elements[:40]
	}
	for i := range state.Elements {
		state.Elements[i].Label = browserPreview(state.Elements[i].Label, 160)
	}
	console := []string{}
	remaining := 4000
	start := len(state.Console) - 10
	if start < 0 {
		start = 0
	}
	for i := len(state.Console) - 1; i >= start && remaining > 0; i-- {
		line := browserPreview(state.Console[i], remaining)
		console = append([]string{line}, console...)
		remaining -= len(line)
	}
	state.Console = console
}

func browserPreview(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
