package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalConfigAuthProfileAndRedaction(t *testing.T) {
	dir := t.TempDir()
	text := `model="base"
model_provider="custom"
profile="work"
[profiles.work]
model="selected"
[model_providers.custom]
base_url="http://127.0.0.1:3000/v1"
wire_api="responses"
requires_openai_auth=true
name="literal # is not a comment"
`
	_ = os.WriteFile(filepath.Join(dir, "config.toml"), []byte(text), 0600)
	_ = os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"OPENAI_API_KEY":"secret-fixture"}`), 0600)
	c, err := LoadDirectory(dir)
	if err != nil || c.Model != "selected" || c.APIKey != "secret-fixture" || c.WireAPI != "responses" {
		t.Fatal("local profile/auth parsing failed", err)
	}
	b, _ := json.Marshal(c.Redacted(nil))
	raw, _ := json.Marshal(c)
	if strings.Contains(string(b), "secret-fixture") || strings.Contains(string(raw), "secret-fixture") {
		t.Fatal("credential serialized")
	}
	_ = os.WriteFile(filepath.Join(dir, "config.toml"), []byte("api_key=\"secret-fixture\" invalid"), 0600)
	c, err = LoadDirectory(dir)
	if err == nil || strings.Contains(err.Error(), "secret-fixture") {
		t.Fatal("unsafe parser error")
	}
}
func TestModelCatalogNoRedirectOrErrorBodyLeak(t *testing.T) {
	leaked := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private" {
			t.Error("missing credential")
		}
		http.Redirect(w, r, target.URL, 302)
	}))
	defer server.Close()
	_, err := (ModelConfig{BaseURL: server.URL, APIKey: "private"}).Models(context.Background())
	if err == nil || leaked || strings.Contains(err.Error(), "private") {
		t.Fatal("redirect credential protection failed")
	}
}
