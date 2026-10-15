package main

import (
	"context"
	"encoding/json"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContextSnapshotHeaderIsRequiredAndMatchesTask(t *testing.T) {
	for _, header := range []string{"", "different", "selection-1"} {
		t.Run(header, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if !strings.Contains(request.URL.String(), "/conversation?attempt=1") {
					t.Errorf("unexpected history path: %s", request.URL)
				}
				w.Header().Set("X-Context-Selection-Hash", header)
				json.NewEncoder(w).Encode([]map[string]any{{"id": "kept", "role": "user", "content": "Only approved project constraints", "sourceConversationId": "source"}})
			}))
			defer server.Close()
			runner := &runner{ControlPlane: server.URL, HTTP: server.Client()}
			task := scopedLocalTask()
			task.ContextSelectionHash = "selection-1"
			messages, err := runner.conversationHistory(context.Background(), task)
			if header != "selection-1" {
				if err == nil {
					t.Fatal("mismatched projection accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) != 1 || messages[0].Role != model.RoleUser || messages[0].Content != "Only approved project constraints" {
				t.Fatalf("unexpected projection: %+v", messages)
			}
		})
	}
}

func TestContextResumeRejectsOldOrDifferentSnapshot(t *testing.T) {
	for _, pair := range [][2]string{{"selected", ""}, {"selected", "other"}, {"", "old"}} {
		if validateContextResume(pair[0], pair[1]) == nil {
			t.Fatalf("unsafe resume accepted: %v", pair)
		}
	}
	for _, pair := range [][2]string{{"selected", "selected"}, {"", ""}} {
		if err := validateContextResume(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
}
