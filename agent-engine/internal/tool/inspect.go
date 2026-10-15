package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type FileInfo struct{ Workspace *workspace.Workspace }

func (FileInfo) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "file_info", Description: "Inspect workspace file size, modification time and SHA-256 without sending its full content to the model.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}, "additionalProperties": false}}
}
func (FileInfo) Risk(json.RawMessage) Risk { return RiskRead }
func (t FileInfo) Execute(ctx context.Context, raw json.RawMessage) Result {
	if t.Workspace == nil {
		return Result{Content: "workspace required", IsError: true}
	}
	a, err := Decode[struct {
		Path string `json:"path"`
	}](raw)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}
	}
	p, err := t.Workspace.Resolve(a.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}
	}
	info, err := os.Stat(p)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return Result{Content: "regular file up to 16 MiB required", IsError: true}
	}
	if ctx.Err() != nil {
		return Result{Content: ctx.Err().Error(), IsError: true}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}
	}
	hash := sha256.Sum256(data)
	b, _ := json.Marshal(map[string]any{"path": a.Path, "bytes": len(data), "sha256": hex.EncodeToString(hash[:]), "modifiedAt": info.ModTime().UTC()})
	return Result{Content: string(b)}
}

type ReadFiles struct{ Workspace *workspace.Workspace }

func (ReadFiles) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "read_files", Description: "Read up to 6 selected workspace files in one round trip, each with path/start_line/end_line. Output limit 16000 bytes. Use narrow line ranges.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"files": map[string]any{"type": "array", "minItems": 1, "maxItems": 6, "items": (ReadFile{}).Definition().Parameters}}, "required": []string{"files"}, "additionalProperties": false}}
}
func (ReadFiles) Risk(json.RawMessage) Risk { return RiskRead }
func (t ReadFiles) Execute(ctx context.Context, raw json.RawMessage) Result {
	var a struct {
		Files []json.RawMessage `json:"files"`
	}
	if json.Unmarshal(raw, &a) != nil || len(a.Files) < 1 || len(a.Files) > 6 {
		return Result{Content: "read_files requires 1..6 file requests", IsError: true}
	}
	results := []Result{}
	total := 0
	for _, v := range a.Files {
		if err := checkIntent("read_file", v); err != nil {
			return Result{IsError: true, Content: err.Error()}
		}
		r := (ReadFile{Workspace: t.Workspace}).Execute(ctx, v)
		total += len(r.Content)
		if total > 16000 {
			return Result{Content: "batch output exceeds 16000 bytes; use narrower line ranges", IsError: true}
		}
		results = append(results, r)
	}
	b, _ := json.Marshal(results)
	return Result{Content: string(b)}
}

type GitInspect struct{ Workspace *workspace.Workspace }

func (GitInspect) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "git_inspect", Description: "Read Git status or recent log (up to 20). Never changes Git state or fetches. No arbitrary Git flags.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"action": map[string]any{"type": "string", "enum": []string{"status", "log"}}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, "required": []string{"action"}, "additionalProperties": false}}
}
func (GitInspect) Risk(json.RawMessage) Risk { return RiskRead }
func (t GitInspect) Execute(ctx context.Context, raw json.RawMessage) Result {
	if t.Workspace == nil {
		return Result{Content: "workspace required", IsError: true}
	}
	a, err := Decode[struct {
		Action string `json:"action"`
		Limit  int    `json:"limit"`
	}](raw)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}
	}
	args := []string{"--no-pager"}
	switch a.Action {
	case "status":
		args = append(args, "status", "--short", "--branch")
	case "log":
		if a.Limit == 0 {
			a.Limit = 10
		}
		if a.Limit < 1 || a.Limit > 20 {
			return Result{Content: "limit outside 1..20", IsError: true}
		}
		args = append(args, "log", "-n", strconv.Itoa(a.Limit), "--format=%h %ad %s", "--date=iso-strict")
	default:
		return Result{Content: "unknown git inspection", IsError: true}
	}
	call, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	cmd := exec.CommandContext(call, "git", args...)
	cmd.Dir = t.Workspace.Root
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if len(out) > 16000 {
		out = append(out[:16000], []byte("\n[output truncated]")...)
	}
	return Result{Content: string(out), IsError: err != nil}
}

type Discover struct{ Registry *Registry }

func (Discover) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "tool_search", Description: "Find actual available tools and schemas by keyword, including database/cache/context/team operations.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string", "maxLength": 80}}, "required": []string{"query"}, "additionalProperties": false}}
}
func (Discover) Risk(json.RawMessage) Risk { return RiskRead }
func (t Discover) Execute(ctx context.Context, raw json.RawMessage) Result {
	a, err := Decode[struct {
		Query string `json:"query"`
	}](raw)
	if err != nil || len(a.Query) > 80 || t.Registry == nil {
		return Result{Content: "invalid tool search", IsError: true}
	}
	result := []model.ToolDefinition{}
	q := strings.ToLower(a.Query)
	for _, d := range t.Registry.Definitions() {
		if strings.Contains(strings.ToLower(d.Name+" "+d.Description), q) {
			result = append(result, d)
		}
	}
	b, _ := json.Marshal(result)
	return Result{Content: string(b)}
}
