package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOAuthDiscoveryPKCEExchangeAndRefresh(t *testing.T) {
	var resourceURL string
	var seenResource string
	var tokenCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+serverURL(r)+`/.well-known/oauth-protected-resource"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/.well-known/oauth-protected-resource":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": resourceURL, "authorization_servers": []string{serverURL(r)}, "scopes_supported": []string{"docs.read"}})
		case "/.well-known/oauth-authorization-server":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": serverURL(r), "authorization_endpoint": serverURL(r) + "/authorize", "token_endpoint": serverURL(r) + "/token", "registration_endpoint": serverURL(r) + "/register", "code_challenge_methods_supported": []string{"S256"}})
		case "/token":
			tokenCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("client:client-secret")) {
				t.Errorf("client authentication was not sent using HTTP Basic")
			}
			seenResource = r.Form.Get("resource")
			if r.Form.Get("grant_type") == "authorization_code" && (r.Form.Get("code") != "one-time-code" || r.Form.Get("code_verifier") == "") {
				t.Errorf("authorization code exchange omitted PKCE: %v", r.Form)
			}
			w.Header().Set("Content-Type", "application/json")
			if tokenCalls == 1 {
				io.WriteString(w, `{"access_token":"access-one","token_type":"Bearer","refresh_token":"refresh-one","expires_in":300}`)
			} else {
				io.WriteString(w, `{"access_token":"access-two","token_type":"Bearer","refresh_token":"refresh-two","expires_in":600}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	resourceURL = server.URL + "/mcp"
	metadata, err := DiscoverOAuth(context.Background(), resourceURL, true)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Resource != resourceURL || metadata.Issuer != server.URL || metadata.TokenEndpoint != server.URL+"/token" {
		t.Fatalf("discovery returned the wrong metadata: %+v", metadata)
	}
	client := OAuthClient{ClientID: "client", ClientSecret: "client-secret", RedirectURI: "http://localhost:3000/api/mcp/oauth/callback", Scope: "docs.read"}
	authorization, err := BeginOAuth(metadata, client)
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := url.Parse(authorization.URL)
	if err != nil || authURL.Query().Get("state") != authorization.State || authURL.Query().Get("code_challenge_method") != "S256" || authURL.Query().Get("resource") != resourceURL {
		t.Fatalf("authorization URL is missing state, PKCE, or resource binding: %q", authorization.URL)
	}
	hash := sha256.Sum256([]byte(authorization.CodeVerifier))
	if authURL.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(hash[:]) {
		t.Fatal("code challenge is not the S256 hash of the persisted verifier")
	}
	tokens, err := ExchangeOAuthCode(context.Background(), metadata, client, "one-time-code", authorization.CodeVerifier, true)
	if err != nil || tokens.AccessToken != "access-one" || tokens.RefreshToken != "refresh-one" || seenResource != resourceURL {
		t.Fatalf("code exchange failed: %+v %v", tokens, err)
	}
	refreshed, err := RefreshOAuthToken(context.Background(), metadata, client, tokens.RefreshToken, true)
	if err != nil || refreshed.AccessToken != "access-two" || refreshed.RefreshToken != "refresh-two" || tokenCalls != 2 {
		t.Fatalf("token refresh failed: %+v %v", refreshed, err)
	}
}

func TestOAuthRejectsIssuerMismatchUnsupportedPKCEAndRedirects(t *testing.T) {
	var resource string
	var redirects int
	var redirectedCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/mcp":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+serverURL(r)+`/.well-known/oauth-protected-resource"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/.well-known/oauth-protected-resource":
			json.NewEncoder(w).Encode(map[string]any{"resource": resource, "authorization_servers": []string{serverURL(r)}})
		case "/.well-known/oauth-authorization-server":
			json.NewEncoder(w).Encode(map[string]any{"issuer": serverURL(r), "authorization_endpoint": serverURL(r) + "/authorize", "token_endpoint": serverURL(r) + "/redirect", "code_challenge_methods_supported": []string{"plain"}})
		case "/redirect":
			redirects++
			http.Redirect(w, r, serverURL(r)+"/token", http.StatusFound)
		case "/token":
			redirectedCalls++
			io.WriteString(w, `{"access_token":"should-not-be-returned","token_type":"Bearer"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	resource = server.URL + "/mcp"
	if _, err := DiscoverOAuth(context.Background(), resource, true); err == nil || !strings.Contains(err.Error(), "S256") {
		t.Fatalf("discovery should reject servers without PKCE S256, got %v", err)
	}
	meta := OAuthMetadata{Resource: resource, Issuer: server.URL, AuthorizationEndpoint: server.URL + "/authorize", TokenEndpoint: server.URL + "/redirect", CodeChallengeMethods: []string{"S256"}}
	_, err := ExchangeOAuthCode(context.Background(), meta, OAuthClient{ClientID: "client", RedirectURI: "http://localhost/callback"}, "code", strings.Repeat("v", 43), true)
	if err == nil || redirects != 1 || redirectedCalls != 0 {
		t.Fatalf("token endpoint redirects must not be followed: error=%v redirects=%d", err, redirects)
	}
	if _, err := DiscoverOAuth(context.Background(), "http://127.0.0.1:80/mcp", false); err == nil {
		t.Fatal("non-HTTPS OAuth endpoint should be rejected by default")
	}
}

func serverURL(r *http.Request) string { return "http://" + r.Host }
