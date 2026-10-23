package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxOAuthMetadata = 64 << 10

type OAuthMetadata struct {
	Resource              string   `json:"resource"`
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint,omitempty"`
	ScopesSupported       []string `json:"scopes_supported,omitempty"`
	CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
}

type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
}

type OAuthClient struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Scope        string
}

type OAuthAuthorization struct {
	URL          string `json:"url"`
	State        string `json:"state"`
	CodeVerifier string `json:"codeVerifier"`
}

// DiscoverOAuth follows the MCP protected-resource metadata flow and requires
// PKCE S256 support before returning any authorization endpoint to the UI.
func DiscoverOAuth(ctx context.Context, resource string, allowPrivate bool) (OAuthMetadata, error) {
	resourceURL, err := parseOAuthURL(resource, allowPrivate)
	if err != nil {
		return OAuthMetadata{}, err
	}
	client := restrictedHTTPClient(allowPrivate, 12*time.Second)
	defer client.CloseIdleConnections()

	challengeURL := ""
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, resourceURL.String(), nil)
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := client.Do(request)
	if err != nil {
		return OAuthMetadata{}, errors.New("MCP authorization discovery failed")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		return OAuthMetadata{}, errors.New("MCP server did not request OAuth authorization")
	}
	for _, header := range response.Header.Values("WWW-Authenticate") {
		if !strings.Contains(strings.ToLower(header), "bearer") {
			continue
		}
		if match := oauthResourceMetadata.FindStringSubmatch(header); len(match) == 2 {
			challengeURL = strings.ReplaceAll(match[1], `\"`, `"`)
			break
		}
	}
	if challengeURL == "" {
		challengeURL = wellKnown(resourceURL, "oauth-protected-resource")
	}
	if _, err = parseOAuthURL(challengeURL, allowPrivate); err != nil {
		return OAuthMetadata{}, errors.New("MCP protected-resource metadata URL is invalid")
	}
	var protected struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err = getOAuthJSON(ctx, client, challengeURL, &protected); err != nil {
		return OAuthMetadata{}, fmt.Errorf("MCP protected-resource metadata is invalid: %w", err)
	}
	if protected.Resource == "" || len(protected.AuthorizationServers) == 0 || len(protected.AuthorizationServers) > 8 {
		return OAuthMetadata{}, errors.New("MCP protected-resource metadata is invalid")
	}
	if normalizeOAuthIssuer(protected.Resource) != normalizeOAuthIssuer(resourceURL.String()) {
		return OAuthMetadata{}, errors.New("MCP protected-resource metadata does not match the configured server")
	}
	issuerURL, err := parseOAuthURL(protected.AuthorizationServers[0], allowPrivate)
	if err != nil || issuerURL.RawQuery != "" || issuerURL.Fragment != "" {
		return OAuthMetadata{}, errors.New("OAuth issuer URL is invalid")
	}
	metadataURL := wellKnown(issuerURL, "oauth-authorization-server")
	var metadata OAuthMetadata
	if err = getOAuthJSON(ctx, client, metadataURL, &metadata); err != nil {
		return OAuthMetadata{}, errors.New("OAuth server metadata is unavailable")
	}
	if normalizeOAuthIssuer(metadata.Issuer) != normalizeOAuthIssuer(issuerURL.String()) {
		return OAuthMetadata{}, errors.New("OAuth issuer metadata does not match the discovered issuer")
	}
	for _, endpoint := range []string{metadata.AuthorizationEndpoint, metadata.TokenEndpoint} {
		u, parseErr := parseOAuthURL(endpoint, allowPrivate)
		if parseErr != nil || u.RawQuery != "" || u.Fragment != "" {
			return OAuthMetadata{}, errors.New("OAuth endpoint URL is invalid")
		}
	}
	if metadata.RegistrationEndpoint != "" {
		u, parseErr := parseOAuthURL(metadata.RegistrationEndpoint, allowPrivate)
		if parseErr != nil || u.RawQuery != "" || u.Fragment != "" {
			return OAuthMetadata{}, errors.New("OAuth registration endpoint URL is invalid")
		}
	}
	if !contains(metadata.CodeChallengeMethods, "S256") {
		return OAuthMetadata{}, errors.New("OAuth server does not support PKCE S256")
	}
	metadata.Resource = protected.Resource
	metadata.Issuer = issuerURL.String()
	return metadata, nil
}

