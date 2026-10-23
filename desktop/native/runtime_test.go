package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestCheckRuntimeUsesConfiguredHealthEndpoint(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/actuator/health" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected health request: %s %s", r.Method, r.URL)
		}
		_, _ = io.WriteString(w, `{"status":"UP"}`)
	}))
	defer server.Close()
	t.Setenv("PROOFCODE_CONTROL_PLANE_URL", server.URL)
	t.Setenv("PATH", t.TempDir())
	app := NewApp()
	app.startup(context.Background())
	report := app.CheckRuntime()
	if len(report.Checks) != 5 || report.Checks[0].State != "missing" || report.Checks[4].State != "ready" || calls != 1 || report.CheckedAt.IsZero() {
		t.Fatalf("unexpected desktop report: %+v calls=%d", report, calls)
	}
}

func TestRuntimeManagementUIEndToEnd(t *testing.T) {
	if os.Getenv("PROOFCODE_RUNTIME_UI_SMOKE") != "1" {
		t.Skip("opt-in: existing UI server, Edge and Playwright required")
	}
	app, _, _ := runtimeFixture(t)
	defer app.closeRuntimeOperations()
	release := make(chan struct{}, 1)
	var running atomic.Bool
	app.runtimeRunner = func(ctx context.Context, _ string, args ...string) (string, error) {
		switch args[0] {
		case "config":
			return "artemis\ncontrol-plane\npostgres\nredis\nrunner\nweb", nil
		case "up":
			select {
			case <-release:
				running.Store(true)
				return "", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		case "stop":
			running.Store(false)
			return "", nil
		case "ps":
			rows := []map[string]string{}
			for _, name := range proofCodeComposeServices {
				state := "exited"
				if running.Load() {
					state = "running"
				}
				rows = append(rows, map[string]string{"Service": name, "State": state})
			}
			data, _ := json.Marshal(rows)
			return string(data), nil
		default:
			t.Errorf("unexpected fixture Compose request: %v", args)
			return "", nil
		}
	}
	secret, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := os.Getenv("PROOFCODE_UI_URL")
		if origin == "" {
			origin = "http://127.0.0.1:5187"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Headers", "Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var state RuntimeManagement
		var err error
		switch r.URL.Path {
		case "/get":
			state, err = app.GetRuntimeManagement()
		case "/check":
			state, err = app.RefreshRuntimeManagement()
		case "/start":
			state, err = app.ManageRuntime("start")
		case "/stop":
			state, err = app.ManageRuntime("stop")
		case "/release":
			select {
			case release <- struct{}{}:
			default:
			}
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "binding failed", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	}))
	defer server.Close()
	config, _ := json.Marshal(map[string]string{"url": server.URL, "token": secret})
	script, err := filepath.Abs(filepath.Join("..", "..", "frontend", "tests", "runtime-management-native.browser.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", script)
	command.Dir = filepath.Join("..", "..")
	command.Env = append(os.Environ(), "PROOFCODE_RUNTIME_MANAGEMENT_NATIVE="+string(config))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("runtime management UI: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func TestCheckRuntimeHonorsDesktopShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	t.Setenv("PROOFCODE_CONTROL_PLANE_URL", "http://127.0.0.1:1")
	t.Setenv("PATH", t.TempDir())
	app := NewApp()
	app.startup(ctx)
	if report := app.CheckRuntime(); len(report.Checks) != 5 || report.Checks[4].State != "unavailable" {
		t.Fatalf("unexpected canceled report: %+v", report)
	}
}

func TestRuntimeEnvironmentUIEndToEnd(t *testing.T) {
	if os.Getenv("PROOFCODE_RUNTIME_UI_SMOKE") != "1" {
		t.Skip("opt-in: existing UI server, Edge and Playwright required")
	}
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"status":"UP"}`) }))
	defer health.Close()
	t.Setenv("PROOFCODE_CONTROL_PLANE_URL", health.URL)
	app := NewApp()
	app.startup(context.Background())
	secret, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := os.Getenv("PROOFCODE_UI_URL")
		if origin == "" {
			origin = "http://127.0.0.1:5187"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Headers", "Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/check" || r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(app.CheckRuntime())
	}))
	defer server.Close()
	config, _ := json.Marshal(map[string]string{"url": server.URL + "/check", "token": secret})
	script, err := filepath.Abs(filepath.Join("..", "..", "frontend", "tests", "runtime-environment.browser.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", script)
	command.Dir = filepath.Join("..", "..")
	command.Env = append(os.Environ(), "PROOFCODE_RUNTIME_NATIVE="+string(config))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("runtime UI: %v\n%s", err, output)
	}
	t.Log(string(output))
}
