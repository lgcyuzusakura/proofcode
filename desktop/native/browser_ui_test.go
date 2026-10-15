package main

import (
	"context"
	"encoding/json"
	bridge "github.com/proofcode-dev/proofcode/agent-engine/pkg/browserbridge"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// Opt-in visual check: React uses real native browser methods, but project and
// timeline records are UI fixtures. No provider requests or SQL are executed.
func TestSharedBrowserUI(t *testing.T) {
	if os.Getenv("PROOFCODE_BROWSER_UI_SMOKE") != "1" {
		t.Skip("opt-in Vite/Playwright check")
	}
	t.Setenv("PROOFCODE_BROWSER_HEADLESS", "1")
	t.Setenv("PROOFCODE_BROWSER_BRIDGE_FILE", filepath.Join(t.TempDir(), "bridge.json"))
	app := localTestApp(t)
	defer app.shutdown(context.Background())
	project, _ := app.registerDirectory(t.TempDir())
	id := "11111111-1111-4111-8111-111111111111"
	secret, _ := randomID()
	names := map[string]bool{"ProjectBrowser": true, "BindProjectBrowser": true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/preview" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<title>ProofCode web preview</title><style>body{font:16px system-ui;background:#f6f8fb;margin:0;color:#28394b}.card{background:white;margin:60px auto;padding:32px;max-width:640px;border:1px solid #e5eaf0;border-radius:14px}button{background:#447b9e;color:white;border:0;padding:12px 20px;border-radius:6px}input{padding:12px;border:1px solid #dce2ea;border-radius:6px}h1{font-weight:600}</style><div class="card"><h1>订单管理 · 开发预览</h1><p>同一个页面，AI 验证，人随时接管。</p><input id="field" placeholder="订单名称"><button id="run" onclick="document.getElementById('result').innerText='验证通过：订单列表已刷新'">刷新订单</button><p id="result">等待验证</p></div><script>console.log('preview-ready')</script>`))
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", os.Getenv("PROOFCODE_UI_URL"))
		w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "forbidden", 403)
			return
		}
		var request struct {
			Method string            `json:"method"`
			Args   []json.RawMessage `json:"args"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&request) != nil || !names[request.Method] {
			http.Error(w, "invalid", 400)
			return
		}
		method := reflect.ValueOf(app).MethodByName(request.Method)
		if len(request.Args) != method.Type().NumIn() {
			http.Error(w, "invalid", 400)
			return
		}
		values := []reflect.Value{}
		for i, arg := range request.Args {
			v := reflect.New(method.Type().In(i))
			if json.Unmarshal(arg, v.Interface()) != nil {
				http.Error(w, "invalid", 400)
				return
			}
			values = append(values, v.Elem())
		}
		result := method.Call(values)
		out := map[string]any{}
		last := result[len(result)-1]
		if !last.IsNil() {
			out["error"] = last.Interface().(error).Error()
		} else if len(result) > 1 {
			out["value"] = result[0].Interface()
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer server.Close()
	if err := app.BindProjectBrowser(id, project.LocalHandle); err != nil {
		t.Fatal(err)
	}
	d, err := bridge.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Call(context.Background(), id, bridge.Action{Action: "open", Value: server.URL + "/preview"}); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Call(context.Background(), id, bridge.Action{Action: "click", Selector: "#run"}); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"bridge": server.URL, "token": secret, "projectId": id, "handle": project.LocalHandle})
	script, _ := filepath.Abs(filepath.Join("..", "..", "smoke", "visual", "shared-browser.cjs"))
	cmd := exec.Command("node", script)
	cmd.Env = append(os.Environ(), "PROOFCODE_BROWSER_UI_CONFIG="+string(config))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser UI: %v\n%s", err, out)
	}
	t.Log(string(out))
}
