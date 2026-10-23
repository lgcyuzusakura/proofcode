package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalDeliveryValidatesPayloadAndPreservesTaskScope(t *testing.T) {
	valid := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("runner credential missing")
		}
		token := "not-a-token"
		if valid {
			token = "12345678-1234-4234-9234-123456789012"
		}
		fmt.Fprintf(w, `{"id":"12345678-1234-4234-9234-123456789012","token":%q,"task":{"taskId":"12345678-1234-4234-9234-123456789013","attempt":2,"executionMode":"PLAN","workspaceId":"saved-workspace","agentSnapshotHash":"saved-agent"}}`, token)
	}))
	defer server.Close()
	runner := &runner{ControlPlane: server.URL, Token: "fixture-token", HTTP: server.Client()}
	delivery, err := runner.claimLocalDelivery(context.Background())
	if err != nil || delivery.Task.Attempt != 2 || delivery.Task.WorkspaceID != "saved-workspace" || delivery.Task.AgentSnapshotHash != "saved-agent" {
		t.Fatalf("lost task snapshot: %+v %v", delivery, err)
	}
	valid = false
	if _, err = runner.claimLocalDelivery(context.Background()); err == nil {
		t.Fatal("invalid receipt token accepted")
	}
}

func TestLocalDeliveryAcknowledgesOnlyThroughLeaseToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var receipt map[string]string
		if json.NewDecoder(r.Body).Decode(&receipt) != nil || receipt["token"] != "receipt-token" {
			t.Error("receipt fencing missing")
		}
		if r.URL.Path != "/internal/local-deliveries/delivery-id/ack" {
			t.Errorf("bad receipt path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()
	runner := &runner{ControlPlane: server.URL, HTTP: server.Client()}
	if err := runner.localReceipt(context.Background(), localDelivery{ID: "delivery-id", Token: "receipt-token"}, "ack"); err == nil {
		t.Fatal("stale receipt accepted")
	}
}

func TestLocalTransportRejectsNonLoopbackControlPlane(t *testing.T) {
	for _, base := range []string{"http://example.com", "http://127.0.0.1.evil.example", "http://user:secret@127.0.0.1", "https://127.0.0.1", "http://127.0.0.1/path"} {
		if err := (&runner{ControlPlane: base}).runLocal(context.Background()); err == nil {
			t.Fatalf("accepted nonlocal endpoint %s", base)
		}
	}
}
