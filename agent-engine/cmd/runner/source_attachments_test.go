package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/event"
)

func TestResolveSourceAttachmentsRequiresRunnerSnapshotAndEnforcesWireLimit(t *testing.T) {
	body := "<proofcode-source>verified source</proofcode-source>"
	hash := hashString(body)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("attempt") != "2" || request.URL.Query().Get("runnerId") != "runner-1" {
			t.Errorf("missing lease identity: %s", request.URL.RawQuery)
		}
		w.Header().Set("X-Source-Attachment-Hash", hash)
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()
	r := &runner{ID: "runner-1", ControlPlane: server.URL, HTTP: server.Client()}
	task := taskMessage{TaskID: "task", Attempt: 2, SourceAttachmentHash: hash}
	if err := r.resolveSourceAttachments(context.Background(), &task); err != nil || task.sourceAttachments != body {
		t.Fatalf("snapshot not resolved: content=%q err=%v", task.sourceAttachments, err)
	}

	large := strings.Repeat("x", (64<<10)+1)
	largeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("X-Source-Attachment-Hash", hashString(large))
		_, _ = fmt.Fprint(w, large)
	}))
	defer largeServer.Close()
	r.ControlPlane = largeServer.URL
	task.SourceAttachmentHash = hashString(large)
	if err := r.resolveSourceAttachments(context.Background(), &task); err == nil {
		t.Fatal("oversized source attachment snapshot accepted")
	}
}

func TestChatInjectsSourceAttachmentExactlyOnce(t *testing.T) {
	attachment := "<proofcode-source path=\"main.go\">package main</proofcode-source>"
	var contents string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/chat/completions" {
			http.NotFound(w, request)
			return
		}
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		for _, message := range body.Messages {
			contents += message.Content
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	r := &runner{ModelBaseURL: server.URL, ModelAPIKey: "fixture", ModelHTTP: server.Client()}
	if err := r.runChat(context.Background(), taskMessage{TaskID: "task", Model: "fixture", Prompt: "question", sourceAttachments: attachment}, event.NewSequencedSink(&event.MemorySink{})); err != nil {
		t.Fatal(err)
	}
	if strings.Count(contents, attachment) != 1 {
		t.Fatalf("attachment injected %d times: %s", strings.Count(contents, attachment), contents)
	}
}
