package main

import (
	"context"
	"encoding/json"
	bridge "github.com/proofcode-dev/proofcode/agent-engine/pkg/browserbridge"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedBrowserAgentHumanSameTargetAndTakeover(t *testing.T) {
	t.Setenv("PROOFCODE_BROWSER_HEADLESS", "1")
	t.Setenv("PROOFCODE_BROWSER_BRIDGE_FILE", filepath.Join(t.TempDir(), "bridge.json"))
	if browserExecutable() == "" {
		t.Skip("Chromium required")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<title>Shared browser</title><button id="run" onclick="document.getElementById('result').innerText='clicked'">Run</button><p id="result"></p><input id="field" oninput="document.getElementById('result').innerText=this.value">`))
	}))
	defer server.Close()
	app := localTestApp(t)
	defer app.shutdown(context.Background())
	p, _ := app.registerDirectory(t.TempDir())
	id := "11111111-1111-4111-8111-111111111111"
	other := "22222222-2222-4222-8222-222222222222"
	if err := app.BindProjectBrowser(id, p.LocalHandle); err != nil {
		t.Fatal(err)
	}
	d, err := bridge.Load()
	if err != nil {
		t.Fatal(err)
	}
	agent, err := d.Call(context.Background(), id, bridge.Action{Action: "open", Value: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Call(context.Background(), id, bridge.Action{Action: "fill", Selector: "#field", Value: "filled value"}); err != nil {
		t.Fatal(err)
	}
	human, err := app.ProjectBrowser(p.LocalHandle, "inspect", "", 0, 0)
	if err != nil || human.TargetID != agent.TargetID || human.Title != "Shared browser" {
		t.Fatal("did not share actual tab", err)
	}
	if _, err = d.Call(context.Background(), other, bridge.Action{Action: "snapshot"}); err == nil {
		t.Fatal("cross project access")
	}
	if _, err = d.Call(context.Background(), id, bridge.Action{Action: "click", Selector: "#run"}); err != nil {
		t.Fatal(err)
	}
	human, err = app.ProjectBrowser(p.LocalHandle, "takeover", "", 0, 0)
	if err != nil || !strings.Contains(human.Text, "clicked") {
		t.Fatal("agent action not visible to human")
	}
	if _, err = d.Call(context.Background(), id, bridge.Action{Action: "click", Selector: "#run"}); err == nil {
		t.Fatal("agent wrote during takeover")
	}
	if _, err = app.ProjectBrowser(p.LocalHandle, "release", "", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Call(context.Background(), id, bridge.Action{Action: "click", Selector: "#run"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(bridge.Action{Action: "snapshot"})
	req, _ := http.NewRequest("POST", d.URL+"/projects/"+id+"/browser", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+d.Token)
	req.Header.Set("Origin", "https://malicious.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("website could call local browser")
	}
}
