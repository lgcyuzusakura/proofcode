package main

import (
	"context"
	"encoding/json"
	"fmt"
	sharedconfig "github.com/proofcode-dev/proofcode/agent-engine/pkg/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicitly opt in: this sends synthetic tasks to the locally configured model.
// Credentials never enter test output, fixture files or the evaluation report.
func TestStandaloneLiveModelTasks(t *testing.T) {
	if os.Getenv("PROOFCODE_LIVE_MODEL_EVAL") != "1" {
		t.Skip("real model evaluation is opt-in")
	}
	configuration, err := sharedconfig.Load()
	if err != nil || configuration.APIKey == "" || configuration.Model == "" {
		t.Fatal("a configured local model is required")
	}
	bundle := os.Getenv("PROOFCODE_STANDALONE_TEST_BUNDLE")
	if bundle == "" {
		t.Fatal("a prepared standalone sidecar is required")
	}
	root := t.TempDir()
	app := &App{ctx: context.Background(), registryDir: filepath.Join(root, "profile"), desktopRoot: filepath.Join(root, "desktop")}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	run, err := app.startStandalone(ctx, bundle)
	if err != nil {
		t.Fatal("standalone startup failed")
	}
	app.standalone = run
	defer app.closeStandalone()
	login, err := app.GetStandaloneSession()
	if err != nil {
		t.Fatal("local session failed")
	}
	session := login["token"].(string)
	request := func(method, path string, body any, target any) error {
		data, _ := json.Marshal(body)
		response, e := app.ProxyRequest(path, method, string(data), "Bearer "+session, "")
		if e != nil {
			return e
		}
		if response.Status/100 != 2 {
			return &liveEvaluationHTTPError{response.Status}
		}
		if target != nil {
			return json.Unmarshal([]byte(response.Body), target)
		}
		return nil
	}
	var provider map[string]any
	if err = request("POST", "/api/model-providers", map[string]any{"name": "Real model evaluation", "baseUrl": configuration.BaseURL, "apiKey": configuration.APIKey, "wireApi": configuration.WireAPI}, &provider); err != nil {
		t.Fatal("provider configuration failed")
	}
	providerID := provider["id"].(string)
	if err = request("POST", "/api/model-providers/"+providerID+"/models/refresh", nil, &provider); err != nil {
		t.Fatal("real provider model catalog unavailable")
	}
	local, err := app.BootstrapScratchProject("live-eval-20261023", "synthetic-clamp")
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"clamp.cjs":      "exports.clamp = (value, min, max) => Math.min(min, Math.max(value, max));\n",
		"clamp.test.cjs": "const assert = require('node:assert/strict'); const { clamp } = require('./clamp.cjs'); for (const [v, lo, hi, want] of [[-1,0,5,0],[3,0,5,3],[8,0,5,5],[0,0,5,0],[5,0,5,5]]) assert.equal(clamp(v,lo,hi),want); console.log('clamp cases passed');\n",
	} {
		if err = os.WriteFile(filepath.Join(local.Path, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var project map[string]any
	if err = request("POST", "/api/projects", map[string]any{"name": "Synthetic live evaluation", "sourceKind": "LOCAL_FOLDER", "localHandle": local.LocalHandle, "defaultBranch": "main"}, &project); err != nil {
		t.Fatal(err)
	}
	projectID := project["id"].(string)
	var scope map[string]any
	if err = request("POST", "/api/projects/"+projectID+"/default-scope", nil, &scope); err != nil {
		t.Fatal(err)
	}
	source, err := app.CaptureProjectSource(local.LocalHandle)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err = request("POST", "/api/projects/"+projectID+"/sources", map[string]any{"workspaceId": scope["workspaceId"], "manifestHash": source.ManifestHash, "files": source.Files}, &snapshot); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		Mode         string `json:"mode"`
		Passed       bool   `json:"passed"`
		Status       string `json:"status"`
		Failure      string `json:"failure,omitempty"`
		ToolCalls    int    `json:"toolCalls"`
		Approvals    int    `json:"approvals"`
		Milliseconds int64  `json:"milliseconds"`
	}
	report := struct {
		Fixture bool      `json:"fixture"`
		Date    string    `json:"date"`
		Model   string    `json:"model"`
		Results []outcome `json:"results"`
	}{false, time.Now().UTC().Format(time.RFC3339), configuration.Model, nil}
	defer func() {
		if file := os.Getenv("PROOFCODE_LIVE_MODEL_REPORT"); file != "" {
			data, _ := json.MarshalIndent(report, "", "  ")
			_ = os.WriteFile(file, data, 0600)
		}
	}()
	for _, scenario := range []struct{ mode, prompt string }{
		{"CHAT", "只返回 17+25 的十进制结果，不要其他文字。"},
		{"PLAN", "读取 clamp.cjs 和 clamp.test.cjs，解释现有 clamp 的具体错误并给出修复计划。不要改动文件，不要执行命令。"},
		{"CODE", "修复 clamp.cjs 的上下界截断错误，保持接口不变，不修改测试。读取现有文件并使用结构化补丁修改，运行 node clamp.test.cjs 验证，最后简述结果。"},
	} {
		if selected := os.Getenv("PROOFCODE_LIVE_MODEL_CASES"); selected != "" && !strings.Contains(","+selected+",", ","+scenario.mode+",") {
			continue
		}
		started := time.Now()
		result := outcome{Mode: scenario.mode, Status: "not_started"}
		var task map[string]any
		body := map[string]any{"projectId": projectID, "model": configuration.Model, "providerId": providerID, "executionMode": scenario.mode, "prompt": scenario.prompt, "maxSteps": 12}
		if scenario.mode != "CHAT" {
			body["sourceSnapshotId"] = snapshot["id"]
		}
		if err = request("POST", "/api/tasks", body, &task); err != nil {
			t.Fatal(err)
		}
		id := task["id"].(string)
		deadline := time.Now().Add(4 * time.Minute)
		for time.Now().Before(deadline) {
			if err = request("GET", "/api/tasks/"+id, nil, &task); err != nil {
				t.Fatal(err)
			}
			result.Status, _ = task["status"].(string)
			if result.Status == "WAITING_APPROVAL" {
				var approvals []map[string]any
				if err = request("GET", "/api/tasks/"+id+"/approvals", nil, &approvals); err != nil {
					t.Fatal(err)
				}
				for _, approval := range approvals {
					if approval["status"] == "PENDING" {
						// These are the only write/exec actions authorized for this synthetic fixture.
						tool, _ := approval["tool"].(string)
						allowed := tool == "apply_patch" || tool == "run_command"
						if err = request("POST", "/api/tasks/"+id+"/approvals/"+approval["id"].(string), map[string]any{"approved": allowed}, nil); err != nil {
							t.Fatal(err)
						}
						result.Approvals++
					}
				}
			}
			if result.Status == "SUCCEEDED" || result.Status == "FAILED" || result.Status == "CANCELLED" {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		if result.Status != "SUCCEEDED" && result.Status != "FAILED" {
			_ = request("POST", "/api/tasks/"+id+"/cancel", nil, nil)
		}
		if diagnostic, ok := task["error"].(string); ok {
			diagnostic = strings.ReplaceAll(diagnostic, configuration.APIKey, "[redacted]")
			diagnostic = strings.ReplaceAll(diagnostic, configuration.BaseURL, "[provider]")
			if len(diagnostic) > 1200 {
				diagnostic = diagnostic[:1200]
			}
			result.Failure = diagnostic
		}
		var events []map[string]any
		_ = request("GET", "/api/tasks/"+id+"/events", nil, &events)
		for _, event := range events {
			if event["type"] == "tool.started" {
				result.ToolCalls++
			}
		}
		result.Passed = result.Status == "SUCCEEDED"
		if scenario.mode == "CHAT" {
			answer, _ := task["result"].(string)
			result.Passed = result.Passed && strings.TrimSpace(answer) == "42"
		}
		if scenario.mode == "PLAN" {
			answer, _ := task["result"].(string)
			result.Passed = result.Passed && strings.Contains(strings.ToLower(answer), "clamp")
		}
		if scenario.mode == "CODE" && result.Passed {
			var artifact map[string]any
			if err = request("GET", "/api/tasks/"+id+"/artifacts/checkpoint", nil, &artifact); err != nil {
				t.Fatal(err)
			}
			patch, _ := artifact["patch"].(string)
			if patch == "" || strings.Contains(patch, "diff --git a/clamp.test.cjs") {
				result.Passed = false
			} else {
				capture, e := app.CaptureProjectSource(local.LocalHandle)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = app.ApplyProjectPatch(local.LocalHandle, capture.ManifestHash, patch); e != nil {
					t.Fatal(e)
				}
				command := exec.Command("node", "clamp.test.cjs")
				command.Dir = local.Path
				result.Passed = command.Run() == nil
			}
		}
		result.Milliseconds = time.Since(started).Milliseconds()
		report.Results = append(report.Results, result)
		t.Logf("real model mode=%s status=%s passed=%t approvals=%d elapsed_ms=%d", result.Mode, result.Status, result.Passed, result.Approvals, result.Milliseconds)
		if !result.Passed {
			t.Logf("real model failure: %s", result.Failure)
			t.Errorf("real model %s scenario did not pass objective checks", scenario.mode)
			break
		}
	}
}

type liveEvaluationHTTPError struct{ status int }

func (e *liveEvaluationHTTPError) Error() string {
	return fmt.Sprintf("evaluation API returned HTTP %d", e.status)
}
