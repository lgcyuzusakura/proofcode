package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

const runnerDataTaskID = "11111111-1111-1111-1111-111111111111"
const runnerDataPlanID = "22222222-2222-2222-2222-222222222222"
const runnerDataResourceID = "33333333-3333-3333-3333-333333333333"

func runnerDataFields(t *testing.T, operation string) map[string]json.RawMessage {
	t.Helper()
	fields := map[string]any{}
	switch operation {
	case "schema":
		fields["resourceId"] = runnerDataResourceID
	case "plan":
		fields["resourceId"] = runnerDataResourceID
		fields["schemaVersion"] = strings.Repeat("a", 64)
		fields["idempotencyKey"] = "task-7-plan-1"
		fields["ir"] = map[string]any{"operation": "select", "table": "users", "columns": []string{"id"}, "limit": 10}
	case "status":
		fields["planId"] = runnerDataPlanID
	case "execute", "explain":
		fields["planId"] = runnerDataPlanID
		fields["digest"] = strings.Repeat("b", 64)
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRunnerDataGatewayBindsPathsMethodsArgumentsAndAuthentication(t *testing.T) {
	base := "/internal/tasks/" + runnerDataTaskID + "/data"
	for _, tc := range []struct {
		operation, method, target string
	}{
		{"resources", http.MethodGet, base + "/resources?attempt=7"},
		{"schema", http.MethodPost, base + "/schema"},
		{"plan", http.MethodPost, base + "/plans"},
		{"status", http.MethodGet, base + "/plans/" + runnerDataPlanID + "?attempt=7"},
		{"execute", http.MethodPost, base + "/plans/" + runnerDataPlanID + "/execute-approved"},
		{"explain", http.MethodPost, base + "/plans/" + runnerDataPlanID + "/explain"},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			fields := runnerDataFields(t, tc.operation)
			before, _ := json.Marshal(fields)
			var requests atomic.Int32
			const response = " {\n\"status\":\"COMMITTED\",\"result\":{\"affectedRows\":1}}\n"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.Method != tc.method || request.URL.RequestURI() != tc.target {
					t.Errorf("wrong bound route: %s %s", request.Method, request.URL.RequestURI())
				}
				if request.Header.Get("Authorization") != "Bearer runner-secret" {
					t.Error("missing runner authentication")
				}
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
				}
				if tc.method == http.MethodGet {
					if len(body) != 0 || request.Header.Get("Content-Type") != "" || request.URL.Query().Get("attempt") != "7" {
						t.Errorf("GET must bind attempt by query without body: %s", body)
					}
				} else {
					if request.Header.Get("Content-Type") != "application/json" {
						t.Error("missing JSON request content type")
					}
					var received map[string]any
					if err := json.Unmarshal(body, &received); err != nil {
						t.Errorf("decode request: %v", err)
					}
					var expected map[string]any
					if err := json.Unmarshal(before, &expected); err != nil {
						t.Errorf("decode expected: %v", err)
					}
					delete(expected, "planId")
					expected["attempt"] = float64(7)
					if !reflect.DeepEqual(received, expected) {
						t.Errorf("body did not bind attempt and preserve allowed fields: got=%+v want=%+v", received, expected)
					}
				}
				_, _ = io.WriteString(w, response)
			}))
			defer server.Close()
			gateway := runnerDataGateway{runner: &runner{ControlPlane: server.URL, Token: "runner-secret", HTTP: server.Client()}, task: taskMessage{TaskID: runnerDataTaskID, Attempt: 7}}
			got, err := gateway.Call(context.Background(), tc.operation, fields)
			after, _ := json.Marshal(fields)
			if err != nil || string(got) != response || requests.Load() != 1 || string(before) != string(after) {
				t.Fatalf("valid response/arguments altered: response=%q err=%v requests=%d", got, err, requests.Load())
			}
		})
	}
}

type runnerDataRoundTripper func(*http.Request) (*http.Response, error)