func BeginOAuth(metadata OAuthMetadata, client OAuthClient) (OAuthAuthorization, error) {
	authorizationEndpoint, err := url.Parse(metadata.AuthorizationEndpoint)
	if err != nil || authorizationEndpoint.Scheme != "https" && authorizationEndpoint.Scheme != "http" || client.ClientID == "" || len(client.ClientID) > 512 || client.RedirectURI == "" {
		return OAuthAuthorization{}, errors.New("OAuth client configuration is invalid")
	}
	redirect, err := url.Parse(client.RedirectURI)
	if err != nil || redirect.Scheme != "https" && !(redirect.Scheme == "http" && (redirect.Hostname() == "localhost" || redirect.Hostname() == "127.0.0.1" || redirect.Hostname() == "::1")) || redirect.Host == "" || redirect.Fragment != "" {
		return OAuthAuthorization{}, errors.New("OAuth redirect URI is invalid")
	}
	state, err := randomOAuthValue(32)
	if err != nil {
		return OAuthAuthorization{}, errors.New("could not create OAuth state")
	}
	verifier, err := randomOAuthValue(32)
	if err != nil {
		return OAuthAuthorization{}, errors.New("could not create OAuth PKCE verifier")
	}
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	query := authorizationEndpoint.Query()
	query.Set("response_type", "code")
	query.Set("client_id", client.ClientID)
	query.Set("redirect_uri", client.RedirectURI)
	if client.Scope != "" {
		query.Set("scope", client.Scope)
	}
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("resource", metadata.Resource)
	authorizationEndpoint.RawQuery = query.Encode()
	return OAuthAuthorization{URL: authorizationEndpoint.String(), State: state, CodeVerifier: verifier}, nil
}

func ExchangeOAuthCode(ctx context.Context, metadata OAuthMetadata, client OAuthClient, code, verifier string, allowPrivate bool) (OAuthTokens, error) {
	if code == "" || len(code) > 4096 || len(verifier) < 43 || len(verifier) > 128 || client.ClientID == "" || client.RedirectURI == "" {
		return OAuthTokens{}, errors.New("OAuth callback is invalid")
	}
	return postOAuthToken(ctx, metadata.TokenEndpoint, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {client.RedirectURI},
		"client_id": {client.ClientID}, "code_verifier": {verifier}, "resource": {metadata.Resource},
	}, client.ClientSecret, allowPrivate)
}

func RefreshOAuthToken(ctx context.Context, metadata OAuthMetadata, client OAuthClient, refreshToken string, allowPrivate bool) (OAuthTokens, error) {
	if client.ClientID == "" || refreshToken == "" || len(refreshToken) > 8192 {
		return OAuthTokens{}, errors.New("OAuth refresh credentials are invalid")
	}
	return postOAuthToken(ctx, metadata.TokenEndpoint, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {client.ClientID}, "resource": {metadata.Resource},
	}, client.ClientSecret, allowPrivate)
}

func postOAuthToken(ctx context.Context, endpoint string, form url.Values, clientSecret string, allowPrivate bool) (OAuthTokens, error) {
	u, err := parseOAuthURL(endpoint, allowPrivate)
	if err != nil || u.RawQuery != "" || u.Fragment != "" {
		return OAuthTokens{}, errors.New("OAuth token endpoint is invalid")
	}
	if len(clientSecret) > 4096 || strings.ContainsAny(clientSecret, "\r\n") {
		return OAuthTokens{}, errors.New("OAuth client configuration is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthTokens{}, errors.New("OAuth token request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if clientSecret != "" {
		request.SetBasicAuth(form.Get("client_id"), clientSecret)
	}
	client := restrictedHTTPClient(allowPrivate, 15*time.Second)
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return OAuthTokens{}, errors.New("OAuth token request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxOAuthMetadata+1))
	if err != nil || len(body) > maxOAuthMetadata || response.StatusCode != http.StatusOK {
		return OAuthTokens{}, errors.New("OAuth token endpoint rejected the request")
	}
	var tokens OAuthTokens
	if json.Unmarshal(body, &tokens) != nil || tokens.AccessToken == "" || len(tokens.AccessToken) > 4096 || strings.IndexFunc(tokens.AccessToken, func(r rune) bool { return r < 33 || r > 126 }) >= 0 || !strings.EqualFold(tokens.TokenType, "Bearer") || len(tokens.RefreshToken) > 8192 || tokens.ExpiresIn < 0 || tokens.ExpiresIn > 31536000 {
		return OAuthTokens{}, errors.New("OAuth token response is invalid")
	}
	return tokens, nil
}

var oauthResourceMetadata = regexp.MustCompile(`(?i)resource_metadata\s*=\s*"((?:[^"\\]|\\.)*)"`)

func getOAuthJSON(ctx context.Context, client *http.Client, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("metadata HTTP status %d", response.StatusCode)
	}
	mediaType := strings.Split(strings.ToLower(response.Header.Get("Content-Type")), ";")[0]
	if mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json") {
		return errors.New("OAuth metadata is not JSON")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxOAuthMetadata+1))
	if err != nil || len(body) > maxOAuthMetadata {
		return errors.New("OAuth metadata is too large")
	}
	return json.Unmarshal(body, target)
}

func parseOAuthURL(raw string, allowPrivate bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid OAuth URL")
	}
	if u.Scheme != "https" && !allowPrivate {
		return nil, errors.New("OAuth requires HTTPS")
	}
	return u, nil
}

func wellKnown(issuer *url.URL, name string) string {
	u := *issuer
	path := strings.TrimSuffix(u.Path, "/")
	u.Path = "/.well-known/" + name + path
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func normalizeOAuthIssuer(raw string) string {
	return strings.TrimSuffix(strings.TrimSpace(raw), "/")
}

func randomOAuthValue(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
