package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

// DataGateway is the only route to project data. Connection credentials and
// approval capabilities are kept in the control plane, outside model context.
type DataGateway interface {
	Call(context.Context, string, map[string]json.RawMessage) (json.RawMessage, error)
}

type DataTool struct {
	Operation string
	Gateway   DataGateway
}

var dataUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var dataDigest = regexp.MustCompile(`(?i)^[0-9a-f]{64}$`)

func (t DataTool) Definition() model.ToolDefinition {
	properties := map[string]any{}
	required := []string{}
	stringField := func(name string) {
		properties[name] = map[string]any{"type": "string"}
		required = append(required, name)
	}
	switch t.Operation {
	case "schema":
		stringField("resourceId")
	case "plan":
		stringField("resourceId")
		stringField("schemaVersion")
		stringField("idempotencyKey")
		properties["ir"] = map[string]any{"type": "object", "description": "Typed single-table SQL or Redis IR. Raw SQL, credentials and approval tokens are not accepted."}
		required = append(required, "ir")
	case "status":
		stringField("planId")
	case "execute", "explain":
		stringField("planId")
		stringField("digest")
	}
	descriptions := map[string]string{
		"resources": "List database and Redis connections bound to the current project. Does not return credentials.",
		"schema":    "Inspect the selected project resource schema and obtain a version for planning.",
		"plan":      "Create an immutable, idempotent SQL/Redis operation plan. This never executes the plan. Use schemaVersion from data_schema.",
		"status":    "Inspect the current project operation plan and its approval/execution state.",
		"explain":   "Run PostgreSQL EXPLAIN without ANALYZE for a read query plan. Does not execute mutations.",
		"execute":   "Execute one immutable data plan after explicit user approval. Requires exact planId and digest. The agent cannot approve it.",
	}
	return model.ToolDefinition{Name: "data_" + t.Operation, Description: descriptions[t.Operation], Parameters: map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}
}

func (t DataTool) Risk(json.RawMessage) Risk {
	if t.Operation == "execute" {
		return RiskWrite
	}
	return RiskRead
}

func (t DataTool) Execute(ctx context.Context, raw json.RawMessage) Result {
	switch t.Operation {
	case "resources", "schema", "plan", "status", "explain", "execute":
	default:
		return failed(errors.New("unknown data tool operation"))
	}
	if t.Gateway == nil {
		return failed(errors.New("project data gateway is unavailable"))
	}
	definition := t.Definition()
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		return failed(errors.New("data tool arguments must be a JSON object"))
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return failed(errors.New("data tool arguments must contain exactly one object"))
	}
	properties := definition.Parameters["properties"].(map[string]any)
	for field := range fields {
		if _, ok := properties[field]; !ok {
			return failed(fmt.Errorf("unexpected data tool argument %s", field))
		}
	}
	for _, field := range definition.Parameters["required"].([]string) {
		value, ok := fields[field]
		if !ok {
			return failed(fmt.Errorf("missing data tool argument %s", field))
		}
		if field == "ir" {
			if len(value) > 128<<10 || len(value) == 0 || value[0] != '{' {
				return failed(errors.New("ir must be a bounded object"))
			}
			continue
		}
		var text string
		if json.Unmarshal(value, &text) != nil || text == "" {
			return failed(fmt.Errorf("%s must be a nonempty string", field))
		}
		if (field == "planId" || field == "resourceId") && !dataUUID.MatchString(text) {
			return failed(fmt.Errorf("%s must be a UUID", field))
		}
		if (field == "digest" || field == "schemaVersion") && !dataDigest.MatchString(text) {
			return failed(fmt.Errorf("%s must be a SHA256 digest", field))
		}
		if field == "idempotencyKey" && len(text) > 200 {
			return failed(errors.New("idempotencyKey exceeds 200 bytes"))
		}
	}
	response, err := t.Gateway.Call(ctx, t.Operation, fields)
	if err != nil {
		return Result{Content: err.Error(), IsError: true, Metadata: map[string]any{"errorCode": "DATA_GATEWAY_REJECTED"}}
	}
	if len(response) > 1<<20 || !json.Valid(response) {
		return failed(errors.New("data gateway returned an invalid or oversized response"))
	}
	metadata := map[string]any{}
	isError := false
	if t.Operation == "execute" {
		var result struct {
			Status string         `json:"status"`
			Result map[string]any `json:"result"`
		}
		if json.Unmarshal(response, &result) != nil || result.Status == "" {
			return failed(errors.New("data execution returned no durable status"))
		}
		metadata["status"] = result.Status
		isError = result.Status != "COMMITTED" && result.Status != "SUCCEEDED" && result.Status != "COMPENSATED"
		for _, key := range []string{"affectedRows", "affectedKeys", "returnedRows", "truncated", "errorCode"} {
			if value, ok := result.Result[key]; ok {
				metadata[key] = value
			}
		}
	}
	return Result{Content: string(response), Metadata: metadata, IsError: isError}
}
