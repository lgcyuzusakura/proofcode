package decision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChooseSendsSystemOneChoiceAndValidatesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		var request struct {
			State     string `json:"state"`
			Model     string `json:"model"`
			Questions map[string]struct {
				Type     string            `json:"type"`
				Criteria map[string]string `json:"criteria"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.State != "inspect the repository" || request.Model != "jev-latest" {
			t.Fatalf("request state/model = %q/%q", request.State, request.Model)
		}
		question, ok := request.Questions[questionID]
		if !ok || question.Type != "choice" {
			t.Fatalf("question = %#v", request.Questions)
		}
		want := map[string]string{"read_file": "Inspect a source file", "run_tests": "Run tests", DeferChoice: "Defer the decision to the normal planner."}
		if len(question.Criteria) != len(want) {
			t.Fatalf("criteria = %#v", question.Criteria)
		}
		for name, description := range want {
			if question.Criteria[name] != description {
				t.Errorf("criteria[%q] = %q, want %q", name, question.Criteria[name], description)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"next_tool":{"type":"choice","choice":"read_file","confidence":0.8917,"probabilities":{"read_file":0.9278,"run_tests":0.016,"defer":0.0562}}},"latency_ms":360.2}`))
	}))
	defer server.Close()

	result, err := (Client{BaseURL: server.URL, APIKey: "test-key", Model: "jev-latest"}).Choose(context.Background(), "inspect the repository", []Option{
		{Name: "read_file", Description: "Inspect a source file"},
		{Name: "run_tests", Description: "Run tests"},
	})
	if err != nil {
		t.Fatalf("Choose() error = %v", err)
	}
	if result.Choice != "read_file" || result.Confidence != 0.8917 || result.Model != "jev-latest" || result.LatencyMS != 360.2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Probabilities["defer"] != 0.0562 {
		t.Fatalf("probabilities = %#v", result.Probabilities)
	}
}

func TestChooseAcceptsV1AndFullEndpointsAndKeepsProvidedDefer(t *testing.T) {
	paths := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		_, _ = w.Write([]byte(`{"answers":{"next_tool":{"type":"choice","choice":"defer","confidence":1,"probabilities":{"defer":1}}}}`))
	}))
	defer server.Close()
	for _, base := range []string{server.URL + "/v1", server.URL + "/v1/systemone"} {
		result, err := (Client{BaseURL: base}).Choose(context.Background(), "state", []Option{{Name: "defer", Description: "Wait"}})
		if err != nil {
			t.Fatalf("Choose(%q) error = %v", base, err)
		}
		if result.Choice != "defer" {
			t.Fatalf("choice = %q", result.Choice)
		}
	}
	close(paths)
	for path := range paths {
		if path != "/v1/systemone" {
			t.Errorf("path = %q", path)
		}
	}
}

func TestChooseRejectsMalformedDecision(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "unknown choice", body: `{"answers":{"next_tool":{"type":"choice","choice":"delete","confidence":1,"probabilities":{"read_file":0.5,"defer":0.5}}}}`, want: "unknown choice"},
		{name: "missing probability", body: `{"answers":{"next_tool":{"type":"choice","choice":"read_file","confidence":1,"probabilities":{"read_file":1}}}}`, want: "probabilities for 2 choices"},
		{name: "bad sum", body: `{"answers":{"next_tool":{"type":"choice","choice":"read_file","confidence":1,"probabilities":{"read_file":0.1,"defer":0.1}}}}`, want: "sum"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			_, err := (Client{BaseURL: server.URL}).Choose(context.Background(), "state", []Option{{Name: "read_file"}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestChooseHonorsTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	started := time.Now()
	_, err := (Client{BaseURL: server.URL, Timeout: 15 * time.Millisecond}).Choose(context.Background(), "state", []Option{{Name: "read_file"}})
	if err == nil || !strings.Contains(err.Error(), "SystemOne request") {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

func TestChooseReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"detail":"model unavailable"}`))
	}))
	defer server.Close()
	_, err := (Client{BaseURL: server.URL}).Choose(context.Background(), "state", []Option{{Name: "read_file"}})
	if err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestChooseRejectsInvalidOptions(t *testing.T) {
	for _, options := range [][]Option{{{Name: " "}}, {{Name: "read_file"}, {Name: "read_file"}}} {
		if _, err := (Client{}).Choose(context.Background(), "state", options); err == nil {
			t.Fatalf("options %#v unexpectedly accepted", options)
		}
	}
}