func (f runnerDataRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestRunnerDataGatewayRejectsUntrustedFieldsAndPathForgingBeforeHTTP(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: runnerDataRoundTripper(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("unexpected HTTP call")
	})}
	r := &runner{ControlPlane: "http://example.invalid", Token: "secret", HTTP: client}
	for _, operation := range []string{"resources", "schema", "plan", "status", "execute", "explain"} {
		for _, field := range []string{"attempt", "projectId", "workspaceId", "taskId", "runnerId", "capability", "approved", "approvalToken", "connectionString", "sql"} {
			t.Run(operation+"/"+field, func(t *testing.T) {
				fields := runnerDataFields(t, operation)
				fields[field] = json.RawMessage(`"forged"`)
				gateway := runnerDataGateway{runner: r, task: taskMessage{TaskID: runnerDataTaskID, Attempt: 7}}
				if response, err := gateway.Call(context.Background(), operation, fields); err == nil || response != nil {
					t.Fatalf("untrusted field accepted: response=%s err=%v", response, err)
				}
			})
		}
	}
	for _, operation := range []string{"", "approve", "EXECUTE", "execute-approved", "../schema", "resources?attempt=1"} {
		t.Run("unknown operation/"+operation, func(t *testing.T) {
			gateway := runnerDataGateway{runner: r, task: taskMessage{TaskID: runnerDataTaskID, Attempt: 7}}
			if _, err := gateway.Call(context.Background(), operation, map[string]json.RawMessage{}); err == nil {
				t.Fatal("unknown operation accepted")
			}
		})
	}
	for _, task := range []taskMessage{{TaskID: "", Attempt: 7}, {TaskID: "../other/data", Attempt: 7}, {TaskID: runnerDataTaskID + "?attempt=1", Attempt: 7}, {TaskID: runnerDataTaskID, Attempt: 0}, {TaskID: runnerDataTaskID, Attempt: -1}} {
		t.Run("invalid task/"+task.TaskID, func(t *testing.T) {
			gateway := runnerDataGateway{runner: r, task: task}
			if _, err := gateway.Call(context.Background(), "resources", map[string]json.RawMessage{}); err == nil {
				t.Fatal("unbound task or attempt accepted")
			}
		})
	}
	for _, operation := range []string{"status", "execute", "explain"} {
		for _, value := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`123`), json.RawMessage(`""`), json.RawMessage(`"../../other/execute"`), json.RawMessage(`"` + runnerDataPlanID + `?attempt=1"`), json.RawMessage(`"` + runnerDataPlanID + `%2fexecute"`)} {
			t.Run("forged plan/"+operation+"/"+string(value), func(t *testing.T) {
				fields := runnerDataFields(t, operation)
				if value == nil {
					delete(fields, "planId")
				} else {
					fields["planId"] = value
				}
				gateway := runnerDataGateway{runner: r, task: taskMessage{TaskID: runnerDataTaskID, Attempt: 7}}
				if _, err := gateway.Call(context.Background(), operation, fields); err == nil {
					t.Fatal("forged plan route accepted")
				}
			})
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid calls reached HTTP: %d", requests.Load())
	}
}

type runnerDataTrackedBody struct {
	reader io.Reader
	err    error
	closed bool
}

func (b *runnerDataTrackedBody) Read(p []byte) (int, error) {
	if b.reader != nil {
		n, err := b.reader.Read(p)
		if err != io.EOF {
			return n, err
		}
		b.reader = nil
		if n > 0 {
			return n, nil
		}
	}
	if b.err != nil {
		return 0, b.err
	}
	return 0, io.EOF
}

func (b *runnerDataTrackedBody) Close() error { b.closed = true; return nil }

func TestRunnerDataExecuteFailureNeverRetriesOrLeaksResponseSecrets(t *testing.T) {
	const secret = "response-password-and-private-row-sentinel"
	for _, tc := range []struct {
		name      string
		status    int
		content   string
		readErr   error
		transport error
	}{
		{name: "HTTP unauthorized", status: http.StatusUnauthorized, content: secret},
		{name: "HTTP conflict", status: http.StatusConflict, content: secret},
		{name: "HTTP throttled", status: http.StatusTooManyRequests, content: secret},
		{name: "HTTP unavailable", status: http.StatusServiceUnavailable, content: secret},
		{name: "read interrupted", status: http.StatusOK, content: `{"status":"COMMITTED","secret":"` + secret, readErr: errors.New("connection reset: " + secret)},
		{name: "oversized", status: http.StatusOK, content: `{"private":"` + secret + strings.Repeat("x", 1<<20) + `"}`},
		{name: "transport failure", transport: errors.New("transport credential: " + secret)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			body := &runnerDataTrackedBody{reader: strings.NewReader(tc.content), err: tc.readErr}
			client := &http.Client{Transport: runnerDataRoundTripper(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/execute-approved") {
					t.Fatalf("execution sent to wrong endpoint: %s %s", request.Method, request.URL)
				}
				if tc.transport != nil {
					return nil, tc.transport
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: body, Request: request}, nil
			})}
			gateway := runnerDataGateway{runner: &runner{ControlPlane: "http://control.invalid", Token: "runner-token", HTTP: client}, task: taskMessage{TaskID: runnerDataTaskID, Attempt: 7}}
			response, err := gateway.Call(context.Background(), "execute", runnerDataFields(t, "execute"))
			if err == nil || response != nil || calls != 1 {
				t.Fatalf("execution failure replayed or became successful: response=%s err=%v calls=%d", response, err, calls)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "runner-token") {
				t.Fatalf("gateway error exposed secrets: %v", err)
			}
			if tc.transport == nil && !body.closed {
				t.Fatal("response body not closed on failure")
			}
		})
	}
}

