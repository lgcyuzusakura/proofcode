package tool

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const dataTestUUID = "11111111-1111-1111-1111-111111111111"

type dataGatewayCall struct {
	operation string
	fields    map[string]json.RawMessage
}

type recordingDataGateway struct {
	calls    []dataGatewayCall
	response json.RawMessage
	err      error
	context  context.Context
}

func (g *recordingDataGateway) Call(ctx context.Context, operation string, fields map[string]json.RawMessage) (json.RawMessage, error) {
	copyFields := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		copyFields[key] = append(json.RawMessage(nil), value...)
	}
	g.calls = append(g.calls, dataGatewayCall{operation: operation, fields: copyFields})
	g.context = ctx
	return g.response, g.err
}

func dataTestArguments(operation string) map[string]any {
	switch operation {
	case "schema":
		return map[string]any{"resourceId": dataTestUUID}
	case "plan":
		return map[string]any{"resourceId": dataTestUUID, "schemaVersion": strings.Repeat("a", 64), "idempotencyKey": "task-1-plan-1", "ir": map[string]any{"operation": "select", "table": "users", "columns": []string{"id"}, "limit": 10}}
	case "status":
		return map[string]any{"planId": dataTestUUID}
	case "execute", "explain":
		return map[string]any{"planId": dataTestUUID, "digest": strings.Repeat("b", 64)}
	default:
		return map[string]any{}
	}
}

func marshalDataTestArguments(t *testing.T, fields map[string]any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDataToolForwardsOnlyValidatedOperationArguments(t *testing.T) {
	for _, operation := range []string{"resources", "schema", "plan", "status", "explain", "execute"} {
		t.Run(operation, func(t *testing.T) {
			gateway := &recordingDataGateway{response: json.RawMessage(`{"status":"COMMITTED","result":{"affectedRows":1}}`)}
			selected := DataTool{Operation: operation, Gateway: gateway}
			arguments := marshalDataTestArguments(t, dataTestArguments(operation))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := selected.Execute(ctx, arguments)
			if result.IsError || len(gateway.calls) != 1 {
				t.Fatalf("valid operation rejected: result=%+v calls=%+v", result, gateway.calls)
			}
			var expected map[string]json.RawMessage
			if err := json.Unmarshal(arguments, &expected); err != nil {
				t.Fatal(err)
			}
			if gateway.calls[0].operation != operation || !reflect.DeepEqual(gateway.calls[0].fields, expected) || gateway.context != ctx {
				t.Fatalf("gateway operation, arguments or context changed: %+v", gateway.calls)
			}
			definition := selected.Definition()
			if definition.Name != "data_"+operation || definition.Parameters["additionalProperties"] != false {
				t.Fatalf("tool schema permits uncontrolled arguments: %+v", definition)
			}
			wantRisk := RiskRead
			if operation == "execute" {
				wantRisk = RiskWrite
			}
			if selected.Risk(arguments) != wantRisk {
				t.Fatalf("operation risk = %q, want %q", selected.Risk(arguments), wantRisk)
			}
		})
	}
}

func TestDataToolRejectsUnknownOperationBeforeGateway(t *testing.T) {
	for _, operation := range []string{"", "approve", "raw_sql", "EXECUTE", "execute-approved", "../execute"} {
		t.Run(operation, func(t *testing.T) {
			gateway := &recordingDataGateway{response: json.RawMessage(`{"status":"COMMITTED"}`)}
			result := (DataTool{Operation: operation, Gateway: gateway}).Execute(context.Background(), json.RawMessage(`{}`))
			if !result.IsError || len(gateway.calls) != 0 {
				t.Fatalf("unknown operation reached gateway: result=%+v calls=%+v", result, gateway.calls)
			}
		})
	}
}

func TestDataToolRejectsCallerBindingAndCapabilityFields(t *testing.T) {
	// Identity and authorization are bound by the runner/control plane, never
	// by model-supplied parameters, even if the supplied value looks valid.
	for _, operation := range []string{"resources", "schema", "plan", "status", "explain", "execute"} {
		for _, field := range []string{"projectId", "workspaceId", "taskId", "attempt", "runnerId", "provider", "environment", "approved", "capability", "approvalToken", "sql", "connectionString", "password"} {
			t.Run(operation+"/"+field, func(t *testing.T) {
				gateway := &recordingDataGateway{response: json.RawMessage(`{}`)}
				arguments := dataTestArguments(operation)
				arguments[field] = "caller-controlled"
				result := (DataTool{Operation: operation, Gateway: gateway}).Execute(context.Background(), marshalDataTestArguments(t, arguments))
				if !result.IsError || len(gateway.calls) != 0 {
					t.Fatalf("untrusted binding reached gateway: result=%+v calls=%+v", result, gateway.calls)
				}
			})
		}
	}
}

func TestDataToolRejectsMalformedAndInvalidParametersBeforeGateway(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		raw       json.RawMessage
	}{
		{"empty", "resources", nil},
		{"malformed", "resources", json.RawMessage(`{"broken":`)},
		{"null", "resources", json.RawMessage(`null`)},
		{"array", "resources", json.RawMessage(`[]`)},
		{"string", "resources", json.RawMessage(`"object"`)},
		{"two objects", "resources", json.RawMessage(`{} {}`)},
		{"trailing primitive", "resources", json.RawMessage(`{} true`)},
		{"trailing junk", "resources", json.RawMessage(`{} garbage`)},
	}
	for _, operation := range []string{"schema", "plan", "status", "explain", "execute"} {
		for field := range dataTestArguments(operation) {
			missing := dataTestArguments(operation)
			delete(missing, field)
			tests = append(tests, struct {
				name      string
				operation string
				raw       json.RawMessage
			}{operation + "/missing " + field, operation, marshalDataTestArguments(t, missing)})
			for name, invalid := range map[string]any{"null": nil, "empty": "", "number": 123, "array": []any{}, "boolean": true} {
				arguments := dataTestArguments(operation)
				arguments[field] = invalid
				tests = append(tests, struct {
					name      string
					operation string
					raw       json.RawMessage
				}{operation + "/" + field + "/" + name, operation, marshalDataTestArguments(t, arguments)})
			}
		}
	}
	for _, tc := range []struct {
		name, operation, field string
		value                  any
	}{
		{"bad resource UUID", "schema", "resourceId", "resource-1"},
		{"bad plan UUID", "execute", "planId", "not-a-uuid"},
		{"short digest", "execute", "digest", strings.Repeat("a", 63)},
		{"nonhex digest", "explain", "digest", strings.Repeat("g", 64)},
		{"bad schema version", "plan", "schemaVersion", "v1"},
		{"long key", "plan", "idempotencyKey", strings.Repeat("x", 201)},
		{"multibyte long key", "plan", "idempotencyKey", strings.Repeat("界", 67)},
		{"long IR", "plan", "ir", map[string]any{"value": strings.Repeat("x", 128<<10)}},
	} {
		arguments := dataTestArguments(tc.operation)
		arguments[tc.field] = tc.value
		tests = append(tests, struct {
			name      string
			operation string
			raw       json.RawMessage
		}{tc.name, tc.operation, marshalDataTestArguments(t, arguments)})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gateway := &recordingDataGateway{response: json.RawMessage(`{}`)}
			result := (DataTool{Operation: tc.operation, Gateway: gateway}).Execute(context.Background(), tc.raw)
			if !result.IsError || len(gateway.calls) != 0 {
				t.Fatalf("invalid parameters reached gateway: result=%+v calls=%+v", result, gateway.calls)
			}
		})
	}
}

