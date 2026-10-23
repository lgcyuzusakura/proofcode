package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxHTTPBody = 4 << 20

// HTTPClient implements MCP Streamable HTTP POST responses (JSON or SSE).
// It does not open a background GET stream or implement legacy SSE/OAuth.
type HTTPClient struct {
	endpoint, bearer, headerName, headerValue, session, protocol string
	client                                                       *http.Client
	ctx                                                          context.Context
	cancel                                                       context.CancelFunc
	next                                                         atomic.Int64
	closed                                                       atomic.Bool
	once                                                         sync.Once
	credentialMu                                                 sync.Mutex
	expiresAt                                                    int64
	refresh                                                      func(context.Context) (string, int64, error)
	credentials                                                  []string
}

func StartHTTP(ctx context.Context, config ServerConfig) (*HTTPClient, error) {
	u, err := url.Parse(config.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.Contains(u.Path, "..") || strings.EqualFold(u.Hostname(), "metadata.google.internal") {
		return nil, errors.New("invalid MCP HTTP endpoint")
	}
	if config.Command != "" || len(config.Args) != 0 || len(config.Env) != 0 || len(config.Bearer) > 4096 || strings.ContainsAny(config.Bearer, "\r\n") || !validCredentialHeader(config.HeaderName, config.HeaderValue) {
		return nil, errors.New("invalid MCP HTTP configuration")
	}
	lifecycle, cancel := context.WithCancel(ctx)
	c := &HTTPClient{endpoint: u.String(), bearer: config.Bearer, headerName: config.HeaderName, headerValue: config.HeaderValue, ctx: lifecycle, cancel: cancel, client: restrictedHTTPClient(config.AllowPrivateHosts, 60*time.Second), expiresAt: config.OAuthExpiresAt, refresh: config.OAuthRefresh, credentials: []string{config.Bearer, config.HeaderValue}}
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools *json.RawMessage `json:"tools"`
		} `json:"capabilities"`
	}
	if err := c.call(ctx, "initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "ProofCode", "version": "0.4.0"}}, &initialized); err != nil {
		_ = c.Close()
		return nil, err
	}
	switch initialized.ProtocolVersion {
	case "2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25":
		c.protocol = initialized.ProtocolVersion
	default:
		_ = c.Close()
		return nil, errors.New("unsupported MCP protocol version")
	}
	if initialized.Capabilities.Tools == nil {
		_ = c.Close()
		return nil, errors.New("MCP server does not advertise tools")
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// Both MCP requests and OAuth metadata/token exchanges use the same checked-IP
// dialer. Redirects and environment proxy settings cannot bypass the policy.
func restrictedHTTPClient(allowPrivate bool, timeout time.Duration) *http.Client {
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxIdleConnsPerHost: 2}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid MCP address")
		}
		if strings.EqualFold(host, "metadata.google.internal") {
			return nil, &transportError{code: "private_host"}
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, &transportError{code: "dns"}
		}
		for _, ip := range ips {
			if !allowPrivate && !publicIP(ip) {
				return nil, &transportError{code: "private_host"}
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func validCredentialHeader(name, value string) bool {
	if name == "" {
		return value == ""
	}
	if len(name) > 128 || len(value) > 4096 || strings.IndexFunc(value, func(r rune) bool { return r < 32 || r > 126 }) >= 0 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return false
		}
	}
	lower := strings.ToLower(name)
	switch lower {
	case "host", "content-length", "connection", "transfer-encoding", "upgrade", "proxy-authorization", "authorization", "content-type", "accept", "accept-encoding", "user-agent", "expect", "trailer", "te", "cookie", "origin", "referer", "cache-control", "pragma":
		return false
	}
	if strings.HasPrefix(lower, "mcp-") || strings.HasPrefix(lower, "proxy-") || strings.HasPrefix(lower, "sec-") {
		return false
	}
	return value != ""
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "198.18.0.0/15", "240.0.0.0/4", "2001:db8::/32"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}

func (c *HTTPClient) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	definitions, err := listTools(ctx, c.call)
	if err != nil {
		return nil, err
	}
	for i := range definitions {
		if c.containsCredential(definitions[i].Name) {
			return nil, errors.New("MCP tool name contains credential data")
		}
		definitions[i].Description = c.redactText(definitions[i].Description)
		definitions[i].InputSchema = c.redactValue(definitions[i].InputSchema).(map[string]any)
	}
	return definitions, nil
}
func (c *HTTPClient) CallTool(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &result)
	if err != nil {
		return nil, err
	}
	var value any
	if json.Unmarshal(result, &value) != nil {
		return nil, errors.New("invalid MCP result")
	}
	redacted, err := json.Marshal(c.redactValue(value))
	if err != nil {
		return nil, errors.New("invalid MCP result")
	}
	return redacted, nil
}

func (c *HTTPClient) containsCredential(value string) bool {
	c.credentialMu.Lock()
	defer c.credentialMu.Unlock()
	for _, secret := range c.credentials {
		if secret != "" && strings.Contains(value, secret) {
			return true
		}
	}
	return false
}

