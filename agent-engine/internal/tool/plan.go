package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/durablefile"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"os"
	"path/filepath"
	"sync"
)

type PlanStep struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"`
}
type PlanState struct {
	Version int64      `json:"version"`
	Summary string     `json:"summary"`
	Steps   []PlanStep `json:"steps"`
}
type Plan struct {
	Path, TaskID string
	Events       *event.SequencedSink
	mu           sync.Mutex
}

func (p *Plan) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "update_plan", Description: "Read or update the task's durable plan. Use get before editing, then set with expectedVersion. Keep at most one step in_progress; do not mark unverified work completed. Changes affect only internal planning state, never project files.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"action": map[string]any{"type": "string", "enum": []string{"get", "set"}}, "expectedVersion": map[string]any{"type": "integer", "minimum": 0}, "summary": map[string]any{"type": "string", "maxLength": 2000}, "steps": map[string]any{"type": "array", "maxItems": 32, "items": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed", "blocked"}}}, "required": []string{"id", "text", "status"}, "additionalProperties": false}}}, "required": []string{"action"}, "additionalProperties": false}}
}

// Internal plan bookkeeping is allowed in read-only investigation mode.
func (p *Plan) Risk(json.RawMessage) Risk { return RiskRead }
func (p *Plan) Execute(ctx context.Context, raw json.RawMessage) Result {
	p.mu.Lock()
	defer p.mu.Unlock()
	fail := func(message string) Result { return Result{Content: message, IsError: true} }
	var input struct {
		Action          string     `json:"action"`
		ExpectedVersion *int64     `json:"expectedVersion"`
		Summary         string     `json:"summary"`
		Steps           []PlanStep `json:"steps"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return fail("invalid plan arguments")
	}
	state := PlanState{Steps: []PlanStep{}}
	if data, err := os.ReadFile(p.Path); err == nil {
		if json.Unmarshal(data, &state) != nil {
			return fail("saved plan is invalid")
		}
	} else if !os.IsNotExist(err) {
		return fail("cannot read plan")
	}
	if input.Action == "get" {
		data, _ := json.Marshal(state)
		return Result{Content: string(data)}
	}
	if input.Action != "set" || input.ExpectedVersion == nil || *input.ExpectedVersion != state.Version || len(input.Steps) == 0 || len(input.Steps) > 32 || len([]rune(input.Summary)) > 2000 {
		return fail("plan version changed or invalid plan; get current plan before setting")
	}
	seen := map[string]bool{}
	active := 0
	for _, step := range input.Steps {
		if step.ID == "" || len(step.ID) > 80 || seen[step.ID] || step.Text == "" || len([]rune(step.Text)) > 500 {
			return fail("invalid or duplicate plan step")
		}
		seen[step.ID] = true
		switch step.Status {
		case "in_progress":
			active++
		case "pending", "completed", "blocked":
		default:
			return fail("invalid step status")
		}
	}
	if active > 1 {
		return fail("at most one plan step may be in progress")
	}
	if err := ctx.Err(); err != nil {
		return fail(err.Error())
	}
	state = PlanState{Version: state.Version + 1, Summary: input.Summary, Steps: input.Steps}
	data, _ := json.Marshal(state)
	if err := os.MkdirAll(filepath.Dir(p.Path), 0700); err != nil {
		return fail("cannot create planning state")
	}
	if err := os.WriteFile(p.Path+".tmp", data, 0600); err != nil {
		return fail("cannot save plan")
	}
	if err := durablefile.Rename(p.Path+".tmp", p.Path); err != nil {
		return fail("cannot commit plan")
	}
	if p.Events != nil {
		if err := p.Events.Emit(ctx, p.TaskID, "plan.updated", map[string]any{"version": state.Version, "summary": state.Summary, "steps": state.Steps}); err != nil {
			return fail(fmt.Sprintf("plan saved but event delivery failed: %v", err))
		}
	}
	return Result{Content: string(data)}
}
