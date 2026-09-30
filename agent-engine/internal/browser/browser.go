package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
)

type Session struct {
	executionMu sync.Mutex
	consoleMu   sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	console     []string
	workspace   *workspace.Workspace
}

func New(parent context.Context, executable string, value *workspace.Workspace) (*Session, error) {
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("headless", true), chromedp.Flag("disable-gpu", true), chromedp.Flag("no-first-run", true), chromedp.Flag("disable-background-networking", true))
	if executable != "" {
		options = append(options, chromedp.ExecPath(executable))
	}
	allocator, allocatorCancel := chromedp.NewExecAllocator(parent, options...)
	ctx, browserCancel := chromedp.NewContext(allocator)
	ctx, cancel := context.WithCancel(ctx)
	valueSession := &Session{ctx: ctx, workspace: value}
	valueSession.cancel = func() { cancel(); browserCancel(); allocatorCancel() }
	chromedp.ListenTarget(ctx, func(event any) {
		called, ok := event.(*runtime.EventConsoleAPICalled)
		if !ok {
			return
		}
		parts := make([]string, 0, len(called.Args))
		for _, arg := range called.Args {
			if len(arg.Value) > 0 {
				parts = append(parts, string(arg.Value))
			} else {
				parts = append(parts, arg.Description)
			}
		}
		valueSession.consoleMu.Lock()
		valueSession.console = append(valueSession.console, fmt.Sprintf("%s: %s", called.Type, strings.Join(parts, " ")))
		valueSession.consoleMu.Unlock()
	})
	startCtx, startCancel := context.WithTimeout(ctx, 20*time.Second)
	defer startCancel()
	if err := chromedp.Run(startCtx); err != nil {
		valueSession.cancel()
		return nil, err
	}
	return valueSession, nil
}

func (s *Session) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}
func (s *Session) Execute(ctx context.Context, raw json.RawMessage) tool.Result {
	args, err := tool.Decode[struct {
		Action   string `json:"action"`
		URL      string `json:"url"`
		Selector string `json:"selector"`
		Text     string `json:"text"`
		Name     string `json:"name"`
	}](raw)
	if err != nil {
		return tool.Result{Content: err.Error(), IsError: true}
	}
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	runCtx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return tool.Result{Content: ctx.Err().Error(), IsError: true}
	}
	switch args.Action {
	case "open":
		if !allowedURL(args.URL) {
			return failure(errors.New("only http and https URLs are allowed"))
		}
		if err := chromedp.Run(runCtx, chromedp.Navigate(args.URL), chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
			return failure(err)
		}
		return tool.Result{Content: "Opened " + args.URL}
	case "snapshot":
		var html string
		if err := chromedp.Run(runCtx, chromedp.OuterHTML("html", &html, chromedp.ByQuery)); err != nil {
			return failure(err)
		}
		if len(html) > 256<<10 {
			html = html[:256<<10]
		}
		return tool.Result{Content: html}
	case "click":
		if args.Selector == "" {
			return failure(errors.New("selector is required"))
		}
		if err := chromedp.Run(runCtx, chromedp.Click(args.Selector, chromedp.ByQuery)); err != nil {
			return failure(err)
		}
		return tool.Result{Content: "Clicked " + args.Selector}
	case "type":
		if args.Selector == "" {
			return failure(errors.New("selector is required"))
		}
		if err := chromedp.Run(runCtx, chromedp.Focus(args.Selector, chromedp.ByQuery), chromedp.SendKeys(args.Selector, args.Text, chromedp.ByQuery)); err != nil {
			return failure(err)
		}
		return tool.Result{Content: "Typed into " + args.Selector}
	case "console":
		s.consoleMu.Lock()
		defer s.consoleMu.Unlock()
		return tool.Result{Content: strings.Join(s.console, "\n"), Metadata: map[string]any{"count": len(s.console)}}
	case "screenshot":
		name := args.Name
		if name == "" {
			name = fmt.Sprintf("browser-%d.png", time.Now().UnixMilli())
		}
		name = filepath.Base(name)
		if !strings.HasSuffix(strings.ToLower(name), ".png") {
			name += ".png"
		}
		relative := filepath.ToSlash(filepath.Join(".proofcode", "artifacts", name))
		path, resolveErr := s.workspace.Resolve(relative)
		if resolveErr != nil {
			return failure(resolveErr)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return failure(err)
		}
		var image []byte
		if err := chromedp.Run(runCtx, chromedp.FullScreenshot(&image, 90)); err != nil {
			return failure(err)
		}
		if err := os.WriteFile(path, image, 0644); err != nil {
			return failure(err)
		}
		return tool.Result{Content: relative, Metadata: map[string]any{"bytes": len(image)}}
	default:
		return failure(fmt.Errorf("unknown browser action %q", args.Action))
	}
}

type Tool struct{ Session *Session }

func (Tool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "browser", Description: "Validate a Web application using an isolated browser. Actions: open, snapshot, click, type, console, screenshot.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"action": map[string]any{"enum": []string{"open", "snapshot", "click", "type", "console", "screenshot"}}, "url": map[string]any{"type": "string"}, "selector": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}}, "required": []string{"action"}, "additionalProperties": false}}
}
func (Tool) Risk(raw json.RawMessage) tool.Risk {
	var value struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(raw, &value)
	if value.Action == "open" || value.Action == "click" || value.Action == "type" {
		return tool.RiskExec
	}
	return tool.RiskRead
}
func (t Tool) Execute(ctx context.Context, raw json.RawMessage) tool.Result {
	return t.Session.Execute(ctx, raw)
}
func allowedURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}
func failure(err error) tool.Result { return tool.Result{Content: err.Error(), IsError: true} }