func (c *HTTPClient) redactText(value string) string {
	c.credentialMu.Lock()
	defer c.credentialMu.Unlock()
	for _, secret := range c.credentials {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return value
}

func (c *HTTPClient) redactValue(value any) any {
	switch item := value.(type) {
	case string:
		return c.redactText(item)
	case map[string]any:
		clean := make(map[string]any, len(item))
		for key, child := range item {
			clean[c.redactText(key)] = c.redactValue(child)
		}
		return clean
	case []any:
		clean := make([]any, len(item))
		for i, child := range item {
			clean[i] = c.redactValue(child)
		}
		return clean
	default:
		return value
	}
}

func (c *HTTPClient) request(ctx context.Context, method string, payload []byte) (*http.Response, error) {
	if c.closed.Load() {
		return nil, errors.New("MCP client closed")
	}
	if c.refresh != nil {
		c.credentialMu.Lock()
		if c.expiresAt > 0 && c.expiresAt <= time.Now().Add(30*time.Second).Unix() {
			access, expires, err := c.refresh(ctx)
			if err != nil {
				c.credentialMu.Unlock()
				return nil, &transportError{code: "authentication"}
			}
			if access == "" || len(access) > 4096 || strings.IndexFunc(access, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
				c.credentialMu.Unlock()
				return nil, &transportError{code: "authentication"}
			}
			if c.bearer != access {
				c.credentials = append(c.credentials, access)
			}
			c.bearer, c.expiresAt = access, expires
		}
		bearer := c.bearer
		c.credentialMu.Unlock()
		return c.sendRequest(ctx, method, payload, bearer)
	}
	return c.sendRequest(ctx, method, payload, c.bearer)
}

func (c *HTTPClient) sendRequest(ctx context.Context, method string, payload []byte, bearer string) (*http.Response, error) {
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	req, err := http.NewRequestWithContext(callCtx, method, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		stop()
		cancel()
		return nil, errors.New("invalid MCP request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if c.headerName != "" {
		req.Header.Set(c.headerName, c.headerValue)
	}
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	if c.protocol != "" {
		req.Header.Set("MCP-Protocol-Version", c.protocol)
	}
	response, err := c.client.Do(req)
	if err != nil {
		stop()
		cancel()
		var classified *transportError
		if errors.As(err, &classified) {
			return nil, classified
		}
		var timeout net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
			return nil, &transportError{code: "timeout"}
		}
		return nil, &transportError{code: "network"}
	}
	response.Body = &cancelBody{ReadCloser: response.Body, done: func() { stop(); cancel() }}
	return response, nil
}

type cancelBody struct {
	io.ReadCloser
	done func()
}

func (b *cancelBody) Close() error { defer b.done(); return b.ReadCloser.Close() }

func (c *HTTPClient) notify(ctx context.Context, method string, params any) error {
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	response, err := c.request(ctx, http.MethodPost, payload)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return &httpStatusError{status: response.StatusCode}
	}
	return nil
}

func (c *HTTPClient) call(ctx context.Context, method string, params any, target any) error {
	id := c.next.Add(1)
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return errors.New("invalid MCP parameters")
	}
	response, err := c.request(ctx, http.MethodPost, payload)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &httpStatusError{status: response.StatusCode}
	}
	if method == "initialize" {
		session := response.Header.Get("Mcp-Session-Id")
		if len(session) > 1024 || strings.IndexFunc(session, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 {
			return errors.New("invalid MCP session")
		}
		c.session = session
	}
	media, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	reader := io.LimitReader(response.Body, maxHTTPBody+1)
	var raw []byte
	if media == "text/event-stream" {
		raw, err = readHTTPEvent(reader, id)
	} else if media == "application/json" {
		raw, err = io.ReadAll(reader)
	} else {
		return errors.New("unsupported MCP response type")
	}
	if err != nil || len(raw) > maxHTTPBody {
		return errors.New("invalid or oversized MCP response")
	}
	var value struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &value) != nil || value.JSONRPC != "2.0" || string(value.ID) != strconv.FormatInt(id, 10) {
		return errors.New("MCP response does not match request")
	}
	if value.Error != nil {
		return fmt.Errorf("MCP RPC error %d", value.Error.Code)
	}
	if len(value.Result) == 0 || string(value.Result) == "null" {
		return errors.New("missing MCP result")
	}
	if json.Unmarshal(value.Result, target) != nil {
		return errors.New("invalid MCP result")
	}
	return nil
}

func readHTTPEvent(reader io.Reader, id int64) ([]byte, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxHTTPBody)
	var data []string
	total := 0
	for scanner.Scan() {
		line := scanner.Text()
		total += len(line) + 1
		if total > maxHTTPBody {
			return nil, errors.New("MCP event stream too large")
		}
		if line == "" {
			if len(data) == 0 {
				continue
			}
			raw := []byte(strings.Join(data, "\n"))
			data = nil
			var item struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(raw, &item) != nil {
				return nil, errors.New("invalid MCP event")
			}
			if string(item.ID) == strconv.FormatInt(id, 10) && item.Method == "" {
				return raw, nil
			}
			if len(item.ID) > 0 {
				return nil, errors.New("unexpected MCP server request or response")
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	return nil, errors.New("MCP event stream ended before response")
}

func (c *HTTPClient) Close() error {
	c.once.Do(func() {
		if c.session != "" && c.ctx.Err() == nil {
			ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
			response, err := c.request(ctx, http.MethodDelete, nil)
			if err == nil {
				_ = response.Body.Close()
			}
			stop()
		}
		c.closed.Store(true)
		c.cancel()
		c.client.CloseIdleConnections()
	})
	return nil
}
