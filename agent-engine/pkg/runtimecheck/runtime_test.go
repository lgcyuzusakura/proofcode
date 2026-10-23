package runtimecheck

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDoctorDistinguishesInstalledDockerFromUnavailableEngine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/actuator/health" {
			t.Errorf("health path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"status":"UP"}`)
	}))
	defer server.Close()
	var calls []string
	var callsMu sync.Mutex
	doctor := Doctor{
		BaseURL: server.URL,
		LookPath: func(name string) (string, error) {
			if name == "git" || name == "docker" {
				return "C:/tools/" + name, nil
			}
			return "", errors.New("missing")
		},
		Run: func(_ context.Context, path string, args ...string) ([]byte, error) {
			callsMu.Lock()
			calls = append(calls, path+" "+strings.Join(args, " "))
			callsMu.Unlock()
			if strings.Contains(strings.Join(args, " "), "info") {
				return nil, errors.New("secret daemon diagnostics are never returned")
			}
			return []byte(path + " version"), nil
		},
	}
	report := doctor.Check(context.Background())
	if len(report.Checks) != 5 || report.Checks[0].State != "ready" || report.Checks[1].State != "ready" || report.Checks[2].State != "unavailable" || report.Checks[3].State != "ready" || report.Checks[4].State != "ready" {
		t.Fatalf("unexpected checks: %+v", report.Checks)
	}
	if strings.Contains(report.Checks[2].Detail, "secret") || strings.Contains(report.Checks[4].Detail, "secret") {
		t.Fatal("raw process output escaped into doctor report")
	}
	if len(calls) != 4 {
		t.Fatalf("unexpected subprocesses: %v", calls)
	}
	seenVersion, seenEngine, seenCompose := false, false, false
	for _, call := range calls {
		seenVersion = seenVersion || strings.Contains(call, "--version")
		seenEngine = seenEngine || strings.Contains(call, " info ")
		seenCompose = seenCompose || strings.Contains(call, "compose version")
	}
	if !seenVersion || !seenEngine || !seenCompose {
		t.Fatalf("Docker CLI, engine and Compose checks were not distinguished: %v", calls)
	}
}

func TestDoctorHandlesMissingToolsInvalidHealthURLAndTimeout(t *testing.T) {
	block := make(chan struct{})
	var once sync.Once
	doctor := Doctor{
		BaseURL:  "file:///private/path",
		LookPath: func(string) (string, error) { return "C:/tools/blocked", nil },
		Run: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
			<-ctx.Done()
			once.Do(func() { close(block) })
			return nil, ctx.Err()
		},
	}
	started := time.Now()
	report := doctor.Check(context.Background())
	<-block
	if time.Since(started) > 5*time.Second || report.Checks[0].State != "unavailable" || report.Checks[1].State != "unavailable" || report.Checks[2].State != "blocked" || report.Checks[3].State != "blocked" || report.Checks[4].State != "missing" {
		t.Fatalf("timeout was not bounded and sanitized: elapsed=%s checks=%+v", time.Since(started), report.Checks)
	}
	missing := Doctor{BaseURL: "http://127.0.0.1:1", LookPath: func(string) (string, error) { return "", io.EOF }}.Check(context.Background())
	if missing.Checks[0].State != "missing" || missing.Checks[1].State != "missing" || missing.Checks[2].State != "blocked" || missing.Checks[3].State != "blocked" || missing.Checks[4].State != "unavailable" {
		t.Fatalf("missing runtime tools misreported: %+v", missing.Checks)
	}
}

func TestDoctorDoesNotFollowHealthRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/health", http.StatusFound)
	}))
	defer server.Close()
	doctor := Doctor{BaseURL: server.URL, LookPath: func(string) (string, error) { return "", io.EOF }}
	report := doctor.Check(context.Background())
	if report.Checks[4].State != "unavailable" {
		t.Fatalf("redirect was treated as successful health: %+v", report.Checks[4])
	}
}

func TestDoctorRejectsNonHealthyControlPlaneStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"DOWN"}`))
	}))
	defer server.Close()
	report := (Doctor{BaseURL: server.URL, LookPath: func(string) (string, error) { return "", io.EOF }}).Check(context.Background())
	if report.Checks[4].State != "unavailable" || report.Checks[4].Detail != "后端报告为非健康状态。" {
		t.Fatalf("non-healthy response was accepted: %+v", report.Checks[4])
	}
}

func TestDoctorRejectsInvalidHealthResponses(t *testing.T) {
	for _, body := range []string{"", "<html>login</html>", `{}`, `null`, `{"status":"UP"} trailing`, strings.Repeat("x", 4097)} {
		t.Run(body[:min(len(body), 24)], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
			defer server.Close()
			doctor := Doctor{BaseURL: server.URL, LookPath: func(string) (string, error) { return "", io.EOF }}
			if got := doctor.Check(context.Background()).Checks[4]; got.State != "unavailable" {
				t.Fatalf("invalid health accepted: %+v", got)
			}
		})
	}
}

func TestDoctorHonorsCallerDeadlineAndDoesNotLeakSuccessfulOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	started := time.Now()
	doctor := Doctor{BaseURL: "file:///invalid", LookPath: func(name string) (string, error) { return name, nil }, Run: func(ctx context.Context, _ string, _ ...string) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() }}
	report := doctor.Check(ctx)
	if time.Since(started) > time.Second || report.Checks[0].State != "unavailable" || report.Checks[1].State != "unavailable" {
		t.Fatalf("caller deadline ignored: %+v", report)
	}
	doctor.Run = func(context.Context, string, ...string) ([]byte, error) { return []byte("secret-success-output"), nil }
	for _, check := range doctor.Check(context.Background()).Checks {
		if strings.Contains(check.Detail, "secret") {
			t.Fatalf("successful output leaked: %+v", check)
		}
	}
}

func TestDoctorOverridesRedirectPolicyWithoutMutatingClient(t *testing.T) {
	var targetCalls int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls++
		_, _ = io.WriteString(w, `{"status":"UP"}`)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	client := &http.Client{Timeout: time.Minute}
	doctor := Doctor{BaseURL: server.URL, Client: client, LookPath: func(string) (string, error) { return "", io.EOF }}
	if got := doctor.Check(context.Background()).Checks[4]; got.State != "unavailable" || targetCalls != 0 {
		t.Fatalf("redirect followed: %+v calls=%d", got, targetCalls)
	}
	if client.Timeout != time.Minute || client.CheckRedirect != nil {
		t.Fatal("injected client was mutated")
	}
}
