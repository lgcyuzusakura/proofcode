package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type transportError struct{ code string }

func (e *transportError) Error() string { return "MCP HTTP request failed: " + e.code }

type httpStatusError struct{ status int }

func (e *httpStatusError) Error() string { return fmt.Sprintf("MCP HTTP status %d", e.status) }

type ToolPreview struct {
	Name          string         `json:"name"`
	QualifiedName string         `json:"qualifiedName"`
	Description   string         `json:"description"`
	InputSchema   map[string]any `json:"inputSchema"`
}

// Inspection describes discovery only. No tool is executed by this operation.
type Inspection struct {
	OK         bool          `json:"ok"`
	Code       string        `json:"code"`
	Stage      string        `json:"stage"`
	HTTPStatus int           `json:"httpStatus,omitempty"`
	Protocol   string        `json:"protocol,omitempty"`
	CheckedAt  time.Time     `json:"checkedAt"`
	ElapsedMS  int64         `json:"elapsedMs"`
	Tools      []ToolPreview `json:"tools"`
}

// InspectHTTP shares the Runner's transport, including DNS checks at dial time.
// The caller supplies a deadline; credentials and raw remote errors never leave it.
func InspectHTTP(ctx context.Context, config ServerConfig) (report Inspection) {
	start := time.Now()
	report = Inspection{Code: "configuration", Stage: "initialize", CheckedAt: start.UTC(), Tools: []ToolPreview{}}
	defer func() {
		if !report.OK {
			report.Tools = []ToolPreview{}
		}
		report.ElapsedMS = time.Since(start).Milliseconds()
	}()
	if !aliasPattern.MatchString(config.Name) || config.URL == "" || !validCredentialHeader(config.HeaderName, config.HeaderValue) {
		return report
	}
	client, err := StartHTTP(ctx, config)
	if err != nil {
		report.Code, report.HTTPStatus = inspectionError(ctx, err, "protocol")
		return report
	}
	defer client.Close()
	report.Protocol = client.protocol
	report.Stage = "catalog"
	definitions, err := client.ListTools(ctx)
	if err != nil {
		report.Code, report.HTTPStatus = inspectionError(ctx, err, "catalog")
		return report
	}
	for _, definition := range definitions {
		name, err := qualifiedToolName(config.Name, definition.Name)
		if err != nil {
			report.Code = "tool_name"
			return report
		}
		report.Tools = append(report.Tools, ToolPreview{Name: definition.Name, QualifiedName: name, Description: definition.Description, InputSchema: definition.InputSchema})
	}
	raw, err := json.Marshal(report.Tools)
	if err != nil || len(raw) > 512<<10 {
		report.Code, report.Tools = "catalog_size", []ToolPreview{}
		return report
	}
	// A remote service can echo Authorization in tool metadata, including map keys.
	for i := range report.Tools {
		value := &report.Tools[i]
		value.Name = redactInspectionText(value.Name, config.Bearer, config.HeaderValue)
		value.QualifiedName = redactInspectionText(value.QualifiedName, config.Bearer, config.HeaderValue)
		value.Description = redactInspectionText(value.Description, config.Bearer, config.HeaderValue)
		value.InputSchema = redactInspectionValue(value.InputSchema, config.Bearer, config.HeaderValue).(map[string]any)
	}
	raw, err = json.Marshal(report.Tools)
	if err != nil || len(raw) > 512<<10 {
		report.Code = "catalog_size"
		return report
	}
	report.OK, report.Code, report.Stage = true, "ok", "complete"
	return report
}

func inspectionError(ctx context.Context, err error, fallback string) (string, int) {
	if ctx.Err() != nil {
		return "timeout", 0
	}
	var transport *transportError
	if errors.As(err, &transport) {
		return transport.code, 0
	}
	var status *httpStatusError
	if errors.As(err, &status) {
		switch {
		case status.status == 401:
			return "authentication", status.status
		case status.status == 403:
			return "forbidden", status.status
		case status.status >= 300 && status.status < 400:
			return "redirect", status.status
		case status.status == 429:
			return "rate_limit", status.status
		default:
			return "http", status.status
		}
	}
	return fallback, 0
}

var bearerText = regexp.MustCompile(`(?i)bearer[ \t]+[A-Za-z0-9._~+/=-]+`)

func redactInspectionText(value string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return bearerText.ReplaceAllString(value, "Bearer [redacted]")
}

func redactInspectionValue(value any, secrets ...string) any {
	switch item := value.(type) {
	case string:
		return redactInspectionText(item, secrets...)
	case map[string]any:
		clean := make(map[string]any, len(item))
		for key, child := range item {
			clean[redactInspectionText(key, secrets...)] = redactInspectionValue(child, secrets...)
		}
		return clean
	case []any:
		clean := make([]any, len(item))
		for i, child := range item {
			clean[i] = redactInspectionValue(child, secrets...)
		}
		return clean
	default:
		return value
	}
}
