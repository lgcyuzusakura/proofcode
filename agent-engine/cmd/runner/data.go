package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

type runnerDataGateway struct {
	runner *runner
	task   taskMessage
}

func (g *runnerDataGateway) Call(ctx context.Context, operation string, fields map[string]json.RawMessage) (json.RawMessage, error) {
	if g.runner == nil || g.runner.HTTP == nil || !taskIDPattern.MatchString(g.task.TaskID) || g.task.Attempt < 1 {
		return nil, errors.New("data gateway requires a bound task and attempt")
	}
	allowed := map[string]map[string]bool{
		"resources": {},
		"schema":    {"resourceId": true},
		"plan":      {"resourceId": true, "schemaVersion": true, "idempotencyKey": true, "ir": true},
		"status":    {"planId": true},
		"execute":   {"planId": true, "digest": true},
		"explain":   {"planId": true, "digest": true},
	}
	operationFields, ok := allowed[operation]
	if !ok {
		return nil, errors.New("unknown data gateway operation")
	}
	for field := range fields {
		if !operationFields[field] {
			return nil, errors.New("unexpected data gateway argument")
		}
	}
	base := "/internal/tasks/" + g.task.TaskID + "/data"
	method, path := http.MethodPost, base
	body := map[string]any{"attempt": g.task.Attempt}
	for k, v := range fields {
		body[k] = v
	}
	var planID string
	if value, ok := fields["planId"]; ok {
		if json.Unmarshal(value, &planID) != nil || !taskIDPattern.MatchString(planID) {
			return nil, errors.New("data gateway planId must be a UUID")
		}
		delete(body, "planId")
	}
	if operationFields["planId"] && planID == "" {
		return nil, errors.New("data gateway planId is required")
	}
	switch operation {
	case "resources":
		method, path, body = http.MethodGet, base+"/resources?attempt="+strconv.Itoa(g.task.Attempt), nil
	case "schema":
		path += "/schema"
	case "plan":
		path += "/plans"
	case "status":
		method, path, body = http.MethodGet, base+"/plans/"+url.PathEscape(planID)+"?attempt="+strconv.Itoa(g.task.Attempt), nil
	case "execute":
		path += "/plans/" + url.PathEscape(planID) + "/execute-approved"
	case "explain":
		path += "/plans/" + url.PathEscape(planID) + "/explain"
	default:
		return nil, errors.New("unknown data gateway operation")
	}
	var requestBody any
	if body != nil {
		requestBody = body
	}
	// A redirect can replay a mutation or forward the runner credential. Data
	// operations must have one request to the configured control-plane endpoint.
	client := *g.runner.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	boundRunner := runner{ControlPlane: g.runner.ControlPlane, Token: g.runner.Token, HTTP: &client}
	response, err := boundRunner.request(ctx, method, path, requestBody)
	if err != nil {
		return nil, errors.New("data gateway transport failed; inspect plan status before retrying")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return nil, errors.New("data gateway response interrupted; inspect plan status before retrying")
	}
	if response.StatusCode/100 != 2 {
		return nil, fmt.Errorf("data gateway rejected %s (HTTP %d)", operation, response.StatusCode)
	}
	if len(data) > 1<<20 {
		return nil, errors.New("data gateway response exceeds 1 MiB")
	}
	return json.RawMessage(data), nil
}
