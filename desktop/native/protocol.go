package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LSP and DAP share Content-Length framing but use different envelopes.
// One project owns each process; callers can never select another project's ID.
type ProtocolState struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	RootURI  string          `json:"rootUri"`
	Running  bool            `json:"running"`
	Sequence int             `json:"sequence"`
	Events   []ProtocolEvent `json:"events"`
	Dropped  bool            `json:"dropped"`
	Error    string          `json:"error"`
}
type ProtocolEvent struct {
	Sequence int             `json:"sequence"`
	Message  json.RawMessage `json:"message"`
}
type protocolRun struct {
	mu         sync.Mutex
	writeMu    sync.Mutex
	handle     string
	state      ProtocolState
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	stdin      io.WriteCloser
	next       int
	pending    map[int]chan json.RawMessage
	eventBytes int
}

func fileURI(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
func readProtocol(reader *bufio.Reader) (json.RawMessage, error) {
	size := -1
	total := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		total += len(line)
		if total > 8192 {
			return nil, errors.New("protocol header exceeds limit")
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(key, "Content-Length") {
			size, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if size < 0 || size > 2<<20 {
		return nil, errors.New("invalid protocol message length")
	}
	data := make([]byte, size)
	_, err := io.ReadFull(reader, data)
	if err == nil && !json.Valid(data) {
		err = errors.New("invalid protocol JSON")
	}
	return data, err
}
func (run *protocolRun) send(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return errors.New("protocol request exceeds limit")
	}
	run.writeMu.Lock()
	defer run.writeMu.Unlock()
	_, err = fmt.Fprintf(run.stdin, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}
func (run *protocolRun) listen(stdout io.Reader) {
	reader := bufio.NewReader(stdout)
	for {
		data, err := readProtocol(reader)
		if err != nil {
			run.mu.Lock()
			if run.state.Running && !errors.Is(err, io.EOF) {
				run.state.Error = err.Error()
			}
			run.mu.Unlock()
			run.cancel()
			return
		}
		var msg struct {
			ID         json.RawMessage `json:"id"`
			Type       string          `json:"type"`
			RequestSeq int             `json:"request_seq"`
			Method     string          `json:"method"`
			Params     json.RawMessage `json:"params"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		responseID := 0
		if run.state.Kind == "lsp" && len(msg.ID) > 0 && msg.Method == "" {
			_ = json.Unmarshal(msg.ID, &responseID)
		}
		if run.state.Kind == "dap" && msg.Type == "response" {
			responseID = msg.RequestSeq
		}
		run.mu.Lock()
		if channel := run.pending[responseID]; responseID > 0 && channel != nil {
			delete(run.pending, responseID)
			channel <- data
			run.mu.Unlock()
			continue
		}
		run.state.Sequence++
		run.state.Events = append(run.state.Events, ProtocolEvent{run.state.Sequence, data})
		run.eventBytes += len(data)
		for len(run.state.Events) > 200 || run.eventBytes > 2<<20 {
			run.eventBytes -= len(run.state.Events[0].Message)
			run.state.Events = run.state.Events[1:]
		}
		run.mu.Unlock()
		// Servers may request configuration or registration. Never silently
		// permit a server to apply edits or spawn a terminal via reverse RPC.
		if run.state.Kind == "lsp" && len(msg.ID) > 0 && msg.Method != "" {
			var id any
			_ = json.Unmarshal(msg.ID, &id)
			answer := map[string]any{"jsonrpc": "2.0", "id": id, "result": nil}
			switch msg.Method {
			case "workspace/configuration":
				var p struct {
					Items []any `json:"items"`
				}
				_ = json.Unmarshal(msg.Params, &p)
				values := make([]any, len(p.Items))
				for i := range values {
					values[i] = map[string]any{}
				}
				answer["result"] = values
			case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create":
			case "workspace/applyEdit":
				answer["result"] = map[string]any{"applied": false, "failureReason": "Review and save edits explicitly in ProofCode"}
			default:
				delete(answer, "result")
				answer["error"] = map[string]any{"code": -32601, "message": "unsupported client request"}
			}
			_ = run.send(answer)
		} else if run.state.Kind == "dap" && msg.Type == "request" {
			var reverse struct {
				Seq     int    `json:"seq"`
				Command string `json:"command"`
			}
			_ = json.Unmarshal(data, &reverse)
			_ = run.send(map[string]any{"seq": 0, "type": "response", "request_seq": reverse.Seq, "command": reverse.Command, "success": false, "message": "Reverse requests are unsupported; use internalConsole"})
		}
	}
}
func (a *App) StartProjectProtocol(handle, kind, program string, args []string) (ProtocolState, error) {
	if kind != "lsp" && kind != "dap" {
		return ProtocolState{}, errors.New("choose lsp or dap")
	}
	if program == "" || len(program) > 500 || len(args) > 100 {
		return ProtocolState{}, errors.New("bounded executable and args required")
	}
	for _, arg := range args {
		if len(arg) > 8192 || strings.ContainsRune(arg, 0) {
			return ProtocolState{}, errors.New("invalid adapter argument")
		}
	}
	a.projectMu.Lock()
	p, err := a.projectByHandle(handle)
	a.projectMu.Unlock()
	if err != nil {
		return ProtocolState{}, err
	}
	a.protocolMu.Lock()
	defer a.protocolMu.Unlock()
	for _, old := range a.protocols {
		old.mu.Lock()
		running := old.state.Running
		old.mu.Unlock()
		if old.handle == handle && old.state.Kind == kind && running {
			return ProtocolState{}, errors.New("stop the active project protocol first")
		}
	}
	executable, resolvedArgs, err := resolveLocalProgram(program, args)
	if err != nil {
		return ProtocolState{}, err
	}
	id, err := randomID()
	if err != nil {
		return ProtocolState{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, executable, resolvedArgs...)
	cmd.Dir = p.Path
	cmd.Env = os.Environ()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return ProtocolState{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return ProtocolState{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return ProtocolState{}, err
	}
	run := &protocolRun{handle: handle, state: ProtocolState{ID: id, Kind: kind, RootURI: fileURI(p.Path), Running: true, Events: []ProtocolEvent{}}, cmd: cmd, cancel: cancel, stdin: stdin, pending: map[int]chan json.RawMessage{}}
	if err = cmd.Start(); err != nil {
		cancel()
		return ProtocolState{}, err
	}
	if a.protocols == nil {
		a.protocols = map[string]*protocolRun{}
	}
	a.protocols[id] = run
	initial := run.state
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})
	go func() { defer close(stdoutDone); run.listen(stdout) }()
	go func() { // Drain stderr without unbounded retention.
		defer close(stderrDone)
		data, _ := io.ReadAll(io.LimitReader(stderr, 64000))
		_, _ = io.Copy(io.Discard, stderr)
		if len(data) > 0 {
			run.mu.Lock()
			run.state.Error = string(data)
			run.mu.Unlock()
		}
	}()
	go func() {
		<-stdoutDone
		<-stderrDone
		err := cmd.Wait()
		run.mu.Lock()
		run.state.Running = false
		if err != nil && run.state.Error == "" {
			run.state.Error = err.Error()
		}
		for id, ch := range run.pending {
			close(ch)
			delete(run.pending, id)
		}
		run.mu.Unlock()
		cancel()
	}()
	return initial, nil
}
func (a *App) protocolByID(handle, id string) (*protocolRun, error) {
	a.protocolMu.Lock()
	defer a.protocolMu.Unlock()
	run := a.protocols[id]
	if run == nil || run.handle != handle {
		return nil, errors.New("protocol session is outside this project")
	}
	return run, nil
}
func (a *App) ProjectProtocolRequest(handle, id, method, params string, notification bool) (json.RawMessage, error) {
	run, err := a.protocolByID(handle, id)
	if err != nil {
		return nil, err
	}
	if len(method) > 200 || method == "" || len(params) > 2<<20 {
		return nil, errors.New("invalid protocol request")
	}
	var body any
	if err = json.Unmarshal([]byte(params), &body); err != nil {
		return nil, err
	}
	run.mu.Lock()
	if !run.state.Running {
		run.mu.Unlock()
		return nil, errors.New("protocol process has exited")
	}
	run.next++
	seq := run.next
	channel := make(chan json.RawMessage, 1)
	if !notification {
		run.pending[seq] = channel
	}
	run.mu.Unlock()
	defer func() { run.mu.Lock(); delete(run.pending, seq); run.mu.Unlock() }()
	var envelope map[string]any
	if run.state.Kind == "lsp" {
		envelope = map[string]any{"jsonrpc": "2.0", "method": method, "params": body}
		if !notification {
			envelope["id"] = seq
		}
	} else {
		if notification {
			return nil, errors.New("DAP commands require responses")
		}
		envelope = map[string]any{"seq": seq, "type": "request", "command": method, "arguments": body}
	}
	if err = run.send(envelope); err != nil {
		return nil, err
	}
	if notification {
		return json.RawMessage(`null`), nil
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case response, ok := <-channel:
		if !ok {
			return nil, errors.New("protocol process exited while waiting")
		}
		return response, nil
	case <-timer.C:
		if run.state.Kind == "lsp" {
			_ = run.send(map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": seq}})
		}
		return nil, errors.New("protocol request timed out")
	}
}
func (a *App) GetProjectProtocol(handle, id string, after int) (ProtocolState, error) {
	run, err := a.protocolByID(handle, id)
	if err != nil {
		return ProtocolState{}, err
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	state := run.state
	state.Events = []ProtocolEvent{}
	if len(run.state.Events) > 0 {
		state.Dropped = after < run.state.Events[0].Sequence-1
	}
	for _, event := range run.state.Events {
		if event.Sequence > after {
			state.Events = append(state.Events, event)
		}
	}
	return state, nil
}
func (a *App) StopProjectProtocol(handle, id string) error {
	run, err := a.protocolByID(handle, id)
	if err != nil {
		return err
	}
	run.mu.Lock()
	running := run.state.Running
	run.mu.Unlock()
	if running {
		_ = stopCommandTree(run.cmd)
		run.cancel()
	}
	return nil
}
