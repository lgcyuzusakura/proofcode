package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sync"
	"sync/atomic"
)

type ServerConfig struct {
	Name              string                                       `json:"name"`
	Command           string                                       `json:"command"`
	Args              []string                                     `json:"args"`
	Env               map[string]string                            `json:"env,omitempty"`
	Directory         string                                       `json:"-"`
	URL               string                                       `json:"url,omitempty"`
	Bearer            string                                       `json:"bearer,omitempty"`
	OAuthGrantID      string                                       `json:"oauthGrantId,omitempty"`
	OAuthExpiresAt    int64                                        `json:"oauthExpiresAt,omitempty"`
	OAuthRefresh      func(context.Context) (string, int64, error) `json:"-"`
	HeaderName        string                                       `json:"headerName,omitempty"`
	HeaderValue       string                                       `json:"headerValue,omitempty"`
	AllowPrivateHosts bool                                         `json:"-"`
}
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}
type Client struct {
	command   *exec.Cmd
	stdin     io.WriteCloser
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[int64]chan response
	next      atomic.Int64
	done      chan struct{}
	closeOnce sync.Once
	doneOnce  sync.Once
	cancel    context.CancelFunc
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func Start(ctx context.Context, config ServerConfig) (*Client, error) {
	if config.Command == "" {
		return nil, errors.New("MCP command is required")
	}
	cmd := exec.CommandContext(ctx, config.Command, config.Args...)
	cmd.Dir = config.Directory
	if len(config.Env) > 0 {
		environment := cmd.Environ()
		for key, value := range config.Env {
			environment = append(environment, key+"="+value)
		}
		cmd.Env = environment
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	client := &Client{command: cmd, stdin: stdin, pending: map[int64]chan response{}, done: make(chan struct{})}
	go client.read(stdout)
	var initialized json.RawMessage
	if err := client.call(ctx, "initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "ProofCode", "version": "0.4.0"}}, &initialized); err != nil {
		_ = client.Close()
		return nil, err
	}
	if err := client.notify("notifications/initialized", map[string]any{}); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (c *Client) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	return listTools(ctx, c.call)
}

func listTools(ctx context.Context, call func(context.Context, string, any, any) error) ([]ToolDefinition, error) {
	var tools []ToolDefinition
	cursor := ""
	seen := map[string]bool{}
	names := map[string]bool{}
	for page := 0; page < 32; page++ {
		var result struct {
			Tools      []ToolDefinition `json:"tools"`
			NextCursor string           `json:"nextCursor"`
		}
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := call(ctx, "tools/list", params, &result); err != nil {
			return nil, err
		}
		for _, definition := range result.Tools {
			if !toolName.MatchString(definition.Name) || names[definition.Name] || definition.InputSchema == nil {
				return nil, errors.New("invalid or duplicate MCP tool definition")
			}
			names[definition.Name] = true
			tools = append(tools, definition)
		}
		if len(tools) > 256 {
			return nil, errors.New("MCP tool catalog exceeds 256 entries")
		}
		if result.NextCursor == "" {
			return tools, nil
		}
		if seen[result.NextCursor] {
			return nil, errors.New("MCP pagination did not advance")
		}
		seen[result.NextCursor] = true
		cursor = result.NextCursor
	}
	return nil, errors.New("MCP pagination exceeds 32 pages")
}

var toolName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	var result json.RawMessage
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &result); err != nil {
		return nil, err
	}
	return result, nil
}
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		c.finish()
		_ = c.stdin.Close()
		if c.command.Process != nil {
			err = c.command.Process.Kill()
			_ = c.command.Wait()
		}
	})
	return err
}
func (c *Client) call(ctx context.Context, method string, params any, target any) error {
	id := c.next.Add(1)
	wait := make(chan response, 1)
	c.pendingMu.Lock()
	c.pending[id] = wait
	c.pendingMu.Unlock()
	defer func() { c.pendingMu.Lock(); delete(c.pending, id); c.pendingMu.Unlock() }()
	message := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	err = c.write(ctx, append(encoded, '\n'))
	if err != nil {
		return err
	}
	select {
	case value := <-wait:
		if value.Error != nil {
			return fmt.Errorf("MCP %s error %d: %s", method, value.Error.Code, value.Error.Message)
		}
		return json.Unmarshal(value.Result, target)
	case <-ctx.Done():
		// The notification is best effort; cancellation never waits for a blocked pipe.
		go func() {
			_ = c.notify("notifications/cancelled", map[string]any{"requestId": id, "reason": "request cancelled"})
		}()
		return ctx.Err()
	case <-c.done:
		return errors.New("MCP server stopped")
	}
}

// A server that stops reading stdin must not make a task uncancellable. Closing
// the client on a blocked write releases the pipe and all pending RPC waiters.
func (c *Client) write(ctx context.Context, data []byte) error {
	finished := make(chan error, 1)
	go func() {
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		if err := ctx.Err(); err != nil {
			finished <- err
			return
		}
		_, err := c.stdin.Write(data)
		finished <- err
	}()
	select {
	case err := <-finished:
		return err
	case <-ctx.Done():
		_ = c.Close()
		return ctx.Err()
	case <-c.done:
		return errors.New("MCP server stopped")
	}
}
func (c *Client) notify(method string, params any) error {
	encoded, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	_, err = c.stdin.Write(append(encoded, '\n'))
	c.writeMu.Unlock()
	return err
}
func (c *Client) read(reader io.Reader) {
	defer c.finish()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var value response
		if json.Unmarshal(scanner.Bytes(), &value) != nil || value.ID == 0 {
			continue
		}
		c.pendingMu.Lock()
		wait := c.pending[value.ID]
		c.pendingMu.Unlock()
		if wait != nil {
			select {
			case wait <- value:
			default:
			}
		}
	}
}

func (c *Client) finish() { c.doneOnce.Do(func() { close(c.done) }) }
