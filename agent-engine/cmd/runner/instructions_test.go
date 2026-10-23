package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/agent"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

func TestInstructionCheckpointBeforeAckAndResumeWithoutDuplicates(t *testing.T) {
	saved := workspaceMetadata{}
	var disk workspaceMetadata
	persists, acks := 0, 0
	failPersist, failAck := true, false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("runnerId") != "runner" || request.URL.Query().Get("attempt") != "1" {
			t.Error("missing lease scope")
		}
		if strings.HasSuffix(request.URL.Path, "/ack") {
			if disk.InstructionSequence != 1 || len(disk.Messages) != 2 {
				t.Error("ack before checkpoint")
			}
			acks++
			if failAck {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(204)
			return
		}
		values := []taskInstruction{}
		if request.URL.Query().Get("after") == "0" {
			values = append(values, taskInstruction{TaskID: "task", Attempt: 1, Sequence: 1, Content: "只修改测试"})
		}
		_ = json.NewEncoder(w).Encode(values)
	}))
	defer server.Close()
	r := &runner{ID: "runner", ControlPlane: server.URL, HTTP: server.Client()}
	persist := func() error {
		persists++
		if failPersist {
			return errors.New("disk full")
		}
		disk = saved
		return nil
	}
	poll := r.instructionPoller(taskMessage{TaskID: "task", Attempt: 1}, &saved, persist)
	state := agent.RunResult{Messages: []model.Message{{Role: model.RoleUser, Content: "original"}}}
	if _, err := poll(context.Background(), state); err == nil || saved.InstructionSequence != 0 || acks != 0 {
		t.Fatalf("failed persistence advanced cursor: %+v %v", saved, err)
	}
	failPersist = false
	failAck = true
	if _, err := poll(context.Background(), state); err == nil || disk.InstructionSequence != 1 {
		t.Fatal("failed ack lost checkpoint", err)
	}
	failAck = false
	resumed := r.instructionPoller(taskMessage{TaskID: "task", Attempt: 1}, &saved, persist)
	messages, err := resumed(context.Background(), agent.RunResult{Messages: disk.Messages})
	if err != nil || len(messages) != 2 || !reflect.DeepEqual(messages, disk.Messages) || persists != 2 || acks != 2 {
		t.Fatalf("resume duplicated or lost instruction: %v %+v persists=%d acks=%d", err, messages, persists, acks)
	}
}

func TestInstructionRejectsWrongScopeAndGaps(t *testing.T) {
	for _, value := range []taskInstruction{{TaskID: "other", Attempt: 1, Sequence: 1, Content: "x"}, {TaskID: "task", Attempt: 2, Sequence: 1, Content: "x"}, {TaskID: "task", Attempt: 1, Sequence: 2, Content: "x"}} {
		t.Run(value.TaskID+string(rune(value.Attempt+48))+string(rune(value.Sequence+48)), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode([]taskInstruction{value}) }))
			defer server.Close()
			runner := &runner{ID: "runner", ControlPlane: server.URL, HTTP: server.Client()}
			saved := workspaceMetadata{}
			poll := runner.instructionPoller(taskMessage{TaskID: "task", Attempt: 1}, &saved, func() error { t.Fatal("invalid instruction persisted"); return nil })
			if _, err := poll(context.Background(), agent.RunResult{}); err == nil || saved.InstructionSequence != 0 {
				t.Fatal("invalid scope accepted")
			}
		})
	}
}