func TestRunnerDataGatewayRejectsUnavailableRunnerBinding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		runner *runner
	}{
		{name: "nil runner"},
		{name: "nil HTTP client", runner: &runner{ControlPlane: "http://example.invalid", Token: "runner-secret"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := runnerDataGateway{runner: tc.runner, task: taskMessage{TaskID: runnerDataTaskID, Attempt: 7}}
			response, err := gateway.Call(context.Background(), "execute", runnerDataFields(t, "execute"))
			if err == nil || response != nil || strings.Contains(err.Error(), "runner-secret") {
				t.Fatalf("unavailable runner binding did not fail safely: response=%s err=%v", response, err)
			}
		})
	}
}

func TestRunnerDataExecuteRedirectNeverReplaysOrForwardsCredentials(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		for _, crossHost := range []bool{false, true} {
			name := "same host"
			if crossHost {
				name = "other host"
			}
			t.Run(http.StatusText(status)+"/"+name, func(t *testing.T) {
				var sourceCalls, targetCalls, targetCredentials atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					targetCalls.Add(1)
					if request.Header.Get("Authorization") != "" {
						targetCredentials.Add(1)
					}
					_, _ = io.WriteString(w, `{"status":"COMMITTED"}`)
				}))
				defer target.Close()
				source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					if request.URL.Path == "/redirected-execute" {
						targetCalls.Add(1)
						if request.Header.Get("Authorization") != "" {
							targetCredentials.Add(1)
						}
						_, _ = io.WriteString(w, `{"status":"COMMITTED"}`)
						return
					}
					sourceCalls.Add(1)
					location := "/redirected-execute"
					if crossHost {
						location = target.URL + "/redirected-execute"
					}
					w.Header().Set("Location", location)
					w.WriteHeader(status)
					_, _ = io.WriteString(w, "response-secret-sentinel")
				}))
				defer source.Close()
				client := source.Client()
				redirectCallbacks := 0
				client.CheckRedirect = func(*http.Request, []*http.Request) error {
					redirectCallbacks++
					return nil
				}
				gateway := runnerDataGateway{runner: &runner{ControlPlane: source.URL, Token: "runner-secret", HTTP: client}, task: taskMessage{TaskID: runnerDataTaskID, Attempt: 7}}
				response, err := gateway.Call(context.Background(), "execute", runnerDataFields(t, "execute"))
				if err == nil || response != nil || sourceCalls.Load() != 1 || targetCalls.Load() != 0 || targetCredentials.Load() != 0 || redirectCallbacks != 0 {
					t.Fatalf("redirect replayed/forwarded execution: response=%s err=%v source=%d target=%d credentials=%d callback=%d", response, err, sourceCalls.Load(), targetCalls.Load(), targetCredentials.Load(), redirectCallbacks)
				}
				if strings.Contains(err.Error(), "response-secret-sentinel") || strings.Contains(err.Error(), "runner-secret") {
					t.Fatalf("redirect error leaked credentials or response body: %v", err)
				}
				// Only data requests disable redirects; the shared runner client
				// must retain its configured behavior for its other consumers.
				if err := client.CheckRedirect(nil, nil); err != nil || redirectCallbacks != 1 {
					t.Fatal("gateway mutated shared HTTP redirect policy")
				}
			})
		}
	}
}
