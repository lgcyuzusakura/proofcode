package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/mcp"
)

// This fixed-purpose helper accepts credentials only on stdin, never argv.
func main() {
	run(os.Stdin, os.Stdout)
}

func run(stdin io.Reader, stdout io.Writer) {
	var input struct {
		Operation         string            `json:"operation"`
		Name              string            `json:"name"`
		URL               string            `json:"url"`
		Bearer            string            `json:"bearer"`
		HeaderName        string            `json:"headerName"`
		HeaderValue       string            `json:"headerValue"`
		AllowPrivateHosts bool              `json:"allowPrivateHosts"`
		Metadata          mcp.OAuthMetadata `json:"metadata"`
		ClientID          string            `json:"clientId"`
		ClientSecret      string            `json:"clientSecret"`
		RedirectURI       string            `json:"redirectUri"`
		Scope             string            `json:"scope"`
		Code              string            `json:"code"`
		CodeVerifier      string            `json:"codeVerifier"`
		RefreshToken      string            `json:"refreshToken"`
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, (32<<10)+1))
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err != nil || len(raw) > 32<<10 || decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		_ = json.NewEncoder(stdout).Encode(mcp.Inspection{Code: "configuration", Stage: "initialize", CheckedAt: time.Now().UTC(), Tools: []mcp.ToolPreview{}})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.OAuthClient{ClientID: input.ClientID, ClientSecret: input.ClientSecret, RedirectURI: input.RedirectURI, Scope: input.Scope}
	switch input.Operation {
	case "oauth-discover":
		result, err := mcp.DiscoverOAuth(ctx, input.URL, input.AllowPrivateHosts)
		writeResult(stdout, result, err)
	case "oauth-begin":
		result, err := mcp.BeginOAuth(input.Metadata, client)
		writeResult(stdout, result, err)
	case "oauth-exchange":
		result, err := mcp.ExchangeOAuthCode(ctx, input.Metadata, client, input.Code, input.CodeVerifier, input.AllowPrivateHosts)
		writeResult(stdout, result, err)
	case "oauth-refresh":
		result, err := mcp.RefreshOAuthToken(ctx, input.Metadata, client, input.RefreshToken, input.AllowPrivateHosts)
		writeResult(stdout, result, err)
	case "":
		report := mcp.InspectHTTP(ctx, mcp.ServerConfig{Name: input.Name, URL: input.URL, Bearer: input.Bearer, HeaderName: input.HeaderName, HeaderValue: input.HeaderValue, AllowPrivateHosts: input.AllowPrivateHosts})
		_ = json.NewEncoder(stdout).Encode(report)
	default:
		_ = json.NewEncoder(stdout).Encode(map[string]string{"error": "OAuth operation failed"})
	}
	return
}

func writeResult[T any](stdout io.Writer, result T, err error) {
	if err != nil {
		_ = json.NewEncoder(stdout).Encode(map[string]string{"error": "OAuth operation failed"})
		return
	}
	_ = json.NewEncoder(stdout).Encode(result)
}