func TestDataToolPropagatesGatewayIRValidationAndTransportFailures(t *testing.T) {
	arguments := dataTestArguments("plan")
	arguments["ir"] = map[string]any{"operation": "raw_sql", "sql": "DELETE FROM users"}
	gateway := &recordingDataGateway{err: errors.New("typed IR operation is not allowed")}
	result := (DataTool{Operation: "plan", Gateway: gateway}).Execute(context.Background(), marshalDataTestArguments(t, arguments))
	if !result.IsError || result.Metadata["errorCode"] != "DATA_GATEWAY_REJECTED" || len(gateway.calls) != 1 {
		t.Fatalf("authoritative gateway rejection was lost: %+v calls=%+v", result, gateway.calls)
	}
	for name, response := range map[string]json.RawMessage{
		"empty":         nil,
		"malformed":     json.RawMessage(`{"status":`),
		"two responses": json.RawMessage(`{} {}`),
		"oversized":     json.RawMessage(`{"data":"` + strings.Repeat("x", 1<<20) + `"}`),
	} {
		t.Run(name, func(t *testing.T) {
			g := &recordingDataGateway{response: response}
			got := (DataTool{Operation: "resources", Gateway: g}).Execute(context.Background(), json.RawMessage(`{}`))
			if !got.IsError || len(g.calls) != 1 {
				t.Fatalf("invalid response accepted: %+v", got)
			}
		})
	}
	if result := (DataTool{Operation: "resources"}).Execute(context.Background(), json.RawMessage(`{}`)); !result.IsError {
		t.Fatal("missing gateway returned success")
	}
}

func TestDataExecuteRequiresKnownDurableSuccessStatus(t *testing.T) {
	arguments := marshalDataTestArguments(t, dataTestArguments("execute"))
	for _, status := range []string{"COMMITTED", "SUCCEEDED", "COMPENSATED", "FAILED", "CANCELLED", "ROLLED_BACK", "COMMIT_UNKNOWN", "COMPENSATION_UNKNOWN", "EXECUTING", "PENDING", "UNRECOGNIZED", "committed", ""} {
		t.Run("status/"+status, func(t *testing.T) {
			response, _ := json.Marshal(map[string]any{"status": status, "result": map[string]any{"affectedRows": 2, "affectedKeys": 1, "returnedRows": 3, "truncated": false, "errorCode": "TEST", "rows": []any{"private-row"}, "values": "private-value", "connectionString": "private-connection"}})
			gateway := &recordingDataGateway{response: response}
			result := (DataTool{Operation: "execute", Gateway: gateway}).Execute(context.Background(), arguments)
			wantSuccess := status == "COMMITTED" || status == "SUCCEEDED" || status == "COMPENSATED"
			if result.IsError == wantSuccess || len(gateway.calls) != 1 {
				t.Fatalf("status %q: result=%+v", status, result)
			}
			if status == "" {
				return
			}
			if result.Metadata["status"] != status || result.Metadata["affectedRows"] != float64(2) {
				t.Fatalf("durable status/count metadata lost: %+v", result.Metadata)
			}
			for key := range result.Metadata {
				switch key {
				case "status", "affectedRows", "affectedKeys", "returnedRows", "truncated", "errorCode":
				default:
					t.Fatalf("gateway values leaked into metadata: %s", key)
				}
			}
		})
	}
	for _, response := range []string{`{}`, `null`, `[]`, `"COMMITTED"`, `{"status":null}`, `{"status":true}`, `{"status":{}}`, `{"status":"COMMITTED","result":[]}`} {
		t.Run("invalid/"+response, func(t *testing.T) {
			gateway := &recordingDataGateway{response: json.RawMessage(response)}
			result := (DataTool{Operation: "execute", Gateway: gateway}).Execute(context.Background(), arguments)
			if !result.IsError {
				t.Fatalf("malformed durable execution response returned success: %s", response)
			}
		})
	}
}
