package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
)

func toolRequestPayload(call model.ToolCall, risk tool.Risk, resumed bool) map[string]any {
	var arguments any = json.RawMessage(call.Arguments)
	if strings.HasPrefix(call.Name, "data_") {
		var object map[string]any
		_ = json.Unmarshal(call.Arguments, &object)
		arguments = dataAuditFields(object, false)
	}
	return map[string]any{"tool": call.Name, "callId": call.ID, "risk": risk, "arguments": arguments, "resumed": resumed}
}

func toolResultPayload(call model.ToolCall, result tool.Result, resumed bool) map[string]any {
	content, metadata := result.Content, result.Metadata
	if strings.HasPrefix(call.Name, "data_") {
		hash := sha256.Sum256([]byte(content))
		content = fmt.Sprintf("Data tool result: error=%t bytes=%d sha256=%s", result.IsError, len(result.Content), hex.EncodeToString(hash[:]))
		metadata = dataAuditFields(metadata, true)
		metadata["redacted"] = true
	}
	return map[string]any{"tool": call.Name, "callId": call.ID, "result": content, "metadata": metadata, "resumed": resumed}
}

// Audit events carry resource/approval identity and counts, never row values,
// SQL, filters, mutation IR, credentials or arbitrary gateway error text.
func dataAuditFields(object map[string]any, includeCounts bool) map[string]any {
	result := map[string]any{}
	for _, key := range []string{"resourceId", "planId", "schemaVersion", "digest"} {
		if value, ok := object[key].(string); ok && auditIdentifier(value) {
			result[key] = value
		}
	}
	for _, key := range []string{"operation", "operationKind"} {
		if value, ok := object[key].(string); ok {
			switch value {
			case "select", "insert", "update", "delete", "schema", "preview", "execute", "query", "mutation":
				result[key] = value
			}
		}
	}
	for _, key := range []string{"readOnly", "mutation", "dryRun"} {
		if value, ok := object[key].(bool); ok {
			result[key] = value
		}
	}
	if includeCounts {
		for _, key := range []string{"rowCount", "columnCount", "affectedRows", "durationMs"} {
			switch value := object[key].(type) {
			case int:
				if value >= 0 {
					result[key] = value
				}
			case int64:
				if value >= 0 {
					result[key] = value
				}
			case float64:
				if value >= 0 {
					result[key] = value
				}
			}
		}
		for _, key := range []string{"errorCode", "policyVersion"} {
			if value, ok := object[key].(string); ok && auditIdentifier(value) {
				result[key] = value
			}
		}
		for _, key := range []string{"policyBlocked", "rollbackAttempted", "rollbackSucceeded"} {
			if value, ok := object[key].(bool); ok {
				result[key] = value
			}
		}
	}
	return result
}

func auditIdentifier(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:", r)) {
			return false
		}
	}
	return true
}
