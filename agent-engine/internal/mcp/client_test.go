package mcp

import (
	"encoding/json"
	"testing"
)

func TestServerConfigJSON(t *testing.T) {
	var config ServerConfig
	if err := json.Unmarshal([]byte(`{"name":"git","command":"git-mcp","args":["--stdio"]}`), &config); err != nil {
		t.Fatal(err)
	}
	if config.Command != "git-mcp" || len(config.Args) != 1 {
		t.Fatalf("unexpected config: %+v", config)
	}
}
